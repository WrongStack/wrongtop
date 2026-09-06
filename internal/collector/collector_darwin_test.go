//go:build darwin

package collector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKinfoStateCoversEverymacOSState(t *testing.T) {
	for _, tc := range []struct {
		stat int8
		want string
	}{{1, "I"}, {2, "R"}, {3, "S"}, {4, "T"}, {5, "Z"}, {0, "?"}, {42, "?"}} {
		if got := kinfoState(tc.stat); got != tc.want {
			t.Errorf("kinfoState(%d) = %q, want %q", tc.stat, got, tc.want)
		}
	}
}

func TestReadProcSysNameRejectsBogusSysctl(t *testing.T) {
	if _, err := readProcSysName("kern.proc.nonexistent"); err == nil {
		t.Error("bogus sysctl must error")
	}
	sys, err := readProcSys()
	if err != nil || len(sys) == 0 {
		t.Fatalf("live read: %d procs, err=%v", len(sys), err)
	}
	self, ok := sys[int32(os.Getpid())]
	if !ok || self.State == "" || self.Name == "" {
		t.Errorf("self missing or incomplete: %+v", self)
	}
}

func TestCountUsersFromUtmpx(t *testing.T) {
	rec := func(typ uint16, user string) []byte {
		b := make([]byte, utmpxRecordSize)
		copy(b[utmpxUserOffset:utmpxUserOffset+len(user)], user)
		b[utmpxTypeOffset] = byte(typ)
		b[utmpxTypeOffset+1] = byte(typ >> 8)
		return b
	}
	var raw []byte
	raw = append(raw, rec(utmpxUserProcess, "ali")...)
	raw = append(raw, rec(utmpxUserProcess, "ali")...) // duplicate: one user
	raw = append(raw, rec(utmpxUserProcess, "")...)    // empty name: ignored
	raw = append(raw, rec(2, "dead")...)               // not a user process

	dir := t.TempDir()
	path := filepath.Join(dir, "utmpx")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	orig := utmpxPath
	utmpxPath = path
	t.Cleanup(func() { utmpxPath = orig })

	if got := countUsers(); got != 1 {
		t.Errorf("countUsers = %d, want 1", got)
	}
	utmpxPath = filepath.Join(dir, "missing")
	if got := countUsers(); got != 0 {
		t.Errorf("missing database must read as 0 users, got %d", got)
	}
}
