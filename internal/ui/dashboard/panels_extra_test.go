package dashboard

import (
	"strings"
	"testing"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/theme"
)

// panelModel returns a model wired to the default theme with the base
// fake snapshot loaded, for exercising the panel render helpers.
func panelModel(t *testing.T) *Model {
	t.Helper()
	cfg := config.Default()
	m := New(cfg, theme.ByName(cfg.Theme))
	m.snap = fakeSnapshot()
	return m
}

// plain strips ANSI styling so assertions match the visible text.
func plain(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if r == '\x1b' {
			inEsc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestHostViewOptionalRows(t *testing.T) {
	m := panelModel(t)
	m.snap = richSnapshot()
	lines := m.hostView(34, 99)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"NAME", "SYS", "OS", "KERNEL", "LOAD", "PROCS", "USERS",
		"TEMP", "SENS", "BATTERY", "charging", "FANS"} {
		if !strings.Contains(joined, want) {
			t.Errorf("hostView missing %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "77%") {
		t.Errorf("hostView missing the battery percentage:\n%s", joined)
	}
	// the load graph trails: with room it is appended
	if len(lines) < 12 {
		t.Errorf("hostView with a generous row budget = %d lines, want the load graph too", len(lines))
	}
}

func TestHostViewOnBattery(t *testing.T) {
	m := panelModel(t)
	m.snap.Battery = &collector.Battery{Percent: 40, Charging: false}
	joined := strings.Join(m.hostView(34, 99), "\n")
	if !strings.Contains(joined, "on battery") {
		t.Errorf("discharged battery should read \"on battery\":\n%s", joined)
	}
}

// Sparse snapshots hide the optional rows entirely.
func TestHostViewSparse(t *testing.T) {
	m := panelModel(t)
	m.snap.Sensors = nil
	m.snap.Battery = nil
	m.snap.Fans = nil
	lines := m.hostView(34, 99)
	joined := strings.Join(lines, "\n")
	for _, gone := range []string{"TEMP", "SENS", "BATTERY", "FANS"} {
		if strings.Contains(joined, gone) {
			t.Errorf("sparse hostView should hide %s:\n%s", gone, joined)
		}
	}
	// a tight row budget drops the trailing load graph first
	tight := m.hostView(34, 3)
	if len(tight) != 5 {
		t.Errorf("tight hostView = %d lines, want the 5 kv rows only", len(tight))
	}
}

func TestFansRow(t *testing.T) {
	m := panelModel(t)
	m.snap.Fans = []collector.Fan{
		{Name: "CPU", RPM: 1200},
		{Name: "GPU", RPM: 940},
		{Name: "PSU", RPM: 800},
		{Name: "AUX", RPM: 600},
	}
	row := plain(m.fansRow(200))
	for _, want := range []string{"CPU 1200rpm", "GPU 940rpm", "PSU 800rpm", "+1"} {
		if !strings.Contains(row, want) {
			t.Errorf("fansRow missing %q: %q", want, row)
		}
	}
	// a narrow panel degrades to bare RPMs plus the overflow marker
	bare := plain(m.fansRow(5))
	if strings.Contains(bare, "CPU") {
		t.Errorf("named fans should not fit a 5-cell panel: %q", bare)
	}
	if !strings.Contains(bare, "1200rpm") || !strings.Contains(bare, "+1") {
		t.Errorf("bare fansRow = %q, want RPMs and the overflow marker", bare)
	}
	// unnamed fans fall back to F0, F1, ...
	m.snap.Fans = []collector.Fan{{RPM: 700}, {Name: "GPU", RPM: 900}}
	if row := plain(m.fansRow(200)); !strings.Contains(row, "F0 700rpm") {
		t.Errorf("unnamed fan should render as F0: %q", row)
	}
}

func TestCPUViewCompactContext(t *testing.T) {
	m := panelModel(t)
	m.SetSize(100, 30) // stacked: the compact context path
	m.snap.Sensors = []collector.Sensor{{Name: "CPU", TempC: 55}, {Name: "GPU", TempC: 48}}
	lines := m.cpuView(60, 20)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "3.2GHz") {
		t.Errorf("cpuView missing the clock line:\n%s", joined)
	}
	if !strings.Contains(joined, "max  90%") && !strings.Contains(joined, "max 90%") {
		t.Errorf("cpuView missing the busiest-core line:\n%s", joined)
	}
	if !strings.Contains(joined, "SENS") {
		t.Errorf("cpuView missing the sensor line:\n%s", joined)
	}
	// sub-200MHz clocks are gopsutil noise: hidden
	m.snap.CPU.FreqMHz = 100
	lines = m.cpuView(60, 20)
	if strings.Contains(strings.Join(lines, "\n"), "GHz") {
		t.Error("cpuView should hide implausible clocks")
	}
}

func TestCPUViewPerCoreBars(t *testing.T) {
	m := panelModel(t)
	m.SetSize(100, 30)
	lines := m.cpuView(60, 20)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "c0") || !strings.Contains(joined, "c9") {
		t.Errorf("per-core bars should cover all ten cores:\n%s", joined)
	}
}

func TestCoreGraphGrid(t *testing.T) {
	m := panelModel(t)
	m.snap.CPU.Cores = []float64{5, 10, 15, 20, 25, 30, 35, 40}
	m.SetSize(120, 38)
	m.Update(collector.SnapshotMsg{Snap: m.snap})

	if got := m.coreGraphGrid(82, 2); got != "" {
		t.Errorf("maxRows below one cell should give up, got %q", got)
	}
	m.snap.CPU.Cores = nil
	if got := m.coreGraphGrid(82, 6); got != "" {
		t.Errorf("no cores should give up, got %q", got)
	}
	m.snap.CPU.Cores = []float64{5, 10, 15, 20, 25, 30, 35, 40}
	grid := m.coreGraphGrid(82, 6)
	for _, want := range []string{"c0", "c7"} {
		if !strings.Contains(grid, want) {
			t.Errorf("core grid missing %q:\n%s", want, grid)
		}
	}
	if strings.Contains(grid, "more cores") {
		t.Errorf("all cores fit, no overflow expected:\n%s", grid)
	}
}

func TestPerCoreView(t *testing.T) {
	m := panelModel(t) // ten cores from the base snapshot
	if got := m.perCoreView(60, 0); got != "" {
		t.Errorf("maxRows 0 should give up, got %q", got)
	}
	m.snap.CPU.Cores = nil
	if got := m.perCoreView(60, 5); got != "" {
		t.Errorf("no cores should give up, got %q", got)
	}
	m.snap.CPU.Cores = []float64{10, 20, 30, 40, 50}
	got := m.perCoreView(60, 5)
	for _, want := range []string{"c0", "c4"} {
		if !strings.Contains(got, want) {
			t.Errorf("per-core view missing %q:\n%s", want, got)
		}
	}
	// more cores than fit: the overflow marker appears
	m.snap.CPU.Cores = []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	if got := m.perCoreView(60, 2); !strings.Contains(got, "+6 more cores") {
		t.Errorf("hidden cores should be counted: %q", got)
	}
}

func TestMemViewVariants(t *testing.T) {
	m := panelModel(t)

	// wide panel: hero digits plus the available-bytes tail
	lines := m.memView(60)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "avail") {
		t.Errorf("wide memView should show available memory:\n%s", joined)
	}
	if !strings.Contains(joined, "SWAP") {
		t.Errorf("memView missing the swap row:\n%s", joined)
	}

	// narrow panel: no hero card, pct leads the RAM row
	lines = m.memView(20)
	joined = strings.Join(lines, "\n")
	if strings.Contains(joined, "avail") {
		t.Errorf("narrow memView should drop the available tail:\n%s", joined)
	}
	if !strings.Contains(joined, "62.5%") {
		t.Errorf("narrow memView should lead with the RAM pct:\n%s", joined)
	}

	// no swap device: a dash placeholder instead of swap rows
	m.snap.Mem.SwapTotal = 0
	lines = m.memView(60)
	joined = strings.Join(lines, "\n")
	if !strings.Contains(joined, "SWAP —") {
		t.Errorf("swapless memView should show a dash:\n%s", joined)
	}

	// linux zram: an extra metered row
	m.snap.Mem.ZramTotal = 1 << 30
	m.snap.Mem.ZramUsed = 1 << 29
	joined = strings.Join(m.memView(60), "\n")
	if !strings.Contains(joined, "ZRAM") {
		t.Errorf("zram memView missing the ZRAM row:\n%s", joined)
	}
}

func TestNetView(t *testing.T) {
	m := panelModel(t)
	m.SetSize(100, 30)
	snap := fakeSnapshot()
	snap.Nets = []collector.NetIface{
		{Name: "en0", RxRate: 1.5e6, TxRate: 3.4e5},
		{Name: "utun4", RxRate: 1e5, TxRate: 9e5}, // upload-dominated
	}
	m.Update(collector.SnapshotMsg{Snap: snap})
	lines := m.netView(2)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"en0", "utun4", "↓pk", "↑pk"} {
		if !strings.Contains(joined, want) {
			t.Errorf("netView missing %q:\n%s", want, joined)
		}
	}
	// the busiest interface sorts first
	if strings.Index(joined, "en0") > strings.Index(joined, "utun4") {
		t.Errorf("busiest interface should lead:\n%s", joined)
	}
}

// A dashboard that never saw traffic has no peak ceilings to show.
func TestNetViewNoPeaksYet(t *testing.T) {
	m := panelModel(t)
	m.SetSize(100, 30)
	joined := strings.Join(m.netView(2), "\n")
	if strings.Contains(joined, "pk") {
		t.Errorf("fresh scales should hide the peak labels: %q", joined)
	}
}

func TestDiskView(t *testing.T) {
	m := panelModel(t)
	m.snap.DiskIOs = []collector.DiskIO{
		{Name: "disk1", BusyPercent: 42},
		{Name: "disk3", ReadBytes: 1000, WriteBytes: 2000, BusyPercent: 87},
	}
	m.snap.Disks = []collector.Disk{
		{Device: "/dev/disk3s1", Mountpoint: "/", Percent: 90},
		{Device: "/dev/disk1s1", Mountpoint: "/System/Volumes/Data", Percent: 60},
		{Device: "/dev/disk2s1", Mountpoint: "/Volumes/Backup", Percent: 40},
	}
	m.ioHist["disk3"] = []float64{1, 2, 3}

	// a two-row budget shows the busiest disk's meter, the fullest mount
	// and an overflow marker
	lines := m.diskView(60, 2)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "disk3") || !strings.Contains(joined, "87%") {
		t.Errorf("diskView missing the busy meter:\n%s", joined)
	}
	if !strings.Contains(joined, "… +2 more") {
		t.Errorf("diskView missing the overflow marker:\n%s", joined)
	}
	if strings.Contains(joined, "Backup") {
		t.Errorf("disks past the row budget should not render:\n%s", joined)
	}

	// roomier budget lists every mount, fullest first
	lines = m.diskView(60, 10)
	joined = strings.Join(lines, "\n")
	if strings.Contains(joined, "more") {
		t.Errorf("every disk fits, no overflow expected:\n%s", joined)
	}
	if strings.Index(joined, "/") > strings.Index(joined, "Backup") {
		t.Errorf("disks should sort by fullness:\n%s", joined)
	}

	// narrow panels skip the busy meter
	lines = m.diskView(20, 5)
	joined = strings.Join(lines, "\n")
	if strings.Contains(joined, "87%") {
		t.Errorf("narrow diskView should skip the busy meter:\n%s", joined)
	}
}

// The DISKS panel's activity spark must follow the mount's device to its
// IO history: IO counters are keyed by kernel device name ("disk3"),
// mount devices carry partition/slice suffixes ("/dev/disk3s5"), and an
// exact partition key (linux lists partitions in its IO table) still wins.
func TestDiskViewSparklineMatchesIODevice(t *testing.T) {
	m := panelModel(t)
	m.snap.DiskIOs = []collector.DiskIO{{Name: "disk3", ReadBytes: 1000, BusyPercent: 5}}
	m.snap.Disks = []collector.Disk{
		{Device: "/dev/disk3s5", Mountpoint: "/", Percent: 90},      // darwin slice suffix
		{Device: "/dev/sda1", Mountpoint: "/mnt/data", Percent: 50}, // exact partition key
	}
	m.ioHist["disk3"] = []float64{4000, 1000} // scaled: █ ▂
	m.ioHist["sda1"] = []float64{1000, 2000}  // scaled: ▄ █

	joined := strings.Join(m.diskView(80, 4), "\n")
	if !strings.ContainsRune(joined, '▂') {
		t.Errorf("slice-suffixed mount must find its history under the whole-disk IO name:\n%s", joined)
	}
	if !strings.ContainsRune(joined, '▄') {
		t.Errorf("exact partition key must still match:\n%s", joined)
	}
}

func TestDiskRightSumsIO(t *testing.T) {
	m := panelModel(t)
	right := m.diskRight()
	if !strings.Contains(right, "R ") || !strings.Contains(right, "W ") {
		t.Errorf("diskRight missing its labels: %q", right)
	}
	m.snap.DiskIOs = []collector.DiskIO{
		{Name: "disk1", ReadBytes: 1e6, WriteBytes: 2e6},
		{Name: "disk3", ReadBytes: 5e5, WriteBytes: 5e5},
	}
	right = plain(m.diskRight())
	// RateFixed pads to 10 cells; the sums land in Mb/s
	if !strings.Contains(right, "1.5 Mb/s") || !strings.Contains(right, "2.5 Mb/s") {
		t.Errorf("diskRight should sum per-device rates: %q", right)
	}
}

func TestMountLabel(t *testing.T) {
	cases := map[string]string{
		"/":                    "/",
		"/Users":               "Users",
		"/Volumes/Backup":      "Backup",
		"/System/Volumes/Data": "Data",
		"/System/Volumes/VM/":  "VM", // trailing slash still yields the base
	}
	for in, want := range cases {
		if got := mountLabel(in); got != want {
			t.Errorf("mountLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProcViewTiebreak(t *testing.T) {
	m := panelModel(t)
	m.snap.Procs = []collector.Proc{
		{PID: 1, Name: "lowmem", CPU: 5, Mem: 1.0, RSS: 1 << 20, User: "root"},
		{PID: 2, Name: "highmem", CPU: 5, Mem: 9.0, RSS: 1 << 20, User: "root"},
	}
	joined := strings.Join(m.procView(60, 5), "\n")
	if strings.Index(joined, "highmem") > strings.Index(joined, "lowmem") {
		t.Errorf("equal CPU should fall back to memory ordering:\n%s", joined)
	}
}

func TestGPULines(t *testing.T) {
	m := panelModel(t)
	if got := m.gpuLines(80, 4); got != nil {
		t.Errorf("no adapters should render no lines, got %v", got)
	}
	m.snap.GPUs = []collector.GPU{{Index: 0, Name: "Apple M2", Util: 55, TempC: 62}}
	if got := m.gpuLines(80, 0); got != nil {
		t.Errorf("no row budget should render no lines, got %v", got)
	}

	// wide panel: utilization bar plus a VRAM meter
	m.snap.GPUs = []collector.GPU{
		{Index: 0, Name: "Apple M2", Util: 55, MemUsed: 4 << 30, MemTotal: 10 << 30, TempC: 62},
	}
	wide := strings.Join(m.gpuLines(80, 4), "\n")
	for _, want := range []string{"GPU0", "Apple M2", "55%", "4.0 GiB/10.0 GiB", "62°"} {
		if !strings.Contains(wide, want) {
			t.Errorf("wide gpuLines missing %q:\n%s", want, wide)
		}
	}

	// narrow panel: VRAM figures without the meter
	narrow := strings.Join(m.gpuLines(50, 4), "\n")
	if !strings.Contains(narrow, "4.0 GiB/10.0 GiB") {
		t.Errorf("narrow gpuLines should keep the VRAM figures:\n%s", narrow)
	}

	// adapters without VRAM reporting render a bare line
	m.snap.GPUs = []collector.GPU{
		{Index: 0, Name: "Illusion", Util: 5, TempC: 44},
		{Index: 1, Name: "Delusion", Util: 7, TempC: 46},
		{Index: 2, Name: "Evident", Util: 9, TempC: 48},
	}
	if got := m.gpuLines(50, 2); len(got) != 2 {
		t.Errorf("row budget should cap the adapter lines at 2, got %d", len(got))
	}
}

func TestGPURight(t *testing.T) {
	m := panelModel(t)
	if got := m.gpuRight(); got != "" {
		t.Errorf("no adapters: gpuRight = %q, want \"\"", got)
	}
	m.snap.GPUs = []collector.GPU{{Name: "Apple M2"}}
	if got := plain(m.gpuRight()); got != "Apple M2" {
		t.Errorf("one adapter: gpuRight = %q, want the name", got)
	}
	m.snap.GPUs = []collector.GPU{{Name: "a"}, {Name: "b"}}
	if got := plain(m.gpuRight()); got != "2 adapters" {
		t.Errorf("two adapters: gpuRight = %q, want %q", got, "2 adapters")
	}
}

func TestSensorsLineAndList(t *testing.T) {
	m := panelModel(t)
	m.snap.Sensors = []collector.Sensor{
		{Name: "CPU", TempC: 55},
		{Name: "GPU", TempC: 48},
		{Name: "PCH", TempC: 41},
	}
	line := m.sensorsLine(200)
	for _, want := range []string{"SENS", "CPU", "GPU", "PCH"} {
		if !strings.Contains(line, want) {
			t.Errorf("sensorsLine missing %q: %q", want, line)
		}
	}
	// a narrow panel fits nothing past the prefix
	if line := m.sensorsLine(5); strings.Contains(line, "CPU") {
		t.Errorf("sensorsLine should drop cells that do not fit: %q", line)
	}
	// fewer than two sensors: no line at all
	m.snap.Sensors = m.snap.Sensors[:1]
	if got := m.sensorsLine(200); got != "" {
		t.Errorf("single sensor should hide the line, got %q", got)
	}

	rest := m.restSensors()
	if len(rest) != 0 {
		t.Fatalf("restSensors of a single-sensor snapshot = %v, want none", rest)
	}
	m.snap.Sensors = []collector.Sensor{{Name: "CPU", TempC: 55}, {Name: "GPU", TempC: 48}}
	if rest := m.restSensors(); len(rest) != 1 || rest[0].Name != "GPU" {
		t.Errorf("restSensors should skip the hottest, got %v", rest)
	}
	if got := m.sensorList(m.snap.Sensors, 200); !strings.Contains(got, "GPU") {
		t.Errorf("sensorList should render every fitting cell: %q", got)
	}
	if got := m.sensorList(m.snap.Sensors, 2); got != "" {
		t.Errorf("sensorList with no room should be empty, got %q", got)
	}
}

func TestHottestSensor(t *testing.T) {
	var snap collector.Snapshot
	if got := hottestSensor(snap); got != nil {
		t.Errorf("sensorless snapshot should have no hottest sensor, got %v", got)
	}
	snap.Sensors = []collector.Sensor{{Name: "CPU", TempC: 71}}
	if got := hottestSensor(snap); got == nil || got.Name != "CPU" {
		t.Errorf("hottest sensor = %v, want the first entry", got)
	}
}

func TestEvaluateAlertsEmptySnapshot(t *testing.T) {
	if al := EvaluateAlerts(config.Default(), collector.Snapshot{}); al != nil {
		t.Errorf("zero-time snapshot should yield no alerts, got %v", al)
	}
}

func TestEvaluateAlertsThresholds(t *testing.T) {
	cfg := config.Default()
	snap := fakeSnapshot()
	snap.CPU.Percent = 95                                         // crit 90
	snap.Mem.Percent = 85                                         // warn 80, crit 95
	snap.Mem.SwapPercent = 96                                     // crit
	snap.Sensors = []collector.Sensor{{Name: "GPU", TempC: 82}}   // crit 80
	snap.Disks = []collector.Disk{{Mountpoint: "/", Percent: 96}} // crit

	al := EvaluateAlerts(cfg, snap)
	if len(al) != 5 {
		t.Fatalf("alerts = %+v, want five crossings", al)
	}
	if !al[0].Crit {
		t.Errorf("critical findings must sort first: %+v", al)
	}
	if al[4].Crit {
		t.Errorf("the lone warning should trail: %+v", al)
	}
	var critKeys, allTexts []string
	for _, a := range al {
		allTexts = append(allTexts, a.Text)
		if a.Crit {
			critKeys = append(critKeys, a.Key)
		}
	}
	for _, want := range []string{"cpu", "swap", "temp:GPU", "disk:/"} {
		found := false
		for _, k := range critKeys {
			if k == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing critical alert %q in %v", want, critKeys)
		}
	}
	joined := strings.Join(allTexts, " | ")
	for _, want := range []string{"CPU 95%", "MEM 85%", "SWAP 96%", "TEMP GPU 82°C", "DISK / 96%"} {
		if !strings.Contains(joined, want) {
			t.Errorf("alerts missing %q: %s", want, joined)
		}
	}
}

// Values between the warn and crit thresholds yield warnings only.
func TestEvaluateAlertsWarnBand(t *testing.T) {
	cfg := config.Default()
	snap := fakeSnapshot()
	snap.CPU.Percent = 75                                             // warn 70, crit 90
	snap.Sensors = []collector.Sensor{{Name: "CPU", TempC: 65}}       // warn 60, crit 80
	snap.Disks = []collector.Disk{{Mountpoint: "/data", Percent: 82}} // warn 80

	al := EvaluateAlerts(cfg, snap)
	if len(al) != 3 {
		t.Fatalf("alerts = %+v, want three warnings", al)
	}
	keys := map[string]string{}
	for _, a := range al {
		if a.Crit {
			t.Errorf("nothing crossed the crit threshold: %+v", a)
		}
		keys[a.Key] = a.Text
	}
	for key, want := range map[string]string{
		"cpu":        "CPU 75%",
		"temp:CPU":   "TEMP CPU 65°C",
		"disk:/data": "DISK data 82%",
	} {
		if got := keys[key]; got != want {
			t.Errorf("alert %q = %q, want %q", key, got, want)
		}
	}
}

func TestTotalRatesAndShortRate(t *testing.T) {
	rx, tx := totalRates([]collector.NetIface{
		{RxRate: 100, TxRate: 10},
		{RxRate: 1, TxRate: 2},
	})
	if rx != 101 || tx != 12 {
		t.Errorf("totalRates = %v, %v; want 101, 12", rx, tx)
	}
	if got := shortRate(1.5e6); got != "1.5 Mb" {
		t.Errorf("shortRate = %q, want \"1.5 Mb\"", got)
	}
	if got := shortRate(500); got != "500 B" {
		t.Errorf("shortRate below the unit = %q, want \"500 B\"", got)
	}
}

func TestPadLinesAndIfaceBudget(t *testing.T) {
	if got := padLines([]string{"a", "b"}, 4); got != "a\nb\n\n" {
		t.Errorf("padLines grow = %q, want \"a\\nb\\n\\n\"", got)
	}
	if got := padLines([]string{"a", "b", "c"}, 2); got != "a\nb" {
		t.Errorf("padLines shrink = %q, want \"a\\nb\"", got)
	}
	if got := padLines(nil, 0); got != "" {
		t.Errorf("padLines nil = %q, want \"\"", got)
	}
	for h, want := range map[int]int{10: 1, 26: 2, 36: 4, 99: 4} {
		if got := ifaceBudget(h); got != want {
			t.Errorf("ifaceBudget(%d) = %d, want %d", h, got, want)
		}
	}
}
