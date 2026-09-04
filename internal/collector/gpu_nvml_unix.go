//go:build linux

package collector

import (
	"fmt"

	"github.com/ebitengine/purego"
)

// nvmlLibName is the NVML shared-object soname; dlopen resolves it
// through the normal loader search path.
const nvmlLibName = "libnvidia-ml.so.1"

// nvmlSym binds one NVML symbol to fn using dlopen + dlsym via purego.
func nvmlSym(fn any, symbol string) error {
	h, err := purego.Dlopen(nvmlLibName, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return fmt.Errorf("loading %s: %w", nvmlLibName, err)
	}
	p, err := purego.Dlsym(h, symbol)
	if err != nil {
		return fmt.Errorf("symbol %s: %w", symbol, err)
	}
	purego.RegisterFunc(fn, p)
	return nil
}
