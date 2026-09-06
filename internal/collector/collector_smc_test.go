//go:build darwin && cgo

package collector

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
)

func TestSMCTemperatureFormats(t *testing.T) {
	if got := smcTemperature("sp78", []byte{0x14, 0x80}); got != 20.5 {
		t.Errorf("sp78 positive: %v", got)
	}
	if got := smcTemperature("sp78", []byte{0xFF, 0x00}); got != -1 {
		t.Errorf("sp78 negative: %v", got)
	}
	if got := smcTemperature("flt ", []byte{0x00, 0x00, 0x30, 0x42}); got != 44 {
		t.Errorf("flt little-endian: %v", got)
	}
	if got := smcTemperature("ui64", []byte{1, 2, 3, 4}); got != 0 {
		t.Errorf("unknown type: %v", got)
	}
	if got := smcTemperature("sp78", []byte{1}); got != 0 {
		t.Errorf("short payload: %v", got)
	}
}

// fakeSMCData answers like the synthetic SMC table.
func fakeSMCData(key string) (string, []byte, error) {
	data := map[string][2]any{
		"TPD1": {"sp78", []byte{0x1E, 0x00}},             // 30.0
		"Tp02": {"flt ", []byte{0xCD, 0xCC, 0x2C, 0x42}}, // 43.2
		"Te03": {"sp78", []byte{0x7F, 0xFF}},             // ~128: dropped as implausible
		"TC0D": {"sp78", []byte{0x00, 0x00}},             // 0: dropped as missing
		"F0Ac": {"fpe2", []byte{0x0F, 0xC0}},             // 1008 rpm
	}
	d, ok := data[key]
	if !ok {
		return "", nil, errors.New("no such key")
	}
	return d[0].(string), d[1].([]byte), nil
}

// fakeSMC installs a synthetic SMC key table: three CPU temps, one
// implausible temp, one fan and one broken read.
func fakeSMC(t *testing.T) {
	t.Helper()
	keys := []string{"TPD1", "Tp02", "Te03", "TC0D", "F0Ac", "ZZZZ"}
	oCount, oAt, oRead := smcKeyCountFn, smcKeyAtFn, smcReadFn
	smcKeyCountFn = func() (int, error) { return len(keys), nil }
	smcKeyAtFn = func(i int) (string, error) {
		if i < 0 || i >= len(keys) {
			return "", errors.New("out of range")
		}
		return keys[i], nil
	}
	smcReadFn = fakeSMCData
	smcTempKeys, smcFanKeys, smcDiscoverOnce = nil, nil, sync.Once{}
	t.Cleanup(func() {
		smcKeyCountFn, smcKeyAtFn, smcReadFn = oCount, oAt, oRead
		// force a fresh real discovery on the next platform use
		smcTempKeys, smcFanKeys, smcDiscoverOnce = nil, nil, sync.Once{}
	})
}

func TestSMCDiscoveryAndReaders(t *testing.T) {
	fakeSMC(t)
	ctx := context.Background()

	temps := platformTemps(ctx)
	if len(temps) != 2 {
		t.Fatalf("temps: %+v", temps)
	}
	got := map[string]float64{}
	for _, s := range temps {
		got[s.Name] = s.TempC
	}
	if math.Abs(got["Tp02"]-43.2) > 0.01 || got["TPD1"] != 30 {
		t.Errorf("temps: %+v", temps)
	}
	fans := readFans()
	if len(fans) != 1 || fans[0].Name != "FAN0" || fans[0].RPM != 1008 {
		t.Errorf("fans: %+v", fans)
	}

	// a temp read that fails mid-list skips that key
	smcReadFn = func(key string) (string, []byte, error) {
		if key != "Tp02" {
			return "", nil, errors.New("read failed")
		}
		return "flt ", []byte{0xCD, 0xCC, 0x2C, 0x42}, nil
	}
	smcTempKeys, smcDiscoverOnce = nil, sync.Once{}
	if temps := platformTemps(ctx); len(temps) != 1 || temps[0].Name != "Tp02" {
		t.Errorf("failed read must skip the key: %+v", temps)
	}

	// a raced key errors between enumeration and discovery
	smcKeyAtFn = func(i int) (string, error) {
		list := []string{"TPD1", "Tp02", "Te03", "TC0D", "F0Ac", "ZZZZ"}
		if i == 5 {
			return "", errors.New("gone")
		}
		return list[i], nil
	}
	smcReadFn = fakeSMCData
	smcTempKeys, smcFanKeys, smcDiscoverOnce = nil, nil, sync.Once{}
	smcDiscover()
	// discovery records every key whose type decodes as a temperature;
	// implausible values are filtered at read time
	if len(smcTempKeys) != 4 || len(smcFanKeys) != 1 {
		t.Errorf("discovery after raced key: temps=%v fans=%v", smcTempKeys, smcFanKeys)
	}

	// a zero-rpm fan (spun down) is dropped
	smcReadFn = func(string) (string, []byte, error) { return "fpe2", []byte{0, 0}, nil }
	smcFanKeys, smcDiscoverOnce = []string{"F0Ac"}, sync.Once{}
	if fans := readFans(); len(fans) != 0 {
		t.Errorf("zero-rpm fans must be dropped: %+v", fans)
	}

	// a fan read failure skips the key
	smcReadFn = func(string) (string, []byte, error) { return "", nil, errors.New("gone") }
	if fans := readFans(); len(fans) != 0 {
		t.Errorf("failed fan reads must be skipped: %+v", fans)
	}

	// discovery gives up on an unreadable key count
	smcKeyCountFn = func() (int, error) { return 0, errors.New("closed") }
	smcTempKeys, smcDiscoverOnce = nil, sync.Once{}
	if temps := platformTemps(ctx); temps != nil {
		t.Errorf("failed discovery must report nothing: %+v", temps)
	}
}

func TestSMCRealHardware(t *testing.T) {
	if !smcReady() {
		t.Skip("no SMC on this machine")
	}
	if n, err := smcKeyCount(); err != nil || n <= 0 || n > 8192 {
		t.Fatalf("key count: %d, %v", n, err)
	}
	if key, err := smcKeyAt(0); err != nil || len(key) != 4 {
		t.Fatalf("key at 0: %q, %v", key, err)
	}
	if _, _, err := smcRead("TOOLONG"); err == nil {
		t.Error("malformed key must error")
	}
	if _, _, err := smcRead("ZZZZ"); err != nil {
		t.Errorf("real SMC tolerates unknown keys, got: %v", err)
	}
}
