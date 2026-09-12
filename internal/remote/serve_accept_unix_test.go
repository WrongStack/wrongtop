//go:build unix

package remote

import (
	"context"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// countOpenFDs counts this process's open file descriptors.
func countOpenFDs(t *testing.T) int {
	t.Helper()
	for _, dir := range []string{"/dev/fd", "/proc/self/fd"} {
		if entries, err := os.ReadDir(dir); err == nil {
			return len(entries)
		}
	}
	t.Skip("cannot count open file descriptors on this platform")
	return 0
}

// awaitServeAttemptEnd waits for Serve to return at the end of a
// rejected attempt. With the descriptor table deliberately full, a
// cancelled Serve may surface the in-flight accept's EMFILE ("accept:
// too many open files") instead of the clean listener close — both
// outcomes end the attempt, so only a non-accept failure is fatal
// here.
func awaitServeAttemptEnd(t *testing.T, errCh chan error) {
	t.Helper()
	select {
	case err := <-errCh:
		if err != nil && !strings.Contains(err.Error(), "accept:") {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not shut down")
	}
}

// TestServeAcceptError covers the accept-failure branch: the soft file
// descriptor limit is lowered to the current usage plus a little, the
// table is filled back up, and one slot is freed for a client dial so
// that the server's Accept call is the one that runs out of descriptors
// while the context is still alive.
func TestServeAcceptError(t *testing.T) {
	var orig syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &orig); err != nil {
		t.Skipf("getrlimit unavailable: %v", err)
	}
	used := countOpenFDs(t)
	target := used + 48
	if uint64(target) > orig.Cur {
		target = int(orig.Cur)
	}
	if target < used+4 {
		t.Skipf("no fd headroom (limit %d, used %d)", orig.Cur, used)
	}
	rl := orig // Max stays untouched so the restore needs no privileges
	rl.Cur = uint64(target)
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rl); err != nil {
		t.Skipf("setrlimit: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &orig) })

	var held []*os.File
	t.Cleanup(func() {
		for _, f := range held {
			_ = f.Close()
		}
	})
	fillToLimit := func() int {
		n := 0
		for {
			f, err := os.Open(os.DevNull)
			if err != nil {
				return n
			}
			held = append(held, f)
			n++
			if n > 8192 {
				return n
			}
		}
	}

	for attempt := 0; attempt < 5; attempt++ {
		addr := freeListenAddr(t)
		ctx, cancel := context.WithCancel(context.Background())
		errCh := startServe(ctx, t, addr, "accept", 250*time.Millisecond)

		// The probe only gets a hello once Serve is accepting, which
		// also proves the initial collect has finished.
		probe := dialRetry(t, addr)
		authorize(t, probe, "accept")

		fillToLimit()
		if len(held) == 0 {
			// No free slot could be claimed; tear down and retry.
			_ = probe.Close()
			cancel()
			awaitServeAttemptEnd(t, errCh)
			continue
		}
		_ = held[0].Close() // free exactly one slot
		held = held[1:]
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			// The slot was taken by something transient; retry.
			_ = probe.Close()
			cancel()
			awaitServeAttemptEnd(t, errCh)
			continue
		}
		select {
		case err := <-errCh:
			_ = conn.Close()
			_ = probe.Close()
			cancel()
			if err == nil || !strings.Contains(err.Error(), "accept:") {
				t.Fatalf("Serve error = %v, want an accept failure", err)
			}
			return
		case <-time.After(3 * time.Second):
			// Accept got the freed slot instead; tear down and retry.
			_ = conn.Close()
			_ = probe.Close()
			cancel()
			awaitServeAttemptEnd(t, errCh)
		}
	}
	t.Fatal("could not trigger an accept failure after 5 attempts")
}
