package canvas

import (
	"testing"

	"charm.land/lipgloss/v2"
)

var benchRamp = Ramp{"#00ff00", "#ffff00", "#ff0000"}

func BenchmarkGradientBarCached(b *testing.B) {
	st := lipgloss.NewStyle()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		GradientBar(24, 0.42, benchRamp, st) // stable value: cache hit path
	}
}

func BenchmarkGradientBarChurn(b *testing.B) {
	st := lipgloss.NewStyle()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		GradientBar(160, float64(i%1001)/1000, benchRamp, st) // uncached width + churn
	}
}

func BenchmarkDualBarCached(b *testing.B) {
	a := Ramp{"#00ff00", "#00ffff"}
	c := Ramp{"#0000ff", "#00ffff"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		DualBar(20, 0.7, a, c)
	}
}

func BenchmarkGraphView(b *testing.B) {
	g := New(80, 3, benchRamp.Colors())
	for i := range 160 {
		g.Push(float64(i%100) / 100)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = g.View()
	}
}
