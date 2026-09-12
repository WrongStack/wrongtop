package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/wrongstack/wrongtop/internal/collector"
)

// errWriter fails every write with the given error.
type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

// startFakeServer accepts TCP connections on a fresh loopback port and
// hands each one to handle. All goroutines it starts are shut down via
// t.Cleanup before the test returns.
func startFakeServer(t *testing.T, handle func(conn net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(conn)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	return ln.Addr().String()
}

func TestWriteFrameMarshalError(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, make(chan int)); err == nil {
		t.Fatal("unmarshalable value accepted")
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes despite marshal failure", buf.Len())
	}
}

func TestWriteFrameWriteError(t *testing.T) {
	boom := errors.New("boom")
	if err := WriteFrame(errWriter{err: boom}, Hello{Protocol: Protocol}); !errors.Is(err, boom) {
		t.Fatalf("write error = %v, want %v", err, boom)
	}
}

func TestReadFrameErrors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"empty reader", nil},
		{"short header", []byte{0, 0}},
		{"short payload", []byte{0, 0, 0, 10, 'a', 'b', 'c'}},
		{"invalid json", []byte{0, 0, 0, 3, 'a', 'b', 'c'}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ReadFrame(bytes.NewReader(tt.data), MaxFrame, new(Hello)); err == nil {
				t.Fatal("broken frame accepted")
			}
		})
	}
}

// TestDialConnectionRefused covers the dial failure branch.
func TestDialConnectionRefused(t *testing.T) {
	addr := freeListenAddr(t)
	if _, err := Dial(context.Background(), addr, "token", "test"); err == nil {
		t.Fatal("dialed a closed port")
	}
}

// TestDialAuthWriteFails pins the "sending auth" branch: the fake server
// reads the 4-byte frame header and then resets the connection, which
// aborts the client's (deliberately oversized) payload write.
func TestDialAuthWriteFails(t *testing.T) {
	addr := startFakeServer(t, func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		var head [4]byte
		if _, err := io.ReadFull(conn, head[:]); err != nil {
			return
		}
		if tc, ok := conn.(*net.TCPConn); ok {
			_ = tc.SetLinger(0) // close sends RST, aborting the client's blocked write
		}
	})
	// The token is far larger than any socket buffer, so the auth payload
	// write is still pending when the RST arrives and must fail.
	hugeToken := strings.Repeat("t", 32<<20)
	_, err := Dial(context.Background(), addr, hugeToken, "test")
	if err == nil || !strings.Contains(err.Error(), "sending auth") {
		t.Fatalf("err = %v, want a sending-auth failure", err)
	}
}

// TestDialWrongProtocol covers the protocol-version mismatch branch.
func TestDialWrongProtocol(t *testing.T) {
	addr := startFakeServer(t, func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		var auth Auth
		if err := ReadFrame(conn, 4<<10, &auth); err != nil {
			return
		}
		if err := WriteFrame(conn, Hello{Protocol: "OTHER/9", RefreshMS: 250}); err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, conn) // wait for the client to hang up
	})
	_, err := Dial(context.Background(), addr, "token", "test")
	if err == nil || !strings.Contains(err.Error(), "server speaks") {
		t.Fatalf("err = %v, want a protocol mismatch", err)
	}
}

// TestNextAfterClose covers the Next error branch: a well-formed server
// that closes the stream after the handshake makes the next read fail.
func TestNextAfterClose(t *testing.T) {
	addr := startFakeServer(t, func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		var auth Auth
		if err := ReadFrame(conn, 4<<10, &auth); err != nil {
			return
		}
		if auth.Token != "secret" {
			return
		}
		_ = WriteFrame(conn, Hello{Protocol: Protocol, RefreshMS: 250})
	})
	client, err := Dial(context.Background(), addr, "secret", "test")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	if _, err := client.Next(); err == nil {
		t.Fatal("Next on a closed stream returned no error")
	}
}

// TestNextSanitizesEscapeSequences pins the decode-boundary sanitization:
// snapshot strings are rendered into styled terminal output, so a hostile
// or compromised serve peer must not be able to smuggle terminal escape
// sequences (screen clears, title writes, altscreen switches) into the
// client — the protocol "carries no commands by design", and escape
// sequences are commands. The data around stripped initiators and the
// numeric fields must survive.
func TestNextSanitizesEscapeSequences(t *testing.T) {
	esc := collector.Snapshot{
		Time: time.Now(),
		Host: collector.Host{
			Hostname: "\x1b]0;pwned\x07\x1b[?1049l",
			OS:       "\u009b5n", // C1 CSI as valid UTF-8, the JSON-realistic form
			Platform: "debian 12",
		},
		Procs: []collector.Proc{{
			PID:   42,
			PPID:  1,
			Name:  "\x1b]0;pwned\x07\x1b[2Jevil-proc",
			User:  "root",
			State: "R",
			CPU:   50,
			Mem:   12.5,
			RSS:   4096,
		}},
		Sensors: []collector.Sensor{{Name: "\x1b[?25lTC0D", TempC: 55}},
		Conns:   []collector.Conn{{Local: "10.0.0.1:80", Remote: "[::]:*", State: "ESTABLISHED"}},
	}

	addr := startFakeServer(t, func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		var auth Auth
		if err := ReadFrame(conn, 4<<10, &auth); err != nil || auth.Token != "secret" {
			return
		}
		_ = WriteFrame(conn, Hello{Protocol: Protocol, RefreshMS: 250})
		for {
			if err := WriteFrame(conn, esc); err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	client, err := Dial(context.Background(), addr, "secret", "test")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	snap, err := client.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}

	if rendered := fmt.Sprintf("%+v", snap); strings.ContainsAny(rendered, "\x1b\u009b") {
		t.Fatalf("decoded snapshot carries escape initiators from the wire: %q", rendered)
	}
	if !strings.Contains(snap.Host.Hostname, "pwned") {
		t.Errorf("hostname content lost in sanitization: %q", snap.Host.Hostname)
	}
	if len(snap.Procs) != 1 || snap.Procs[0].PID != 42 || snap.Procs[0].CPU != 50 || snap.Procs[0].RSS != 4096 {
		t.Errorf("numeric proc data must be preserved: %+v", snap.Procs)
	}
	if !strings.Contains(snap.Procs[0].Name, "evil-proc") {
		t.Errorf("proc name content lost in sanitization: %q", snap.Procs[0].Name)
	}
}

// TestDialHandshakeDeadline pins the handshake context bound: a server
// that accepts and consumes the auth frame but never answers must not
// block Dial past the caller's deadline — `wrongtop connect` wraps Dial
// in a 10s WithTimeout that has to be able to fire.
func TestDialHandshakeDeadline(t *testing.T) {
	addr := startFakeServer(t, func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		_, _ = io.Copy(io.Discard, conn) // stall: drain, never answer
	})
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	type dialResult struct{ err error }
	done := make(chan dialResult, 1)
	go func() {
		_, err := Dial(ctx, addr, "token", "test")
		done <- dialResult{err: err}
	}()
	select {
	case res := <-done:
		if res.err == nil {
			t.Fatal("Dial succeeded against a silent server")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Dial ignored the context deadline: still blocked 5s after a 250ms deadline")
	}
}

// TestDialCancelledContext covers the no-deadline boundary: an
// already-cancelled context must fail Dial immediately, not dial and
// then stream on a cancelled context.
func TestDialCancelledContext(t *testing.T) {
	addr := startFakeServer(t, func(conn net.Conn) {
		defer func() { _ = conn.Close() }()
		_, _ = io.Copy(io.Discard, conn)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Dial(ctx, addr, "token", "test"); err == nil {
		t.Fatal("Dial succeeded with an already-cancelled context")
	}
}
