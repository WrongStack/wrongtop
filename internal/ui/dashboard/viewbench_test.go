package dashboard

import (
	"testing"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/theme"
)

func benchModel(b *testing.B, w, h int) *Model {
	b.Helper()
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.SetSize(w, h)
	for range 2 { // graphs need 2+ samples
		m.Update(collector.SnapshotMsg{Snap: fakeSnapshot()})
	}
	m.View() // prime the cache
	return m
}

// BenchmarkDashboardViewCold measures a full rebuild (one snapshot per
// iteration); BenchmarkDashboardViewWarm measures the cached path that
// every non-snapshot event (mouse, keys) hits.
func BenchmarkDashboardViewCold(b *testing.B) {
	m := benchModel(b, 200, 50)
	b.ResetTimer()
	for range b.N {
		m.viewCache = ""
		_ = m.View()
	}
}

func BenchmarkDashboardViewWarm(b *testing.B) {
	m := benchModel(b, 200, 50)
	b.ResetTimer()
	for range b.N {
		_ = m.View()
	}
}
