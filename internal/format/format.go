// Package format provides small formatting helpers shared across the UI.
package format

import (
	"fmt"
	"time"
)

// Bytes renders a byte count in binary units, e.g. "9.9 GiB".
func Bytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Rate renders a bytes-per-second figure in decimal units, e.g. "12.4 MB/s".
func Rate(bps float64) string {
	const unit = 1000
	if bps < unit {
		return fmt.Sprintf("%.0f B/s", bps)
	}
	div, exp := float64(unit), 0
	// exp is capped at the last unit: rate values can arrive straight
	// off the remote wire, so any finite number — and +Inf — must clamp
	// into "KMGTPE" instead of indexing past it (or looping forever).
	for n := bps / unit; n >= unit && exp < len("KMGTPE")-1; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cb/s", bps/div, "KMGTPE"[exp])
}

// RateFixed renders a rate right-padded to a fixed cell width, so live
// chip and head figures don't jitter sideways as their magnitude crosses
// unit boundaries ("986 B/s" ↔ "12.4 MB/s").
func RateFixed(w int, bps float64) string {
	s := Rate(bps)
	for len(s) < w { // rates are pure ASCII: len == display width
		s += " "
	}
	return s
}

// BytesCompact renders a byte count in binary units like Bytes, but
// drops the decimal once the value reaches 100 of its unit, so wide
// counts stay inside narrow table columns ("9.9 GiB", "128 GiB").
func BytesCompact(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	v := float64(n) / float64(div)
	if v >= 100 {
		return fmt.Sprintf("%.0f %ciB", v, "KMGTPE"[exp])
	}
	return fmt.Sprintf("%.1f %ciB", v, "KMGTPE"[exp])
}

// CPUPct renders a CPU percentage for a fixed 5-cell column: a busy
// multi-thread process can exceed 100 (up to cores×100), so values ≥100
// drop the decimal and values ≥1000 clamp — the column can never push a
// table row out of alignment.
func CPUPct(v float64) string {
	switch {
	case v >= 1000:
		return " 999+"
	case v >= 100:
		return fmt.Sprintf("%5.0f", v)
	default:
		return fmt.Sprintf("%5.1f", v)
	}
}

// Uptime renders a duration compactly, e.g. "3d 4h", "12m" or "45s".
func Uptime(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
