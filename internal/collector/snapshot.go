// Package collector polls system metrics into serializable snapshots.
// Snapshot fields are plain data so the struct can later cross a wire
// unchanged (remote monitoring is planned post-v1).
package collector

import "time"

// SnapshotMsg wraps a Snapshot for the bubbletea event loop.
type SnapshotMsg struct {
	Snap Snapshot
}

// Snapshot is one sample of everything wrongtop shows.
type Snapshot struct {
	Time time.Time `json:"time"`
	Host Host      `json:"host"`
	CPU  CPU       `json:"cpu"`
	Mem  Mem       `json:"mem"`
}

// Host holds slow-changing system identity plus volatile load figures.
type Host struct {
	Hostname string        `json:"hostname"`
	OS       string        `json:"os"`
	Platform string        `json:"platform"`
	Kernel   string        `json:"kernel"`
	Arch     string        `json:"arch"`
	Uptime   time.Duration `json:"uptime"`
	Procs    int           `json:"procs"`
	Load     [3]float64    `json:"load"`
}

// CPU is one CPU sample. Percent is the aggregate across all logical
// cores; Cores holds one percentage per logical core.
type CPU struct {
	Percent float64   `json:"percent"`
	Cores   []float64 `json:"cores"`
}

// Mem is one memory sample.
type Mem struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Available   uint64  `json:"available"`
	Percent     float64 `json:"percent"`
	SwapTotal   uint64  `json:"swap_total"`
	SwapUsed    uint64  `json:"swap_used"`
	SwapPercent float64 `json:"swap_percent"`
}

