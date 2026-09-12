package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wrongstack/wrongtop/internal/collector"
)

// freeListenAddr reserves an ephemeral loopback port and releases it.
func freeListenAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// dialRetry dials until the listener is bound or the timeout expires.
func dialRetry(t *testing.T, addr string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			return conn
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial %s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// startServe runs Serve on a goroutine and returns its address and error
// channel; the caller must cancel the context and read the channel.
func startServe(ctx context.Context, t *testing.T, addr, token string, refresh time.Duration) chan error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- Serve(ctx, Options{Listen: addr, Token: token, Refresh: refresh}) }()
	return errCh
}

// awaitServeError waits for Serve to return and requires a clean shutdown.
func awaitServeError(t *testing.T, errCh chan error) {
	t.Helper()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not shut down")
	}
}

// authorize exchanges the auth handshake for hello on a live server.
func authorize(t *testing.T, conn net.Conn, token string) Hello {
	t.Helper()
	if err := WriteFrame(conn, Auth{Token: token}); err != nil {
		t.Fatal(err)
	}
	var hello Hello
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err := ReadFrame(conn, 1<<10, &hello); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	return hello
}

// TestServeListenError covers the refresh floor and the listen failure
// branch: the port is already taken by ln.
func TestServeListenError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	err = Serve(context.Background(), Options{
		Listen:  ln.Addr().String(),
		Token:   "secret",
		Refresh: time.Millisecond, // below the 250ms floor
	})
	if err == nil || !strings.Contains(err.Error(), "listening on") {
		t.Fatalf("err = %v, want a listen failure", err)
	}
}

// TestServeRegisterAfterCancel cancels the server context while a client
// is mid-handshake; completing the handshake afterwards must be refused
// by the register hook and the connection dropped.
func TestServeRegisterAfterCancel(t *testing.T) {
	addr := freeListenAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := startServe(ctx, t, addr, "secret", 250*time.Millisecond)

	// The probe only gets a hello once Serve is accepting connections,
	// which also proves the initial collect has finished.
	probe := dialRetry(t, addr)
	defer func() { _ = probe.Close() }()
	authorize(t, probe, "secret")

	// Second client: send half of the auth frame header so the server
	// blocks reading it, then cancel, then finish the handshake.
	conn := dialRetry(t, addr)
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte{0, 0}); err != nil { // half of a 4-byte frame header
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond) // let the server block in ReadFrame
	cancel()

	auth, err := json.Marshal(Auth{Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte{0, byte(len(auth))}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(auth); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	// The server must hang up (register refused) rather than stream.
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatalf("connection was not closed after canceled-context register: %v", err)
	}
	awaitServeError(t, errCh)
}

// TestServeUnregistersDisconnectedClient covers the unregister hook: a
// client that resets its connection mid-stream makes the server's next
// frame write fail, which must remove and close its channel.
func TestServeUnregistersDisconnectedClient(t *testing.T) {
	addr := freeListenAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := startServe(ctx, t, addr, "secret", 250*time.Millisecond)

	conn := dialRetry(t, addr)
	authorize(t, conn, "secret")
	var snap collector.Snapshot
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	if err := ReadFrame(conn, MaxFrame, &snap); err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	// Reset the connection so the server's next write fails.
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetLinger(0)
	}
	_ = conn.Close()

	// Give a broadcast tick time to hit the dead socket, then shut down.
	time.Sleep(1500 * time.Millisecond)
	cancel()
	awaitServeError(t, errCh)
}

// TestServeDropsSlowClient covers the slow-client branch in broadcast:
// a client that stops reading fills its channel, and the next broadcast
// drops and closes it instead of stalling the server.
func TestServeDropsSlowClient(t *testing.T) {
	addr := freeListenAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := startServe(ctx, t, addr, "secret", 250*time.Millisecond)

	conn := dialRetry(t, addr)
	defer func() { _ = conn.Close() }()
	authorize(t, conn, "secret")
	// Shrink the client's receive window so the server's kernel send
	// buffer fills within a few frames. Without this the drop time
	// tracks OS-autotuned loopback buffer sizes — far past this test's
	// window on Linux CI, comfortably inside it on macOS.
	if tc, ok := conn.(*net.TCPConn); ok {
		_ = tc.SetReadBuffer(512)
	}

	// Stop reading: the server's socket buffers back up, handleConn
	// blocks writing, and the 4-slot client channel overflows.
	time.Sleep(7 * time.Second)

	// Drain: buffered frames end with EOF once the server dropped us.
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.Copy(io.Discard, conn); err != nil {
		t.Fatalf("server never dropped the slow client: %v", err)
	}
	cancel()
	awaitServeError(t, errCh)
}

// TestHandleConnWriteDeadlineDropsClient pins the per-frame write
// deadline in handleConn on an unbuffered pipe: a client that stops
// reading mid-stream is dropped once the deadline fires, no matter how
// large the OS socket buffers are (the end-to-end TCP variant above
// depends on kernel buffer limits and stays platform-sensitive).
func TestHandleConnWriteDeadlineDropsClient(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()

	registered := make(chan chan collector.Snapshot, 1)
	unregistered := make(chan struct{}, 1)
	register := func(c chan collector.Snapshot) bool { registered <- c; return true }
	unregister := func(chan collector.Snapshot) { unregistered <- struct{}{} }
	go handleConn(server, Hello{RefreshMS: 250}, "secret", register, unregister)

	// Handshake on the pipe: the handler is already parked in ReadFrame,
	// so the frame write/read pair up without deadlock.
	authorize(t, client, "secret")

	var feed chan collector.Snapshot
	select {
	case feed = <-registered:
	case <-time.After(2 * time.Second):
		t.Fatal("handleConn never registered")
	}

	// Queue frames; the handler stalls writing the first one.
	for i := 0; i < 3; i++ {
		feed <- collector.Snapshot{}
	}

	// Read exactly one frame, then stop: the next WriteFrame can only
	// unblock via the write deadline.
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	var snap collector.Snapshot
	if err := ReadFrame(client, MaxFrame, &snap); err != nil {
		t.Fatalf("first frame: %v", err)
	}
	_ = client.SetReadDeadline(time.Time{})

	select {
	case <-unregistered:
		// dropped once the write deadline fired
	case <-time.After(8 * time.Second):
		t.Fatal("handleConn did not drop the stalled client")
	}
}

// TestServeShutdownEndsClientStreams pins the shutdown sweep: after the
// context is cancelled and Serve returns, every connected client's
// stream must end — the sweep closes each client's channel so its
// handleConn loop unregisters and closes the conn instead of parking on
// `range ch` forever and leaking the goroutine and conn.
func TestServeShutdownEndsClientStreams(t *testing.T) {
	addr := freeListenAddr(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := startServe(ctx, t, addr, "secret", 250*time.Millisecond)

	// The probe proves Serve is accepting and streaming before the
	// clients under test dial.
	probe := dialRetry(t, addr)
	defer func() { _ = probe.Close() }()
	authorize(t, probe, "secret")

	var clients []*Client
	for i := 0; i < 2; i++ {
		c, err := Dial(ctx, addr, "secret", "test")
		if err != nil {
			t.Fatalf("dial client %d: %v", i, err)
		}
		defer c.Close()
		clients = append(clients, c)
		if _, err := c.Next(); err != nil { // one real snapshot proves the stream is live
			t.Fatalf("client %d received no snapshot: %v", i, err)
		}
	}

	cancel()
	awaitServeError(t, errCh)

	// Every connected client must observe the stream ending promptly:
	// buffered snapshots (cap 4) may still arrive after the sweep, then
	// the closed channel ends the stream with a read error. A deadline
	// timeout means the channel was never closed — the leak this pins.
	for i, c := range clients {
		_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			if _, err := c.Next(); err != nil {
				if errors.Is(err, os.ErrDeadlineExceeded) {
					t.Fatalf("client %d still streaming 5s after Serve returned: its channel was never closed", i)
				}
				break // stream ended — the sweep closed the channel
			}
		}
	}
}

// The tests below drive handleConn directly over net.Pipe for precise
// control over the auth handshake and channel lifecycle.

func TestHandleConnAuthReadError(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConn(server, Hello{Protocol: Protocol}, "secret",
			func(chan collector.Snapshot) bool { return true },
			func(chan collector.Snapshot) {})
	}()
	_ = client.Close() // hang up before sending auth
	<-done
}

func TestHandleConnBadToken(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConn(server, Hello{Protocol: Protocol}, "secret",
			func(chan collector.Snapshot) bool { return true },
			func(chan collector.Snapshot) {})
	}()
	if err := WriteFrame(client, Auth{Token: "wrong"}); err != nil {
		t.Fatal(err)
	}
	// Silent refusal: the server closes without writing anything.
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadAll(client); err != nil {
		t.Fatalf("expected clean close after bad token: %v", err)
	}
	<-done
}

func TestHandleConnHelloWriteError(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConn(server, Hello{Protocol: Protocol}, "secret",
			func(chan collector.Snapshot) bool { return true },
			func(chan collector.Snapshot) {})
	}()
	// net.Pipe is synchronous, so WriteFrame only returns once the
	// server has consumed the whole auth frame.
	if err := WriteFrame(client, Auth{Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	_ = client.Close() // the server's hello write must fail
	<-done
}

func TestHandleConnRegisterRefused(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConn(server, Hello{Protocol: Protocol}, "secret",
			func(chan collector.Snapshot) bool { return false },
			func(chan collector.Snapshot) {})
	}()
	if err := WriteFrame(client, Auth{Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	var hello Hello
	if err := ReadFrame(client, 1<<10, &hello); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadAll(client); err != nil {
		t.Fatalf("expected close when register is refused: %v", err)
	}
	<-done
}

func TestHandleConnSnapshotWriteError(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = server.Close() }()
	chans := make(chan chan collector.Snapshot, 1)
	var unregistered bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConn(server, Hello{Protocol: Protocol}, "secret",
			func(ch chan collector.Snapshot) bool { chans <- ch; return true },
			func(chan collector.Snapshot) { unregistered = true })
	}()
	if err := WriteFrame(client, Auth{Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	var hello Hello
	if err := ReadFrame(client, 1<<10, &hello); err != nil {
		t.Fatal(err)
	}
	_ = client.Close() // the next snapshot write must fail
	ch := <-chans
	ch <- collector.Snapshot{Time: time.Unix(1000, 0).UTC()}
	<-done
	if !unregistered {
		t.Error("unregister was not called after the write failed")
	}
}

// TestHandleConnStalledWriterReleasedByDeadline pins the stalled-writer
// release: a client that stops reading parks handleConn inside
// WriteFrame, and the drop and shutdown paths can only close the
// channel under it — a parked conn.Write never observes that. The
// per-frame write deadline must fail the stalled write so the loop
// returns, unregisters and releases the goroutine and conn.
func TestHandleConnStalledWriterReleasedByDeadline(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()
	chans := make(chan chan collector.Snapshot, 1)
	var unregistered bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConn(server, Hello{Protocol: Protocol, RefreshMS: 250}, "secret",
			func(ch chan collector.Snapshot) bool { chans <- ch; return true },
			func(chan collector.Snapshot) { unregistered = true })
	}()
	if err := WriteFrame(client, Auth{Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	var hello Hello
	if err := ReadFrame(client, 1<<10, &hello); err != nil {
		t.Fatal(err)
	}
	ch := <-chans
	// The client reads nothing from here: net.Pipe is unbuffered, so
	// handleConn parks inside WriteFrame on this snapshot.
	ch <- collector.Snapshot{Time: time.Unix(1000, 0).UTC()}
	close(ch) // what the slow-client drop and the shutdown sweep both do

	// The write deadline (5s floor) must break the stalled write and end
	// the loop; 10s covers it with margin.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handleConn is still parked in WriteFrame after its channel was closed")
	}
	if !unregistered {
		t.Error("unregister was not called after the stalled write failed")
	}
}

func TestHandleConnStreamsSnapshots(t *testing.T) {
	server, client := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()
	chans := make(chan chan collector.Snapshot, 1)
	unregistered := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConn(server, Hello{Protocol: Protocol}, "secret",
			func(ch chan collector.Snapshot) bool { chans <- ch; return true },
			func(chan collector.Snapshot) { close(unregistered) })
	}()
	if err := WriteFrame(client, Auth{Token: "secret"}); err != nil {
		t.Fatal(err)
	}
	var hello Hello
	if err := ReadFrame(client, 1<<10, &hello); err != nil {
		t.Fatal(err)
	}

	ch := <-chans
	want := []collector.Snapshot{
		{Time: time.Unix(1000, 0).UTC()},
		{Time: time.Unix(2000, 0).UTC()},
	}
	for _, snap := range want {
		ch <- snap
	}
	for _, snap := range want {
		var got collector.Snapshot
		_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := ReadFrame(client, MaxFrame, &got); err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		if !reflect.DeepEqual(got, snap) {
			t.Fatalf("snapshot = %+v, want %+v", got, snap)
		}
	}
	close(ch) // end of stream: the loop must exit and unregister
	<-done
	select {
	case <-unregistered:
	default:
		t.Error("unregister was not called after the stream ended")
	}
}
