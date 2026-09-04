//go:build linux || windows

package collector

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// NVIDIA's NVML driver library is loaded at runtime: no build-time
// dependency, no helper process and no command execution. On machines
// without the driver the library is simply absent and GPU monitoring
// stays off. Library loading is per-OS (dlfcn vs LoadLibrary).

var (
	nvmlOnce  sync.Once
	nvmlOK    bool
	nvmlErr   error
	nvmlInit  func() uint32
	nvmlCount func(*uint32) uint32
	nvmlHnd   func(uint32, *uintptr) uint32
	nvmlName  func(uintptr, unsafe.Pointer, uint32) uint32
	nvmlUtil  func(uintptr, *nvmlUtilization) uint32
	nvmlMem   func(uintptr, *nvmlMemory) uint32
	nvmlTemp  func(uintptr, uint32, *uint32) uint32
)

// nvmlUtilization mirrors nvmlUtilization_t.
type nvmlUtilization struct{ GPU, Memory uint32 }

// nvmlMemory mirrors nvmlMemory_t including the reserved tail newer
// headers added, so the driver can never write past our allocation.
type nvmlMemory struct {
	Total, Free, Used uint64
	Reserved          [3]uint64
}

// nvmlLoad resolves the NVML symbols once; it reports whether the
// library and every symbol we need are present.
func nvmlLoad() bool {
	nvmlOnce.Do(func() {
		sym := func(fn any, symbol string) error {
			return nvmlSym(fn, symbol)
		}
		for _, e := range []error{
			sym(&nvmlInit, "nvmlInit_v2"),
			sym(&nvmlCount, "nvmlDeviceGetCount_v2"),
			sym(&nvmlHnd, "nvmlDeviceGetHandleByIndex_v2"),
			sym(&nvmlName, "nvmlDeviceGetName"),
			sym(&nvmlUtil, "nvmlDeviceGetUtilizationRates"),
			sym(&nvmlMem, "nvmlDeviceGetMemoryInfo"),
			sym(&nvmlTemp, "nvmlDeviceGetTemperature"),
		} {
			if e != nil {
				nvmlErr = e
				return
			}
		}
		nvmlOK = true
	})
	return nvmlOK
}

const nvmlTempSensorGPU = 0

// probeGPUs reports every NVIDIA adapter's utilization, VRAM usage and
// temperature through NVML. Returns an error when the driver is missing
// or initialization fails (which puts the collector into back-off).
func probeGPUs(ctx context.Context) ([]GPU, error) {
	_ = ctx // NVML calls are non-blocking
	if !nvmlLoad() {
		return nil, fmt.Errorf("nvml unavailable: %w", nvmlErr)
	}
	if rc := nvmlInit(); rc != 0 {
		return nil, fmt.Errorf("nvmlInit: %d", rc)
	}
	var n uint32
	if rc := nvmlCount(&n); rc != 0 {
		return nil, fmt.Errorf("nvmlDeviceGetCount: %d", rc)
	}
	if n > 16 { //nolint:mnd // sane adapter ceiling
		return nil, errors.New("implausible GPU count")
	}
	out := make([]GPU, 0, n)
	for i := uint32(0); i < n; i++ {
		var dev uintptr
		if rc := nvmlHnd(i, &dev); rc != 0 {
			continue
		}
		g := GPU{Index: int(i)}
		var name [96]byte
		if rc := nvmlName(dev, unsafe.Pointer(&name[0]), uint32(len(name))); rc == 0 {
			g.Name = cstr(&name[0], int64(len(name)))
		}
		var util nvmlUtilization
		if rc := nvmlUtil(dev, &util); rc == 0 {
			g.Util = float64(util.GPU)
		}
		var mem nvmlMemory
		if rc := nvmlMem(dev, &mem); rc == 0 {
			g.MemUsed, g.MemTotal = mem.Used, mem.Total
		}
		var temp uint32
		if rc := nvmlTemp(dev, nvmlTempSensorGPU, &temp); rc == 0 {
			g.TempC = float64(temp)
		}
		out = append(out, g)
	}
	return out, nil
}

// cstr converts a NUL-terminated char buffer into a Go string.
func cstr(p *byte, max int64) string {
	for i := int64(0); i < max; i++ {
		if *(*byte)(unsafe.Add(unsafe.Pointer(p), i)) == 0 {
			return string(unsafe.Slice(p, i))
		}
	}
	return string(unsafe.Slice(p, max))
}
