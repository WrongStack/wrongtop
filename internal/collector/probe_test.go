//go:build darwin && cgo

package collector

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"testing"
	"time"
)

// TestLiveSMCProbe prints live AppleSMC readings on the host. It is a
// manual verification tool for the cgo reader: set WRONGTOP_PROBE_SMC=1
// to enable (GitHub runners are VMs with no SMC and must skip it).
func TestLiveSMCProbe(t *testing.T) {
	if os.Getenv("WRONGTOP_PROBE_SMC") == "" {
		t.Skip("set WRONGTOP_PROBE_SMC=1 to probe live SMC readings")
	}

	t.Logf("smcReady=%v", smcReady())
	if n, err := smcKeyCount(); err != nil {
		t.Logf("key count error: %v", err)
	} else {
		t.Logf("key count=%d", n)
		// dump every temperature-ish key with its decoded value
		for i := 0; i < n; i++ {
			k, err := smcKeyAt(i)
			if err != nil || k[0] != 'T' {
				continue
			}
			typ, data, err := smcRead(k)
			if err != nil {
				continue
			}
			val := ""
			switch {
			case typ == "sp78" && len(data) >= 2:
				val = fmt.Sprintf("%.1f°C", float64(int8(data[0]))+float64(data[1])/256.0)
			case typ == "flt " && len(data) >= 4:
				val = fmt.Sprintf("%.1f°C", math.Float32frombits(binary.LittleEndian.Uint32(data)))
			case typ == "ioft" && len(data) >= 4:
				val = fmt.Sprintf("%.1f°C(ioft)", math.Float32frombits(binary.LittleEndian.Uint32(data)))
			case typ == "fpe2" && len(data) >= 2:
				val = fmt.Sprintf("%.0f", float64(uint16(data[0])<<6|uint16(data[1])>>2))
			}
			t.Logf("T-key %q type=%q %s", k, typ, val)
		}
		for _, fk := range []string{"FNum", "F0Ac", "F1Ac", "F0Mn"} {
			typ, data, err := smcRead(fk)
			t.Logf("fankey %q err=%v type=%q bytes=%v", fk, err, typ, data)
		}
	}

	c := New(time.Second)
	snap := c.Collect(context.Background())

	if len(snap.Sensors) == 0 {
		t.Log("no temperature sensors reported")
	}
	for _, s := range snap.Sensors {
		t.Logf("sensor %-8s %.1f°C", s.Name, s.TempC)
	}
	if len(snap.Fans) == 0 {
		t.Log("no fans reported")
	}
	for _, f := range snap.Fans {
		t.Logf("fan %-8s %.0f rpm", f.Name, f.RPM)
	}
	if b := snap.Battery; b != nil {
		t.Logf("battery %.0f%% charging=%v", b.Percent, b.Charging)
	} else {
		t.Log("no battery reported")
	}
	if len(snap.Sensors) == 0 && len(snap.Fans) == 0 && snap.Battery == nil {
		t.Fatal("probe enabled but nothing reported — SMC reader is broken")
	}
	fmt.Fprintln(os.Stderr) // keep -v output readable
}
