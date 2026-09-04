# WrongTop

**WrongTop** is a cross-platform terminal system monitor for macOS, Linux and Windows. It watches CPU, memory, processes, disks, network, Docker containers, temperatures, fans and GPUs in a tabbed TUI with live braille charts — inspired by htop, btop and glances, built with Go, [Bubble Tea](https://github.com/charmbracelet/bubbletea) v2 and [gopsutil](https://github.com/shirou/gopsutil).

## Features

- **Single-screen overview** (btop-style): the dashboard packs every major panel into one screen — host identity, CPU, memory/swap/zram, network, disks, GPUs and the busiest processes — in a responsive bordered grid
- **Dashboard panels** — host (OS, kernel, uptime, load average, logged-in users, temperatures, battery, fan speeds), total CPU with a scrolling braille graph, per-core usage bars and frequency, RAM/swap/zram bars with history, network down/up graphs with the busiest interfaces, per-mount usage bars with aggregate I/O rates, and a top-process panel
- **Sensors** — CPU temperatures and fan RPMs on every platform it can get them: on macOS (arm64) WrongTop reads the AppleSMC directly via IOKit — real P-core temperatures that neither btop nor glances show on a Mac — plus battery state through IOPowerSources; Linux uses hwmon; Windows tries ACPI thermal zones and WMI
- **GPU monitoring** — utilization, VRAM and temperature through NVIDIA's NVML driver library, loaded at runtime (no cgo, no helper process); the panel appears only when a GPU exists
- **Alerts with history** (glances-style): a warning strip appears when thresholds are crossed, and `a` opens a timestamped session history with severities and durations
- **Live status bar** — threshold-colored cpu/mem chips, network throughput, temperature and battery at the bottom, btop-style
- **Process table** — sortable, live-filterable list with PID, name, CPU%, MEM%, RSS, user, thread count, nice value and htop-style state letter; **tree view** with collapse/expand; a full **signal menu** (TERM, KILL, INT, HUP, QUIT, STOP, CONT); per-process **detail box** with the full command line
- **Docker** — container list with live CPU, memory, network and block I/O fetched concurrently; start / stop / restart; follow-mode log viewer with scrollback. The daemon is optional: the tab shows a notice and retries until it appears
- **Disks** — filesystem usage bars plus per-device read/write rates, IOPS and busy time
- **Network** — per-interface throughput and totals, sorted by current activity
- **Themes** — eight built-in palettes plus **user-defined theme files** loaded from `~/.config/wrongtop/themes/*.yml`; `T` cycles themes live without losing state
- **Live config** — `R` hot-reloads the YAML (theme, refresh, thresholds, layout, key bindings); `p` cycles dashboard density presets
- **Remote monitoring** — `wrongtop serve` streams snapshots to `wrongtop connect` clients over a token-authenticated, read-only protocol; `wrongtop dump` prints JSON lines for scripts
- **Zero-config** — runs fine without a config file; YAML overrides are optional
- Full mouse support — click to select rows and tabs, wheel to scroll — and alt-screen rendering, responsive from narrow terminals to full-screen grids

## Installation

Requires Go 1.26+.

```sh
go install github.com/ersinkoc/wrongtop/cmd/wrongtop@latest
```

Or build from source:

```sh
git clone https://github.com/ersinkoc/wrongtop
cd wrongtop
make build   # or: go build -o wrongtop ./cmd/wrongtop
```

> Building on macOS with CGO enabled (`CGO_ENABLED=1`, the default) links IOKit and unlocks AppleSMC temperatures, fan speeds and battery readings. A `CGO_ENABLED=0` build works everywhere but skips those sensors.

Release binaries (macOS/Linux/Windows × amd64/arm64), a Homebrew tap and checksums are produced by [goreleaser](https://goreleaser.com) on every `v*` tag; darwin builds link IOKit, the rest stay cgo-free.

## Usage

```sh
wrongtop                        # start the monitor
wrongtop -c config.yaml         # use a specific config file
wrongtop serve                  # stream snapshots to connect clients
wrongtop connect host:port      # watch a remote machine
wrongtop dump --count 5         # JSON snapshots to stdout
wrongtop version                # print the version
wrongtop config-sample          # print an annotated sample config
```

The config file can also be set with the `WRONGTOP_CONFIG` environment variable; otherwise `~/.config/wrongtop/config.yaml` is used if present.

### Remote monitoring

One machine runs the headless server; any number of clients watch it:

```sh
# on the machine to monitor — require a token, never run it open
WRONGTOP_TOKEN=choose-a-secret wrongtop serve --listen :61234

# from anywhere that can reach the port
WRONGTOP_TOKEN=choose-a-secret wrongtop connect server:61234

# scripts get the same data as JSON lines
WRONGTOP_TOKEN=choose-a-secret wrongtop connect …   # interactive
wrongtop dump --count 60                            # local JSON lines
```

The protocol is read-only by construction: clients authenticate with a constant-time token check and then only receive snapshot frames. Remote views disable the Docker tab, process signaling and command-line lookups — there is nothing to execute on the server.

### Keybindings

Global (work on every tab):

| Key | Action |
|---|---|
| `1`–`N` | jump to tab (the status bar shows the live range) |
| `tab` / `shift+tab` | next / previous tab |
| `T` | cycle color theme (built-ins and user themes) |
| `R` | reload the config file |
| `a` | alert history overlay |
| `?` / `esc` | toggle / dismiss overlays |
| `q` / `ctrl+c` | quit |
| mouse | click tabs and rows, wheel to scroll |

Processes tab:

| Key | Action |
|---|---|
| `↑` `↓` / mouse wheel | move selection |
| `/` | filter — matches name, user, or PID prefix for all-digit queries (config: `keys.filter`) |
| `s` | cycle sort key: cpu → mem → pid → name → user |
| `S` | reverse sort order |
| `t` | toggle the process tree (`left`/`right` collapse/expand subtrees) |
| `enter` | open the process detail box (full command line, nice, threads, …) |
| `k` | signal menu preset to SIGTERM (config: `keys.kill`) |
| `K` | signal menu preset to SIGKILL — the uppercase variant of the kill key |
| `←` `→` | pick the signal in the menu |
| `y` | send the selected signal · `n`/`esc` cancel |

WrongTop refuses to signal its own process.

Dashboard tab:

| Key | Action |
|---|---|
| `p` | cycle density: full → compact → minimal |

Docker tab:

| Key | Action |
|---|---|
| `enter` | follow container logs (`esc` to go back, wheel scrolls) |
| `s` / `t` / `r` | start / stop / restart container |

Disks tab:

| Key | Action |
|---|---|
| `←` `→` | switch between the filesystem and I/O tables |

## Configuration

WrongTop works with no configuration. To customize, run `wrongtop config-sample` and start from the annotated output.

```yaml
theme: gruvbox-dark       # 8 built-ins, or a file in ~/.config/wrongtop/themes
refresh: 1s               # sample interval, clamped to 250ms–10s
layout: full              # full | compact | minimal (p cycles it live)

modules:                  # optional tabs
  processes: true         # show the process table
  docker: true            # show the Docker tab

thresholds:               # percent where values turn warning / critical
  cpu_warn: 70
  cpu_crit: 90
  mem_warn: 80
  mem_crit: 95
  temp_warn: 60           # CPU temperature °C (shown when the platform reports it)
  temp_crit: 80

keys:                     # processes-tab overrides (single letters)
  kill: k                 # opens the signal menu; uppercase presets SIGKILL
  filter: /

read_only: false          # true disables process signaling (also set on remote views)
```

Missing fields fall back to the defaults shown; a missing file is not an error.

### Custom themes

Drop a YAML file into `~/.config/wrongtop/themes/` — the file name becomes the theme name (or set `name:`), any omitted color inherits the default palette:

```yaml
# ~/.config/wrongtop/themes/ocean.yml
bg: "#0b1e2d"
fg: "#cfe3f5"
red: "#ff6b6b"
green: "#5fd39a"
yellow: "#f5d76e"
blue: "#5aa9e6"
purple: "#b58cd9"
cyan: "#6ed3e7"
orange: "#f0932b"
gray: "#5d7a94"
```

`theme: ocean` picks it up, and it joins the `T` cycle. Files that fail to parse are skipped.

## Project layout

| Package | Purpose |
|---|---|
| `cmd/wrongtop` | CLI entry point (cobra): run, serve, connect, dump, config-sample |
| `internal/app` | Bubble Tea root model: tab bar, global keys, overlays, alert history, status bar |
| `internal/collector` | gopsutil polling into JSON-ready `Snapshot` values (darwin: bulk sysctl + cgo AppleSMC/IOPowerSources; linux: hwmon fans, zram, utmp; windows: WMI battery/thermal; NVML for GPUs; slow-metric caches) |
| `internal/config` | YAML config loading, defaults and clamping |
| `internal/dockerclient` | moby SDK wrapper: concurrent container stats, actions, log streaming |
| `internal/format` | byte-count, rate and uptime formatting helpers |
| `internal/procs` | process filter/sort, tree builder, signal menu and delivery |
| `internal/remote` | token-authenticated snapshot streaming protocol (serve/connect plumbing) |
| `internal/theme` | built-in + user palettes and derived lipgloss styles |
| `internal/ui` | tab contract, scroll-proof table click mapping, titled borders |
| `internal/ui/canvas` | braille time-series graphs and fraction bars |
| `internal/ui/dashboard` | the overview tab: btop-style grid of host/CPU/memory/network/disk/GPU/process panels + alert strip |
| `internal/ui/processes` | the process table tab: flat/tree, filters, signal menu, detail box |
| `internal/ui/docker` | the containers tab |
| `internal/ui/disks` | the filesystems and I/O tab |
| `internal/ui/network` | the per-interface traffic tab |

## Development

```sh
make build   # build
make test    # run tests (-race)
make vet     # static checks
make cross   # all platform targets into dist/
```

Embed a version string at build time:

```sh
go build -ldflags "-X github.com/ersinkoc/wrongtop/cmd/wrongtop.version=$(git describe --tags --always)" -o wrongtop ./cmd/wrongtop
```

## Roadmap

- [x] Dashboard: host, CPU, memory, swap, zram, sensors, fans, battery, GPUs
- [x] Processes: filter, sort, tree view, detail box, signal menu
- [x] Disks: filesystems and I/O rates
- [x] Network: per-interface traffic
- [x] Docker: containers, stats, actions, log streaming
- [x] Remote monitoring: `serve` / `connect` / `dump`
- [ ] Container support beyond Docker (Podman, k8s)
- [ ] Exporters (InfluxDB, Prometheus) and web dashboard

## License

MIT — see [LICENSE](LICENSE).
