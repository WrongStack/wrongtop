# Changelog

All notable changes to WrongTop are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow
[Semantic Versioning](https://semver.org).

## [2.0.0] - 2026-09-12

### Added
- **Connected-frame dashboard** (btop-style): shared dividers, resolved
  junctions, per-category colored panels with **border readouts** —
  title with icon on the left, live value flush right (uptime on HOST,
  `45.2% · 55°C` on CPU, R/W rates on DISKS, …)
- **Per-core rolling graphs** in the wide CPU panel plus big-digit
  memory hero on a soft card
- **SENSORS tab**: rolling temperature graphs per sensor, fan speeds
  with activity sparklines, battery state with charge history
- **CONNECTIONS tab**: live TCP table (traffic first), per-state
  coloring, process names where the platform resolves PIDs
- **ACTIVITY meter columns** in processes, docker, disks and network
  tables; **DROP/s** column on the network tab
- Docker **health-aware status coloring** and a graceful empty state
- AppleSMC temperatures, fan speeds and battery on macOS (arm64 cgo);
  NVML GPU monitoring on Linux/Windows; zram on Linux
- Alert chips on the tab bar with a **pulsing** critical state,
  durations, and a timestamped session history (`a`)
- Process tree with collapse/expand, signal menu, per-process detail
  box with full command line
- Remote monitoring: `serve` / `connect` / `dump` over a token
  authenticated, read-only stream
- Persistent connection-lost banner in remote mode
- Themes: ten built-ins plus user theme files (`~/.config/wrongtop/themes`),
  live cycling with `T`; `border: rounded | square | thick | double`;
  `nerd_fonts` icon set
- GitHub Actions release pipeline (goreleaser; darwin builds link IOKit
  via a macOS runner, everything else ships cgo-free)

### Changed
- Module path moved to `github.com/wrongstack/wrongtop`
- Full CPU/RAM optimization pass: view rebuild −55% time / −73%
  allocations, process collection −59%, memoized bars and graphs,
  hidden tabs skip table rebuilds, docked status bar

### Fixed
- Frame renderer dropped a border cell at every panel-color transition
- `-ldflags -X` version injection under the Go 1.27 linker (`main.version`
  form)
- Table crash on terminal resize when column counts changed
- Disks I/O column truncation for `440.0 Kb/s`-width rates
- Help/alerts overlays and the status flash overflowed wide terminals;
  all are now truncated to terminal width
- Config hot-reload dropped the module set and the remote read-only flag
- Remote mode is now forced read-only, and remote stream writes are
  bounded by a deadline so stalled clients release; `Dial` respects its
  context deadline and `Serve` closes all client streams on shutdown
- Sensor history maps grew without bound as labels churned
- Hex channels were not zero-padded, corrupting dark gradient blends
- Config loader rejected tree and app-layer keys in overrides
- Docker panel kept stale container entries across updates
- `dump` hung after the final requested snapshot
- Percent alert thresholds were not normalized like temperature ones
- Short rate chips padded the `/s` suffix instead of the rate digits

## [1.0.0] - 2026-09-03

Initial release: tabbed TUI (dashboard, processes, docker, disks,
network) with braille graphs, gradient meters, alert history, themes,
config hot-reload and remote monitoring.
