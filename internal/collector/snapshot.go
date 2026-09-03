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
	Time    time.Time  `json:"time"`
	Host    Host       `json:"host"`
	CPU     CPU        `json:"cpu"`
	Mem     Mem        `json:"mem"`
	Sensors []Sensor   `json:"sensors,omitempty"`
	Battery *Battery   `json:"battery,omitempty"`
	Procs   []Proc     `json:"procs,omitempty"`
	Disks   []Disk     `json:"disks,omitempty"`
	DiskIOs []DiskIO   `json:"disk_ios,omitempty"`
	Nets    []NetIface `json:"nets,omitempty"`
}

// Proc is one process sample. CPU is an interval delta (htop-style) and
// can exceed 100 for multi-threaded processes.
type Proc struct {
	PID     int32   `json:"pid"`
	PPID    int32   `json:"ppid"`
	Name    string  `json:"name"`
	CPU     float64 `json:"cpu"`
	Mem     float64 `json:"mem"` // percent of physical memory
	RSS     uint64  `json:"rss"`
	User    string  `json:"user"`
	State   string  `json:"state"` // single letter: R S Z T I ...
	Threads int32   `json:"threads"`
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
// cores; Cores holds one percentage per logical core. FreqMHz is the
// current average clock and is 0 when the platform does not report it.
type CPU struct {
	Percent float64   `json:"percent"`
	FreqMHz float64   `json:"freq_mhz,omitempty"`
	Cores   []float64 `json:"cores"`
}

// Sensor is one CPU-relevant temperature reading.
type Sensor struct {
	Name  string  `json:"name"`
	TempC float64 `json:"temp_c"`
}

// Battery is the main battery state; a nil pointer means the machine has
// no battery (or the platform does not report one).
type Battery struct {
	Percent  float64 `json:"percent"`
	Charging bool    `json:"charging"`
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

// Disk is one mounted filesystem.
type Disk struct {
	Device     string  `json:"device"`
	Mountpoint string  `json:"mountpoint"`
	FSType     string  `json:"fs_type"`
	Total      uint64  `json:"total"`
	Used       uint64  `json:"used"`
	Free       uint64  `json:"free"`
	Percent    float64 `json:"percent"`
}

// DiskIO is a per-device I/O rate sample (interval delta).
type DiskIO struct {
	Name        string  `json:"name"`
	ReadBytes   float64 `json:"read_bytes_per_s"`
	WriteBytes  float64 `json:"write_bytes_per_s"`
	ReadIOPS    float64 `json:"read_iops"`
	WriteIOPS   float64 `json:"write_iops"`
	BusyPercent float64 `json:"busy_percent"`
}

// NetIface is a per-interface traffic sample (rates are interval deltas).
type NetIface struct {
	Name          string  `json:"name"`
	RxRate        float64 `json:"rx_rate"` // bytes/s
	TxRate        float64 `json:"tx_rate"` // bytes/s
	RxRatePackets float64 `json:"rx_pps"`
	TxRatePackets float64 `json:"tx_pps"`
	RxTotal       uint64  `json:"rx_total"`
	TxTotal       uint64  `json:"tx_total"`
}
