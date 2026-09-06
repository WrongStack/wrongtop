package dockerclient

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

// errWriter fails every write with the given error.
type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

// errReader yields the given data first, then fails with err.
type errReader struct {
	data []byte
	err  error
}

func (r *errReader) Read(p []byte) (int, error) {
	if len(r.data) > 0 {
		n := copy(p, r.data)
		r.data = r.data[n:]
		return n, nil
	}
	return 0, r.err
}

// frame encodes one multiplexed docker log frame.
func frame(stream byte, payload string) []byte {
	out := make([]byte, 8+len(payload))
	out[0] = stream
	binary.BigEndian.PutUint32(out[4:8], uint32(len(payload)))
	copy(out[8:], payload)
	return out
}

func TestCopyLogStreamTTY(t *testing.T) {
	var out bytes.Buffer
	if err := CopyLogStream(&out, strings.NewReader("raw logs"), true); err != nil {
		t.Fatalf("CopyLogStream: %v", err)
	}
	if out.String() != "raw logs" {
		t.Errorf("out = %q, want %q", out.String(), "raw logs")
	}
}

func TestCopyLogStreamTTYWriteError(t *testing.T) {
	boom := errors.New("boom")
	if err := CopyLogStream(errWriter{err: boom}, strings.NewReader("raw logs"), true); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
}

func TestCopyLogStreamDemux(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(1, "out1"))
	in.Write(frame(2, "err2"))
	in.Write(frame(1, "out3"))

	var out bytes.Buffer
	if err := CopyLogStream(&out, &in, false); err != nil {
		t.Fatalf("CopyLogStream: %v", err)
	}
	if out.String() != "out1err2out3" {
		t.Errorf("out = %q, want %q", out.String(), "out1err2out3")
	}
}

func TestCopyLogStreamCleanEnd(t *testing.T) {
	tests := []struct {
		name string
		in   io.Reader
	}{
		{"empty stream", strings.NewReader("")},
		{"truncated header", bytes.NewReader(frame(1, "x")[:3])},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			// The daemon closing mid-header ends the stream without error.
			if err := CopyLogStream(&out, tt.in, false); err != nil {
				t.Fatalf("CopyLogStream: %v", err)
			}
		})
	}
}

func TestCopyLogStreamFrameError(t *testing.T) {
	boom := errors.New("disk on fire")
	short := frame(1, "ab")                     // full 8-byte header...
	binary.BigEndian.PutUint32(short[4:8], 200) // ...but only 2 payload bytes follow
	oversize := frame(1, "")
	binary.BigEndian.PutUint32(oversize[4:8], 1<<30) // claims 1 GiB, no payload
	tests := []struct {
		name string
		in   io.Reader
	}{
		{"truncated payload", bytes.NewReader(short)},
		{"oversize length claim", bytes.NewReader(oversize)},
		{"reader failure", &errReader{data: frame(1, "x"), err: boom}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := CopyLogStream(&out, tt.in, false); err == nil {
				t.Fatal("broken frame accepted")
			}
		})
	}
}
