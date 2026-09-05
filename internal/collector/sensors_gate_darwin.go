//go:build darwin

package collector

// gopsutilSensorsAvailable is false on darwin: gopsutil's sensor probe
// costs ~50ms per call there and yields nothing — the AppleSMC reader
// (platformTemps) is both faster and better.
const gopsutilSensorsAvailable = false
