//go:build windows

package collector

import (
	"golang.org/x/sys/windows"

	"github.com/ebitengine/purego"
)

// nvmlLibName is the NVML DLL name; NewLazySystemDLL searches the usual
// loader locations.
const nvmlLibName = "nvml.dll"

// nvmlSym binds one NVML export to fn using LoadLibrary +
// GetProcAddress.
func nvmlSym(fn any, symbol string) error {
	dll := windows.NewLazySystemDLL(nvmlLibName)
	proc := dll.NewProc(symbol)
	if err := proc.Find(); err != nil {
		return err
	}
	purego.RegisterFunc(fn, proc.Addr())
	return nil
}
