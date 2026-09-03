package collector

import (
	"context"
	"testing"

	"github.com/shirou/gopsutil/v4/process"
)

func BenchmarkProcCalls(b *testing.B) {
	ctx := context.Background()
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		b.Fatal(err)
	}
	// take a sample of 100 processes
	if len(procs) > 100 {
		procs = procs[:100]
	}
	b.Logf("sample size: %d", len(procs))

	b.Run("Times", func(b *testing.B) {
		for range b.N {
			for _, p := range procs {
				p.TimesWithContext(ctx) //nolint:errcheck
			}
		}
	})
	b.Run("Name", func(b *testing.B) {
		for range b.N {
			for _, p := range procs {
				p.NameWithContext(ctx) //nolint:errcheck
			}
		}
	})
	b.Run("MemoryInfo", func(b *testing.B) {
		for range b.N {
			for _, p := range procs {
				p.MemoryInfoWithContext(ctx) //nolint:errcheck
			}
		}
	})
	b.Run("Username", func(b *testing.B) {
		for range b.N {
			for _, p := range procs {
				p.UsernameWithContext(ctx) //nolint:errcheck
			}
		}
	})
	b.Run("Status", func(b *testing.B) {
		for range b.N {
			for _, p := range procs {
				p.StatusWithContext(ctx) //nolint:errcheck
			}
		}
	})
	b.Run("NumThreads", func(b *testing.B) {
		for range b.N {
			for _, p := range procs {
				p.NumThreadsWithContext(ctx) //nolint:errcheck
			}
		}
	})
	b.Run("Ppid", func(b *testing.B) {
		for range b.N {
			for _, p := range procs {
				p.PpidWithContext(ctx) //nolint:errcheck
			}
		}
	})
}
