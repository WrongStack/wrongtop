package remote

import (
	"context"
	"encoding/json"
	"io"
	"net"
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
	defer ln.Close()
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
	defer probe.Close()
	authorize(t, probe, "secret")

	// Second client: send half of the auth frame header so the server
	// blocks reading it, then cancel, then finish the handshake.
	conn := dialRetry(t, addr)
	defer conn.Close()
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
	defer conn.Close()
	authorize(t, conn, "secret")

	// Stop reading: the server's socket buffers back up, handleConn
	// blocks writing, and the 4-slot client channel overflows.
	time.Sleep(7 * time.Second)

	// Drain: buffered frames end with EOF once the server dropped us.
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.Copy(io.Discard, conn); err != nil {
		t.Fatalf("server never dropped the slow client: %v", err)
	}
	cancel()
	awaitServeError(t, errCh)
}

// The tests below drive handleConn directly over net.Pipe for precise
// control over the auth handshake and channel lifecycle.

func TestHandleConnAuthReadError(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleConn(server, Hello{Protocol: Protocol}, "secret",
			func(chan collector.Snapshot) bool { return true },
			func(chan collector.Snapshot) {})
	}()
	client.Close() // hang up before sending auth
	<-done
}

func TestHandleConnBadToken(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
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
	defer server.Close()
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
	client.Close() // the server's hello write must fail
	<-done
}

func TestHandleConnRegisterRefused(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
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
	defer server.Close()
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
	client.Close() // the next snapshot write must fail
	ch := <-chans
	ch <- collector.Snapshot{Time: time.Unix(1000, 0).UTC()}
	<-done
	if !unregistered {
		t.Error("unregister was not called after the write failed")
	}
}

func TestHandleConnStreamsSnapshots(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	defer server.Close()
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
