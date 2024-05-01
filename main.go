package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"net/http"

	"github.com/patrickfnielsen/iperf-lb/internal/proxy"
	"github.com/patrickfnielsen/iperf-lb/internal/session"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// https://github.com/esnet/iperf/blob/bd1437791a63579d589e9bea7de9250a876a5c97/src/iperf.h#L134
const IPERF_COOKIE_SIZE = 37
const IPERF_NAME = "iperf3"

var (
	promSessionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "iperflb_sessions_total",
		Help: "The total number of iperf session spawned",
	})
	promSessionsCurrent = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "iperflb_sessions_current",
		Help: "The current number of iperf sessions",
	})
)

var (
	listen        string
	enableMetrics bool
	dialTimeout   time.Duration
	sessions      session.Sessions = session.Sessions{}
)

func main() {
	log.Printf("Starting iperf-lb")

	flag.StringVar(&listen, "l", ":5201", "The ip and port to listen to, default :5201")
	flag.BoolVar(&enableMetrics, "metrics", false, "Set to enable prometheus metrics on :2112")
	flag.DurationVar(&dialTimeout, "t", time.Millisecond*1500, "Dial timeout, default 1500 millisecond")
	flag.Parse()

	if enableMetrics {
		http.Handle("/metrics", promhttp.Handler())
		go http.ListenAndServe(":2112", nil)

		log.Print("Listening for metrics on [::]:2112")
	}

	_, err := exec.LookPath(IPERF_NAME)
	if err != nil {
		log.Fatal("failed to find iperf3 process")
	}

	if err := forward(listen, dialTimeout); err != nil {
		log.Printf("error forwarding %s", err.Error())
		os.Exit(1)
	}
}

func forward(from string, dialTimeout time.Duration) error {
	l, err := net.Listen("tcp", from)
	if err != nil {
		return fmt.Errorf("error listening on %s %s", from, err.Error())
	}

	log.Printf("Listening on %s", l.Addr().String())

	defer l.Close()

	for {
		// accept a connection on the local port of the load balancer
		local, err := l.Accept()
		if err != nil {
			return fmt.Errorf("error accepting connection %s", err.Error())
		}

		fullClientAddress := local.RemoteAddr().String()
		clientIP, err := getClientIp(local)
		if err != nil {
			return err
		}

		log.Printf("Accepted connection (%s)", fullClientAddress)

		// read the iperf cookie, we use this to loadbalance iperf sessions to the same process
		var reply strings.Builder
		read, err := io.CopyN(&reply, local, IPERF_COOKIE_SIZE)
		if err != nil {
			return fmt.Errorf("could not read: %v", err.Error())
		}

		if read <= 0 {
			return fmt.Errorf("got no reply from server, %+v", reply)
		}

		iperfCookie := reply.String()
		log.Printf("Read iperf cookie %s (%s)", iperfCookie, fullClientAddress)

		// get an existing iperf service for the client, or spawn a new iperf server on the next free port
		var upstream string
		s, sessionFound := sessions.Get(iperfCookie)
		if sessionFound {
			upstream = fmt.Sprintf("localhost:%d", s.IperfPort)
			log.Printf("Found session [::1]:%d", s.IperfPort)
		}

		// if no session is found, spawn a new one
		if !sessionFound {
			iperfPort := sessions.GetNextPort()
			iperfCmd := exec.Command(IPERF_NAME, "--forceflush", "-1", "-s", "-p", strconv.Itoa(iperfPort))
			upstream = fmt.Sprintf("localhost:%d", iperfPort)
			session := session.Session{
				Client:      clientIP,
				IperfPort:   iperfPort,
				IperfCookie: iperfCookie,
				Iperf:       iperfCmd,
			}
			sessions.Add(session)

			log.Printf("Spawning session [::1]:%d", iperfPort)

			err = startProcessAndWaitForReady(iperfCmd, "Server listening")
			if err != nil {
				return err
			}

			// wait for iperf to despawn and remove it from the list
			// it despawns after a test has been run
			go waitAndCleanupSession(session)
		}

		// A separate Goroutine means the loop can accept another
		// incoming connection on the local address
		go proxy.Connect(local, upstream, from, iperfCookie, dialTimeout)
	}
}

func getClientIp(conn net.Conn) (string, error) {
	if addr, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		return addr.IP.String(), nil
	}

	return "", fmt.Errorf("failed to get client ip address")
}

func startProcessAndWaitForReady(iperfCmd *exec.Cmd, readyString string) error {
	// reader for command output, used to check that the process is ready
	stdout, err := iperfCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to listen to iperf output")
	}

	// start the iperf process and wait for the correct output
	if err = iperfCmd.Start(); err != nil {
		return err
	}

	promSessionsCurrent.Inc()
	promSessionsTotal.Inc()

	for {
		tmp := make([]byte, 1024)
		_, err := stdout.Read(tmp)
		if err != nil {
			return fmt.Errorf("failed to read iperf output: %s", err)
		}

		if strings.Contains(string(tmp), readyString) {
			return nil
		}
	}
}

func waitAndCleanupSession(session session.Session) {
	err := session.Iperf.Wait()
	if err != nil {
		log.Printf("Session exited unexpected %s\n", err.Error())
	}

	log.Printf("Cleaning up session [::1]:%d", session.IperfPort)
	sessions.Remove(session)
	promSessionsCurrent.Dec()
}
