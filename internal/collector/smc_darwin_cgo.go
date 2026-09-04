//go:build darwin && cgo

// AppleSMC reader for darwin builds with cgo enabled: CPU-relevant
// temperatures and fan RPMs straight from the SMC, no helper binaries.
// The C layer follows the well-established user-client protocol documented
// by theopolis/smc-fuzzer; Go does the decoding and filtering.
package collector

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation

#include <IOKit/IOKitLib.h>
#include <string.h>

// SMC user-client commands; every call goes through selector 2 with the
// command carried in data8.
#define WT_CMD_READ_BYTES   5
#define WT_CMD_READ_INDEX   8
#define WT_CMD_READ_KEYINFO 9

typedef struct {
	char   major;
	char   minor;
	char   build;
	char   reserved[1];
	UInt16 release;
} wtSMCVers;

typedef struct {
	UInt16 version;
	UInt16 length;
	UInt32 cpuPLimit;
	UInt32 gpuPLimit;
	UInt32 memPLimit;
} wtSMCLimit;

typedef struct {
	UInt32 dataSize;
	UInt32 dataType;
	char   dataAttributes;
} wtSMCKeyInfo;

typedef struct {
	UInt32       key;
	wtSMCVers    vers;
	wtSMCLimit   limit;
	wtSMCKeyInfo info;
	char         result;
	char         status;
	char         data8;
	UInt32       data32;
	UInt8        bytes[32];
} wtSMCKeyData;

static io_connect_t wt_smc_conn = 0;

static kern_return_t wt_smc_call(wtSMCKeyData *in, wtSMCKeyData *out) {
	size_t isz = sizeof(wtSMCKeyData), osz = sizeof(wtSMCKeyData);
	return IOConnectCallStructMethod(wt_smc_conn, 2, in, isz, out, &osz);
}

static int wt_smc_open(void) {
	if (wt_smc_conn != 0) {
		return 0; // already open
	}
	// IOMainPort replaces IOMasterPort (macOS 12+); the SDK gate keeps
	// older toolchains compiling.
#if defined(__MAC_12_0) || (defined(MAC_OS_X_VERSION_MIN_REQUIRED) && MAC_OS_X_VERSION_MIN_REQUIRED >= 120000)
	mach_port_t master = MACH_PORT_NULL;
	kern_return_t r = IOMainPort(MACH_PORT_NULL, &master);
#else
	mach_port_t master = MACH_PORT_NULL;
	kern_return_t r = IOMasterPort(MACH_PORT_NULL, &master);
#endif
	if (r != kIOReturnSuccess) {
		return (int)r;
	}
	io_object_t dev = IOServiceGetMatchingService(master, IOServiceMatching("AppleSMC"));
	if (!dev) {
		return 0x900; // no AppleSMC device (VMs)
	}
	r = IOServiceOpen(dev, mach_task_self(), 0, &wt_smc_conn);
	IOObjectRelease(dev);
	if (r != kIOReturnSuccess) {
		return (int)r;
	}
	return 0;
}

// wt_smc_read fetches the value of the 4-character key. size is
// in/out (buffer size / actual size), type4 receives the data-type
// four-character code (NUL-terminated).
static int wt_smc_read(const char *key4, UInt8 *out, UInt32 *size, char *type4) {
	wtSMCKeyData in, outd;
	memset(&in, 0, sizeof in);
	memset(&outd, 0, sizeof outd);
	in.key = ((UInt32)(unsigned char)key4[0] << 24) |
		((UInt32)(unsigned char)key4[1] << 16) |
		((UInt32)(unsigned char)key4[2] << 8) |
		(UInt32)(unsigned char)key4[3];

	in.data8 = WT_CMD_READ_KEYINFO;
	kern_return_t r = wt_smc_call(&in, &outd);
	if (r != kIOReturnSuccess) {
		return (int)r;
	}
	UInt32 want = outd.info.dataSize;
	type4[0] = (char)((outd.info.dataType >> 24) & 0xff);
	type4[1] = (char)((outd.info.dataType >> 16) & 0xff);
	type4[2] = (char)((outd.info.dataType >> 8) & 0xff);
	type4[3] = (char)(outd.info.dataType & 0xff);
	type4[4] = 0;

	UInt32 buf = *size;
	*size = want;
	if (want > buf) {
		want = buf;
	}

	in.info.dataSize = *size > 32 ? 32 : *size;
	in.data8 = WT_CMD_READ_BYTES;
	r = wt_smc_call(&in, &outd);
	if (r != kIOReturnSuccess) {
		return (int)r;
	}
	memcpy(out, outd.bytes, want);
	return 0;
}

static int wt_smc_key_count(UInt32 *count) {
	UInt8 buf[4];
	UInt32 size = 4;
	char type[5];
	int r = wt_smc_read("#KEY", buf, &size, type);
	if (r != 0) {
		return r;
	}
	*count = ((UInt32)buf[0] << 24) | ((UInt32)buf[1] << 16) |
		((UInt32)buf[2] << 8) | (UInt32)buf[3];
	return 0;
}

static int wt_smc_key_at(UInt32 idx, char *key4) {
	wtSMCKeyData in, outd;
	memset(&in, 0, sizeof in);
	memset(&outd, 0, sizeof outd);
	in.data8 = WT_CMD_READ_INDEX;
	in.data32 = idx;
	kern_return_t r = wt_smc_call(&in, &outd);
	if (r != kIOReturnSuccess) {
		return (int)r;
	}
	key4[0] = (char)((outd.key >> 24) & 0xff);
	key4[1] = (char)((outd.key >> 16) & 0xff);
	key4[2] = (char)((outd.key >> 8) & 0xff);
	key4[3] = (char)(outd.key & 0xff);
	key4[4] = 0;
	return 0;
}
*/
import "C"

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"unsafe"
)

var (
	smcOnce   sync.Once
	smcOpenOK bool
)

// smcReady opens the AppleSMC connection exactly once and reports
// whether it succeeded. All SMC calls are serialized by the collector's
// slow-metric mutex.
func smcReady() bool {
	smcOnce.Do(func() {
		smcOpenOK = C.wt_smc_open() == 0
	})
	return smcOpenOK
}

// smcRead fetches a key's raw bytes and data type.
func smcRead(key string) (typ string, data []byte, err error) {
	if len(key) != 4 {
		return "", nil, fmt.Errorf("smc: key %q must be 4 chars", key)
	}
	var (
		k      = []byte(key)
		buf    [32]C.UInt8
		size   = C.UInt32(32)
		typBuf [5]C.char
	)
	if rc := C.wt_smc_read((*C.char)(unsafe.Pointer(&k[0])), &buf[0], &size, &typBuf[0]); rc != 0 {
		return "", nil, fmt.Errorf("smc: read %s: 0x%x", key, uint32(rc))
	}
	n := int(size)
	if n > 32 {
		n = 32
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(buf[i])
	}
	return C.GoString(&typBuf[0]), out, nil
}

// smcKeyCount reports how many keys the SMC exposes.
func smcKeyCount() (int, error) {
	var n C.UInt32
	if rc := C.wt_smc_key_count(&n); rc != 0 {
		return 0, fmt.Errorf("smc: key count: 0x%x", uint32(rc))
	}
	return int(n), nil
}

// smcKeyAt returns the 4-character key at index i.
func smcKeyAt(i int) (string, error) {
	var key [5]C.char
	if rc := C.wt_smc_key_at(C.UInt32(i), &key[0]); rc != 0 {
		return "", fmt.Errorf("smc: key at %d: 0x%x", i, uint32(rc))
	}
	return C.GoString(&key[0]), nil
}

// platformTemps enumerates SMC temperature keys and returns CPU-relevant
// readings. Two families carry CPU silicon temperatures: TPD* floats on
// Apple Silicon (P-core cluster) and Tp*/Te*/TC<n>* sp78 keys on older
// machines (cores and package). Skin (Ts*), GPU (Tg*/TG*), battery
// (TB*) and power-stage (TCM*/TCH*) sensors are deliberately excluded.
func platformTemps(ctx context.Context) []Sensor {
	if !smcReady() {
		return nil
	}
	total, err := smcKeyCount()
	if err != nil || total <= 0 || total > 8192 {
		return nil
	}
	var out []Sensor
	for i := 0; i < total; i++ {
		key, err := smcKeyAt(i)
		if err != nil || key[0] != 'T' {
			continue
		}
		cpu := key[:3] == "TPD" && len(key) == 4 || // Apple Silicon P-cores
			len(key) == 4 && key[1] == 'p' || // P cores, older chips
			len(key) == 4 && key[1] == 'e' || // E cores
			len(key) == 4 && key[1] == 'C' && key[2] >= '0' && key[2] <= '9' // Intel core/package
		if !cpu {
			continue
		}
		typ, data, err := smcRead(key)
		if err != nil {
			continue
		}
		t := smcTemperature(typ, data)
		if t <= 0 || t > 110 || math.IsNaN(t) { // implausible readings
			continue
		}
		out = append(out, Sensor{Name: key, TempC: t})
	}
	return out
}

// smcTemperature decodes the SMC fixed-point and float temperature
// formats: sp78 (signed 7.8 fixed) and "flt " (little-endian float32).
func smcTemperature(typ string, data []byte) float64 {
	switch {
	case typ == "sp78" && len(data) >= 2:
		return float64(int8(data[0])) + float64(data[1])/256.0
	case typ == "flt " && len(data) >= 4:
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(data)))
	default:
		return 0
	}
}

// readFans reports every SMC fan's current RPM (keys FNum, F<i>Ac with
// fpe2 fixed-point values). Some machines do not expose FNum, so the
// first few fan-current keys are probed regardless.
func readFans() []Fan {
	if !smcReady() {
		return nil
	}
	n := 0
	if typ, data, err := smcRead("FNum"); err == nil && len(data) >= 1 && (typ == "ui8 " || typ == "ui8") {
		n = int(data[0])
	}
	if n == 0 {
		n = 4 // probe fallback
	}
	if n > 8 {
		n = 8
	}
	var fans []Fan
	for i := 0; i < n; i++ {
		typ, data, err := smcRead(fmt.Sprintf("F%dAc", i))
		if err != nil || typ != "fpe2" || len(data) < 2 {
			continue
		}
		rpm := float64(uint16(data[0])<<6 | uint16(data[1])>>2) // fpe2: 2 frac bits
		if rpm <= 0 {
			continue
		}
		fans = append(fans, Fan{Name: fmt.Sprintf("FAN%d", i), RPM: rpm})
	}
	return fans
}
