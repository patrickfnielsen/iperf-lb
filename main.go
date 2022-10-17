package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"
)

type Session struct {
	Client    string
	IperfPort int
	Iperf     *exec.Cmd
}

func (session *Session) containsClient(str string) bool {
	return session.Client == str
}

type Sessions []Session

func (sessions Sessions) getNextPort() int {
	newPort := 5202
	for _, s := range sessions {
		if s.IperfPort >= newPort {
			newPort = s.IperfPort + 1
		}
	}

	return newPort
}

func main() {
	log.Printf("Starting iperf-lb")

	var (
		listen      string
		dialTimeout time.Duration
	)

	flag.StringVar(&listen, "l", ":5201", "The ip and port to listen to, default :5201")
	flag.DurationVar(&dialTimeout, "t", time.Millisecond*1500, "Dial timeout, default 1500 millisecond")
	flag.Parse()

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

func removeSession(sessions Sessions, iperf *exec.Cmd) Sessions {
	if iperf == nil {
		return sessions
	}

	filteredSessions := sessions[:0]
	for _, session := range sessions {
		if session.Iperf != iperf {
			filteredSessions = append(filteredSessions, session)
		}
	}
	return filteredSessions
}

func forward(name, from string, dialTimeout time.Duration) error {
	var sessions Sessions
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
		existingSessionFound := false
		log.Printf("Accepted connection (%s)", fullClientAddress)

		// get a iperf existing iperf service for the client, or spawn a new iperf server on the next free port
		for _, s := range sessions {
			if s.containsClient(clientIP) {
				log.Printf("Found existing session [::1]:%d (%s)", s.IperfPort, fullClientAddress)
				upstream = fmt.Sprintf("localhost:%d", s.IperfPort)
				existingSessionFound = true
			}
		}

		// if no session is found, spawn a new one
		if !existingSessionFound {
			iperfPort := sessions.getNextPort()
			iperfCmd := exec.Command("iperf3", "-1", "-s", "-p", strconv.Itoa(iperfPort))
			upstream = fmt.Sprintf("localhost:%d", iperfPort)
			sessions = append(sessions, Session{
				Client:    clientIP,
				IperfPort: iperfPort,
				Iperf:     iperfCmd,
			})

			log.Printf("Spawning session [::1]:%d", iperfPort)

			iperfCmd.Start()
			time.Sleep(time.Second * 1) //TODO: REMOVE THIS

			// wait for iperf to despawn and remove it from the list
			// it despawns after a test has been run
			go func() {
				err := iperfCmd.Wait()
				if err != nil {
					log.Printf("Session exited unexpected %s\n", err.Error())
				}

				for _, s := range sessions {
					if s.Iperf == iperfCmd {
						log.Printf("Cleaning up session [::1]:%d", iperfPort)
						sessions = removeSession(sessions, iperfCmd)
					}
				}
			}()
		}

		// A separate Goroutine means the loop can accept another
		// incoming connection on the local address
		go connect(local, upstream, from, dialTimeout)
	}
}

// connect dials the upstream address, then copies data
// between it and connection accepted on a local port
func connect(local net.Conn, upstreamAddr, from string, dialTimeout time.Duration) {
	defer local.Close()

	// If Dial is used on its own, then the timeout can be as long
	// as 2 minutes on MacOS for an unreachable host
	upstream, err := net.DialTimeout("tcp", upstreamAddr, dialTimeout)
	if err != nil {
		log.Printf("error dialing %s %s", upstreamAddr, err.Error())
		return
	}
	defer upstream.Close()

	log.Printf("Connected %s => %s (%s)", from, upstream.RemoteAddr().String(), local.RemoteAddr().String())

	ctx := context.Background()
	if err := copy(ctx, local, upstream); err != nil && err.Error() != "done" {
		log.Printf("error forwarding connection %s", err.Error())
	}

	log.Printf("Closed %s => %s (%s)", from, upstream.RemoteAddr().String(), local.RemoteAddr().String())
}

// copy copies data between two connections using io.Copy
// and will exit when either connection is closed or runs
// into an error
func copy(ctx context.Context, from, to net.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	errgrp, _ := errgroup.WithContext(ctx)
	errgrp.Go(func() error {
		io.Copy(from, to)
		cancel()

		return fmt.Errorf("done")
	})

	errgrp.Go(func() error {
		io.Copy(to, from)
		cancel()

		return fmt.Errorf("done")
	})

	errgrp.Go(func() error {
		<-ctx.Done()

		// This closes both ends of the connection as
		// soon as possible.
		from.Close()
		to.Close()
		return fmt.Errorf("done")
	})

	if err := errgrp.Wait(); err != nil {
		return err
	}

	return nil
}
