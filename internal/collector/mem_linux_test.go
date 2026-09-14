//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"testing"
)

// TestZramStatsAggregates drives the zram reader against a synthetic
// sysfs tree: byte totals sum across devices, and unconfigured
// (disksize 0), unparsable, mm_stat-less and empty-mm_stat devices are
// skipped.
func TestZramStatsAggregates(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("zram0/disksize", "8589934592\n") // 8 GiB
	write("zram0/mm_stat", "1048576 204800 0 0\n")
	write("zram1/disksize", "0\n") // unconfigured → skipped
	write("zram1/mm_stat", "999 1 1\n")
	write("zram2/disksize", "4294967296\n") // 4 GiB
	write("zram2/mm_stat", "2048000 512000 0 0\n")
	write("zram3/disksize", "garbage\n")    // unparsable → skipped
	write("zram4/disksize", "1073741824\n") // 1 GiB, no mm_stat → skipped
	write("zram5/disksize", "2147483648\n") // 2 GiB, empty mm_stat → skipped
	write("zram5/mm_stat", "\n")
	write("zram6/disksize", "3221225472\n") // 3 GiB, unparsable mm_stat → skipped
	write("zram6/mm_stat", "abc def\n")

	orig := zramRoot
	zramRoot = root
	t.Cleanup(func() { zramRoot = orig })

	total, used := zramStats()
	if want := uint64(8589934592 + 4294967296); total != want {
		t.Errorf("total: got %d, want %d", total, want)
	}
	if want := uint64(1048576 + 2048000); used != want {
		t.Errorf("used: got %d, want %d", used, want)
	}
}

// TestZramStatsAbsent pins the no-zram contract: a machine without
// compressed swap devices reports zeros.
func TestZramStatsAbsent(t *testing.T) {
	orig := zramRoot
	zramRoot = t.TempDir() // no zram* devices
	t.Cleanup(func() { zramRoot = orig })

	total, used := zramStats()
	if total != 0 || used != 0 {
		t.Errorf("expected zeros without zram, got total=%d used=%d", total, used)
	}
}
