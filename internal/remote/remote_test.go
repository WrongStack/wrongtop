package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/wrongstack/wrongtop/internal/collector"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	hello := Hello{Protocol: Protocol, Version: "test", Hostname: "box", RefreshMS: 1000}
	if err := WriteFrame(&buf, hello); err != nil {
		t.Fatal(err)
	}
	var got Hello
	if err := ReadFrame(&buf, MaxFrame, &got); err != nil {
		t.Fatal(err)
	}
	if got != hello {
		t.Fatalf("round trip = %+v, want %+v", got, hello)
	}
}

func TestReadFrameRejectsOversize(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, map[string]string{"pad": "x"}); err != nil {
		t.Fatal(err)
	}
	// corrupt the length header to claim a huge payload
	data := buf.Bytes()
	data[0] = 0x7f // ~2 GiB claim in the top length byte
	if err := ReadFrame(bytes.NewReader(data), MaxFrame, new(json.RawMessage)); err == nil {
		t.Fatal("oversize frame accepted")
	}
}

func TestAuthorize(t *testing.T) {
	if Authorize("right", "") {
		t.Error("empty expected token must never match")
	}
	if Authorize("wrong", "right") {
		t.Error("wrong token accepted")
	}
	if !Authorize("right", "right") {
		t.Error("right token rejected")
	}
}

// TestServeEndToEnd dials a live server with a good and a bad token and
// verifies the snapshot stream reaches the good client.
func TestServeEndToEnd(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // Serve binds its own listener on the freed port

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go func() {
		_ = Serve(ctx, Options{Listen: addr, Token: "secret", Refresh: 250 * time.Millisecond})
	}()
	time.Sleep(150 * time.Millisecond) // let the server bind

	// bad token must fail the handshake
	badCtx, badCancel := context.WithTimeout(ctx, 3*time.Second)
	if _, err := Dial(badCtx, addr, "nope", "test"); err == nil {
		t.Error("bad token was accepted")
	}
	badCancel()

	client, err := Dial(ctx, addr, "secret", "test")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	if client.Hello.Protocol != Protocol || client.Hello.RefreshMS != 250 {
		t.Fatalf("hello = %+v", client.Hello)
	}

	snap, err := client.Next()
	if err != nil {
		t.Fatalf("first snapshot: %v", err)
	}
	if snap.Time.IsZero() {
		t.Fatal("snapshot has no timestamp")
	}
	if len(snap.Procs) == 0 {
		t.Error("snapshot carries no processes")
	}
}

// TestServeRefusesNoToken pins the safe default: no token, no server.
func TestServeRefusesNoToken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	err = Serve(context.Background(), Options{Listen: addr, Token: ""})
	if err == nil {
		t.Fatal("Serve started without a token")
	}
}

// TestSnapshotJSONShape sanity-checks the wire payload is a JSON object
// with the fields scripts rely on.
func TestSnapshotJSONShape(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, collector.Snapshot{}); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(buf.Bytes()[4:], &raw); err != nil { // skip framing
		t.Fatalf("payload is not JSON: %v", err)
	}
	if _, ok := raw["time"]; !ok {
		t.Error("snapshot JSON missing time field")
	}
}
