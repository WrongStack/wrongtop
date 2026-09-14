//go:build linux

package collector

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// utmpRecord builds one synthetic 384-byte utmp entry.
func utmpRecord(typ uint16, user string) []byte {
	rec := make([]byte, utmpRecordSize)
	binary.LittleEndian.PutUint16(rec[utmpTypeOffset:], typ)
	copy(rec[utmpUserOffset:utmpUserOffset+utmpUserSize], user)
	return rec
}

// TestCountUsersDistinct drives the utmp reader against a synthetic
// database: USER_PROCESS records count, distinct usernames dedupe,
// boot events and nameless records do not, and a truncated trailing
// record is ignored.
func TestCountUsersDistinct(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "utmp")
	var raw []byte
	raw = append(raw, utmpRecord(7, "alice")...)
	raw = append(raw, utmpRecord(7, "bob")...)
	raw = append(raw, utmpRecord(7, "alice")...) // duplicate user → counted once
	raw = append(raw, utmpRecord(2, "")...)      // boot event, not a login
	raw = append(raw, utmpRecord(7, "")...)      // login with no user name
	raw = append(raw, make([]byte, 100)...)      // truncated tail → ignored
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	orig := utmpPath
	utmpPath = path
	t.Cleanup(func() { utmpPath = orig })

	if n := countUsers(); n != 2 {
		t.Errorf("distinct users: got %d, want 2", n)
	}
}

// TestCountUsersAbsent pins the no-utmp contract: a machine without
// the database reports zero users.
func TestCountUsersAbsent(t *testing.T) {
	orig := utmpPath
	utmpPath = filepath.Join(t.TempDir(), "absent")
	t.Cleanup(func() { utmpPath = orig })

	if n := countUsers(); n != 0 {
		t.Errorf("absent utmp: got %d, want 0", n)
	}
}
