// Package remote implements wrongtop's read-only remote monitoring
// protocol: a token-authenticated TCP handshake followed by a stream of
// length-prefixed JSON collector snapshots. Clients only ever receive
// data — the protocol carries no commands by design.
package remote

import (
	"context"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/wrongstack/wrongtop/internal/collector"
)

// DefaultPort is used by serve and connect when none is specified.
const DefaultPort = 61234

// Protocol is the protocol identifier verified in the handshake.
const Protocol = "WRONGTOP/1"

// MaxFrame caps one frame's payload; snapshots stay orders of magnitude
// below this even on busy machines.
const MaxFrame = 16 << 20

// Auth is the first frame a client sends after connecting.
type Auth struct {
	Token string `json:"token"`
}

// Hello is the first frame the server sends after successful auth.
type Hello struct {
	Protocol  string `json:"protocol"`
	Version   string `json:"version,omitempty"`
	Hostname  string `json:"hostname,omitempty"`
	RefreshMS int    `json:"refresh_ms"`
}

// WriteFrame writes v as one 4-byte big-endian length-prefixed JSON
// frame.
func WriteFrame(w io.Writer, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var head [4]byte
	binary.BigEndian.PutUint32(head[:], uint32(len(data)))
	if _, err := w.Write(head[:]); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// ReadFrame reads one length-prefixed JSON frame into v. Payloads above
// max bytes are rejected before allocation.
func ReadFrame(r io.Reader, max int, v any) error {
	var head [4]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(head[:])
	if n > uint32(max) {
		return fmt.Errorf("frame of %d bytes exceeds the %d byte limit", n, max)
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Authorize reports whether the provided token matches the expected one,
// comparing in constant time. An empty expected token never matches, so
// a misconfigured server refuses everyone instead of anyone.
func Authorize(provided, expected string) bool {
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

// Client is the TUI side of the protocol: dial, authenticate, then pull
// snapshots one by one.
type Client struct {
	conn  net.Conn
	Hello Hello
}

// Dial connects to addr, performs the token handshake and returns a
// client ready to stream snapshots.
func Dial(ctx context.Context, addr, token, version string) (*Client, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	// Bound the handshake by the caller's context: closing the conn
	// unblocks a stalled auth write or hello read, so a deadline or
	// cancel still fires after the TCP dial has succeeded. stop() runs
	// before return, so a live post-handshake stream is never closed.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	c := &Client{conn: conn}
	if err := WriteFrame(conn, Auth{Token: token}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("sending auth: %w", err)
	}
	if err := ReadFrame(conn, MaxFrame, &c.Hello); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("handshake failed (bad token?): %w", err)
	}
	if c.Hello.Protocol != Protocol {
		_ = conn.Close()
		return nil, fmt.Errorf("server speaks %q, we speak %q", c.Hello.Protocol, Protocol)
	}
	return c, nil
}

// Next blocks until the next snapshot arrives. It unblocks with an error
// when Close cancels the connection.
func (c *Client) Next() (collector.Snapshot, error) {
	var snap collector.Snapshot
	if err := ReadFrame(c.conn, MaxFrame, &snap); err != nil {
		return snap, err
	}
	sanitizeSnapshot(&snap)
	return snap, nil
}

// sanitizeSnapshot strips terminal escape initiators from every string
// the wire carries. Snapshot strings are rendered into styled output —
// process names, hostnames, sensor labels, alert text — and a hostile
// or compromised serve peer must not be able to smuggle terminal
// commands (screen clears, title writes, altscreen switches) into the
// client's terminal: the protocol "carries no commands by design", and
// escape sequences are commands.
func sanitizeSnapshot(s *collector.Snapshot) {
	strip := func(sp *string) { *sp = sanitizeString(*sp) }
	strip(&s.Host.Hostname)
	strip(&s.Host.OS)
	strip(&s.Host.Platform)
	strip(&s.Host.Kernel)
	strip(&s.Host.Arch)
	for i := range s.Procs {
		strip(&s.Procs[i].Name)
		strip(&s.Procs[i].User)
		strip(&s.Procs[i].State)
	}
	for i := range s.Sensors {
		strip(&s.Sensors[i].Name)
	}
	for i := range s.Fans {
		strip(&s.Fans[i].Name)
	}
	for i := range s.GPUs {
		strip(&s.GPUs[i].Name)
	}
	for i := range s.Disks {
		strip(&s.Disks[i].Device)
		strip(&s.Disks[i].Mountpoint)
		strip(&s.Disks[i].FSType)
	}
	for i := range s.DiskIOs {
		strip(&s.DiskIOs[i].Name)
	}
	for i := range s.Nets {
		strip(&s.Nets[i].Name)
	}
	for i := range s.Conns {
		strip(&s.Conns[i].Local)
		strip(&s.Conns[i].Remote)
		strip(&s.Conns[i].State)
	}
}

// sanitizeString removes the bytes that introduce terminal escape
// sequences: ESC (0x1b) starts CSI/OSC/DCS, and the C1 CSI (U+009B)
// does the same on terminals that accept 8-bit controls. The needle
// must encode U+009B as valid UTF-8: a bare 0x9b byte is invalid UTF-8
// and invisible to ContainsAny (staticcheck SA1011).
func sanitizeString(s string) string {
	if !strings.ContainsAny(s, "\x1b\u009b") {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == 0x1b || r == 0x9b {
			return -1
		}
		return r
	}, s)
}

// Close tears down the connection.
func (c *Client) Close() { _ = c.conn.Close() }
