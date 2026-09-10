package remote

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/wrongstack/wrongtop/internal/collector"
)

// Options configures Serve.
type Options struct {
	// Listen is the TCP address to bind, e.g. ":61234".
	Listen string
	// Token clients must present; Serve refuses to start without one.
	Token string
	// Refresh is the snapshot cadence (floored at 250ms).
	Refresh time.Duration
	// Version is reported in the Hello frame.
	Version string
}

// Serve runs the monitoring server until ctx is done: one shared
// collector feeds every authenticated client. The protocol is read-only
// by construction — client input is limited to the auth handshake.
func Serve(ctx context.Context, opts Options) error {
	if opts.Token == "" {
		return fmt.Errorf("serve requires a token (--token or $WRONGTOP_TOKEN)")
	}
	if opts.Refresh < 250*time.Millisecond { //nolint:mnd // matches config floor
		opts.Refresh = 250 * time.Millisecond
	}

	ln, err := net.Listen("tcp", opts.Listen)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", opts.Listen, err)
	}
	defer func() { _ = ln.Close() }()

	coll := collector.New(opts.Refresh)
	hello := Hello{
		Protocol:  Protocol,
		Version:   opts.Version,
		RefreshMS: int(opts.Refresh / time.Millisecond),
	}

	var (
		mu      sync.Mutex
		clients = make(map[chan collector.Snapshot]struct{})
	)
	broadcast := func(snap collector.Snapshot) {
		mu.Lock()
		defer mu.Unlock()
		for ch := range clients {
			select {
			case ch <- snap:
			default:
				// slow client: drop it rather than stall everyone
				delete(clients, ch)
				close(ch)
			}
		}
	}

	go func() {
		<-ctx.Done()
		_ = ln.Close() // unblocks Accept so Serve can return
		// End every connected stream: closing each client's channel
		// finishes its handleConn range loop, which unregisters and
		// closes the conn. Race-free against broadcast and register:
		// all three hold mu for their whole critical section, and the
		// register hook refuses once ctx is done.
		mu.Lock()
		defer mu.Unlock()
		for ch := range clients {
			delete(clients, ch)
			close(ch)
		}
	}()

	// one initial sample fills the Hello hostname before clients arrive
	first := coll.Collect(ctx)
	hello.Hostname = first.Host.Hostname
	broadcast(first)

	go func() {
		ticker := time.NewTicker(opts.Refresh)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				collectCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				broadcast(coll.Collect(collectCtx))
				cancel()
			}
		}
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // shutdown
			}
			return fmt.Errorf("accept: %w", err)
		}
		go handleConn(conn, hello, opts.Token, func(ch chan collector.Snapshot) bool {
			mu.Lock()
			defer mu.Unlock()
			select {
			case <-ctx.Done():
				return false
			default:
			}
			if _, dup := clients[ch]; dup {
				return true
			}
			clients[ch] = struct{}{}
			return true
		}, func(ch chan collector.Snapshot) {
			mu.Lock()
			defer mu.Unlock()
			if _, ok := clients[ch]; ok {
				delete(clients, ch)
				close(ch)
			}
		})
	}
}

// handleConn authenticates one client, registers it and writes snapshots
// until the stream ends or the server shuts down.
func handleConn(conn net.Conn, hello Hello, token string,
	register func(chan collector.Snapshot) bool, unregister func(chan collector.Snapshot)) {
	defer func() { _ = conn.Close() }()

	// bound the handshake: a token check must not hold a slot forever
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	var auth Auth
	if err := ReadFrame(conn, 4<<10, &auth); err != nil {
		return
	}
	if !Authorize(auth.Token, token) {
		return // silent refusal: no oracle for wrong-vs-missing
	}
	if err := WriteFrame(conn, hello); err != nil {
		return
	}
	_ = conn.SetDeadline(time.Time{}) // stream mode

	ch := make(chan collector.Snapshot, 4)
	if !register(ch) {
		return
	}
	defer unregister(ch)

	// Each frame is written against a deadline: a client that stops
	// reading (asleep laptop, wedged reader) fills the socket buffers
	// and would otherwise park this goroutine in WriteFrame forever.
	// The drop and shutdown paths close the channel underneath us, and
	// only a failed write lets the range loop observe that and release
	// the conn.
	wd := max(5*time.Second, 2*time.Duration(hello.RefreshMS)*time.Millisecond)
	for snap := range ch {
		_ = conn.SetWriteDeadline(time.Now().Add(wd))
		if err := WriteFrame(conn, snap); err != nil {
			return
		}
	}
}
