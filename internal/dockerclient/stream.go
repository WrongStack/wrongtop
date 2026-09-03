package dockerclient

import (
	"encoding/binary"
	"fmt"
	"io"
)

// streamHeaderLen is the docker multiplexed stream frame header size:
// [stream byte][3 pad][4-byte big-endian payload length].
const streamHeaderLen = 8

// CopyLogStream copies a container log stream to w, demultiplexing the
// stdout/stderr frames the daemon prepends when the container has no
// TTY. It returns when the stream ends or ctx's reader is closed.
func CopyLogStream(w io.Writer, r io.Reader, tty bool) error {
	if tty {
		_, err := io.Copy(w, r)
		return err
	}
	hdr := make([]byte, streamHeaderLen)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil // daemon closed the stream
			}
			return err
		}
		n := binary.BigEndian.Uint32(hdr[4:streamHeaderLen])
		if _, err := io.CopyN(w, r, int64(n)); err != nil {
			return fmt.Errorf("log frame: %w", err)
		}
	}
}
