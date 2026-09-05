//go:build !darwin

package collector

// gopsutilSensorsAvailable is true everywhere but darwin: hwmon (linux)
// and the ACPI path are cheap enough to try first.
const gopsutilSensorsAvailable = true
