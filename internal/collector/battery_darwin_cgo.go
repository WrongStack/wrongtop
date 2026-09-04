//go:build darwin && cgo

package collector

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation

#include <IOKit/ps/IOPowerSources.h>
#include <IOKit/ps/IOPSKeys.h>

// wt_battery reads the first power source that carries a capacity key
// (the internal battery or a UPS). Desktop Macs have none and get a
// non-zero result. Returns 0 on success.
static int wt_battery(double *percent, int *charging) {
	CFTypeRef blob = IOPSCopyPowerSourcesInfo();
	if (!blob) {
		return 1;
	}
	CFArrayRef list = IOPSCopyPowerSourcesList(blob);
	if (!list) {
		CFRelease(blob);
		return 1;
	}
	int found = 1;
	for (CFIndex i = 0; i < CFArrayGetCount(list); i++) {
		CFTypeRef ps = IOPSGetPowerSourceDescription(blob, CFArrayGetValueAtIndex(list, i));
		if (!ps || CFGetTypeID(ps) != CFDictionaryGetTypeID()) {
			continue;
		}
		CFDictionaryRef d = (CFDictionaryRef)ps;
		CFNumberRef cap = CFDictionaryGetValue(d, CFSTR(kIOPSCurrentCapacityKey));
		if (!cap) {
			continue; // AC adapter entry, not a battery
		}
		double pct = 0;
		CFNumberGetValue(cap, kCFNumberDoubleType, &pct);
		*percent = pct;
		CFBooleanRef chg = CFDictionaryGetValue(d, CFSTR(kIOPSIsChargingKey));
		*charging = chg != NULL && CFBooleanGetValue(chg);
		found = 0;
		break;
	}
	CFRelease(list);
	CFRelease(blob);
	return found;
}
*/
import "C"

import "context"

// readBattery reports battery state via IOPowerSources — no helper
// processes involved.
func readBattery(ctx context.Context) *Battery {
	var (
		pct C.double
		chg C.int
	)
	if C.wt_battery(&pct, &chg) != 0 {
		return nil
	}
	return &Battery{Percent: float64(pct), Charging: chg != 0}
}
