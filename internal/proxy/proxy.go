package proxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"golang.org/x/sync/errgroup"
)

// connect dials the upstream address, then copies data
// between it and connection accepted on a local port
func Connect(local net.Conn, upstreamAddr, from string, initalData string, dialTimeout time.Duration) {
	defer local.Close()

	// If Dial is used on its own, then the timeout can be as long
	// as 2 minutes on MacOS for an unreachable host
	upstream, err := net.DialTimeout("tcp", upstreamAddr, dialTimeout)
	if err != nil {
		log.Printf("error dialing %s %s", upstreamAddr, err.Error())
		return
	}
	defer upstream.Close()

	// as we might read the inital data (iperf cookie in this case early),
	// we need to make sure its written to the upstream as well
	upstream.Write([]byte(initalData))

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
