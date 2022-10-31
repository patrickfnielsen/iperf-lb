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

	"github.com/patrickfnielsen/iperf-lb/internal/proxy"
	"github.com/patrickfnielsen/iperf-lb/internal/session"
)

// https://github.com/esnet/iperf/blob/bd1437791a63579d589e9bea7de9250a876a5c97/src/iperf.h#L134
const IPERF_COOKIE_SIZE = 37
const IPERF_NAME = "iperf3"

func main() {
	log.Printf("Starting iperf-lb")

	var (
		listen      string
		dialTimeout time.Duration
	)

	flag.StringVar(&listen, "l", ":5201", "The ip and port to listen to, default :5201")
	flag.DurationVar(&dialTimeout, "t", time.Millisecond*1500, "Dial timeout, default 1500 millisecond")
	flag.Parse()

	_, err := exec.LookPath(IPERF_NAME)
	if err != nil {
		log.Fatal("failed to find iperf3 process")
	}

	if err := forward("iperf-lb", listen, dialTimeout); err != nil {
		log.Printf("error forwarding %s", err.Error())
		os.Exit(1)
	}
}

func getClientIp(conn net.Conn) (error, string) {
	if addr, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		return nil, addr.IP.String()
	}

	return fmt.Errorf("failed to get client ip address"), ""
}

func forward(name, from string, dialTimeout time.Duration) error {
	var allSessions *session.Sessions = &session.Sessions{}
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
		err, clientIP := getClientIp(local)
		if err != nil {
			return err
		}

		var upstream string
		log.Printf("Accepted connection (%s)", fullClientAddress)

		// read the iperf cookie, we use this to loadbalance iperf sessions to the same process
		var reply strings.Builder
		read, err := io.CopyN(&reply, local, IPERF_COOKIE_SIZE)
		if err != nil {
			return fmt.Errorf("could not read: %v\n", err.Error())
		}

		if read <= 0 {
			return fmt.Errorf("got no reply from server, %+v\n", reply)
		}

		var iperfCookie = reply.String()
		log.Printf("Read iperf cookie %s (%s)", iperfCookie, fullClientAddress)

		// get an existing iperf service for the client, or spawn a new iperf server on the next free port
		s, sessionFound := allSessions.GetSession(iperfCookie)
		if sessionFound {
			upstream = fmt.Sprintf("localhost:%d", s.IperfPort)
			log.Printf("Found session [::1]:%d", s.IperfPort)
		}

		// if no session is found, spawn a new one
		if !sessionFound {
			iperfPort := allSessions.GetNextPort()
			iperfCmd := exec.Command(IPERF_NAME, "-1", "-s", "-p", strconv.Itoa(iperfPort))
			upstream = fmt.Sprintf("localhost:%d", iperfPort)
			session := session.Session{
				Client:      clientIP,
				IperfPort:   iperfPort,
				IperfCookie: iperfCookie,
				Iperf:       iperfCmd,
			}
			*allSessions = append(*allSessions, session)

			log.Printf("Spawning session [::1]:%d", iperfPort)

			iperfCmd.Start()
			time.Sleep(time.Second * 1) //TODO: Checkout for correct text???

			// wait for iperf to despawn and remove it from the list
			// it despawns after a test has been run
			go waitAndCleanupSession(allSessions, session)
		}

		// A separate Goroutine means the loop can accept another
		// incoming connection on the local address
		go proxy.Connect(local, upstream, from, iperfCookie, dialTimeout)
	}
}

func waitAndCleanupSession(sessions *session.Sessions, session session.Session) {
	err := session.Iperf.Wait()
	if err != nil {
		log.Printf("Session exited unexpected %s\n", err.Error())
	}

	for _, s := range *sessions {
		if s.Iperf == session.Iperf {
			log.Printf("Cleaning up session [::1]:%d", session.IperfPort)
			*sessions = *sessions.RemoveSession(session)
		}
	}
}
