# WrongTop

**WrongTop** is a cross-platform terminal system monitor for macOS, Linux and Windows. It watches CPU, memory, processes, disks, network and Docker containers in a tabbed TUI with live braille charts — inspired by htop and btop, built with Go, [Bubble Tea](https://github.com/charmbracelet/bubbletea) v2 and [gopsutil](https://github.com/shirou/gopsutil).

## Features

- **Dashboard** — host identity (OS, kernel, uptime, load average), total CPU with a scrolling braille graph, per-core usage bars, and memory/swap panels with history graphs
- **Process table** — sortable, live-filterable list of every process with PID, name, CPU%, MEM%, RSS, user, thread count and htop-style state letter; terminate (SIGTERM) or force kill (SIGKILL) behind a confirmation prompt
- **Docker** — container list with live CPU, memory, network and block I/O; start / stop / restart; follow-mode log viewer with stream demultiplexing. The daemon is optional: the tab shows a notice and retries until it appears
- **Disks** — filesystem usage bars plus per-device read/write rates, IOPS and busy time
- **Network** — per-interface throughput and totals, sorted by current activity
- **Threshold colors** — values shift from normal → warning → critical based on configurable CPU/memory thresholds
- **Themes** — three built-in palettes: `gruvbox-dark` (default), `catppuccin-mocha`, `dracula`
- **Zero-config** — runs fine without a config file; YAML overrides are optional
- Mouse wheel scrolling and alt-screen rendering, responsive down to narrow terminals

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

Release binaries (macOS/Linux/Windows × amd64/arm64), a Homebrew tap and checksums are produced by [goreleaser](https://goreleaser.com) on every `v*` tag.

## Usage

```sh
wrongtop                  # start the monitor
wrongtop -c config.yaml   # use a specific config file
wrongtop version          # print the version
wrongtop config-sample    # print an annotated sample config
```

The config file can also be set with the `WRONGTOP_CONFIG` environment variable; otherwise `~/.config/wrongtop/config.yaml` is used if present.

### Keybindings

Global (work on every tab):

| Key | Action |
|---|---|
| `1`–`N` | jump to tab (the status bar shows the live range) |
| `tab` / `shift+tab` | next / previous tab |
| `?` / `esc` | toggle / dismiss the help overlay |
| `q` / `ctrl+c` | quit |

Processes tab:

| Key | Action |
|---|---|
| `↑` `↓` / mouse wheel | move selection |
| `/` | filter — matches name, user, or PID prefix for all-digit queries (config: `keys.filter`) |
| `s` | cycle sort key: cpu → mem → pid → name → user |
| `S` | reverse sort order |
| `k` | terminate selected process (SIGTERM) (config: `keys.kill`) |
| `K` | force kill selected process (SIGKILL) — the uppercase variant of the kill key |
| `y` / `f` / `n` `esc` | confirm / upgrade to `kill -9` / cancel |

WrongTop refuses to kill its own process.

Docker tab:

| Key | Action |
|---|---|
| `enter` | follow container logs (`esc` to go back) |
| `s` / `t` / `r` | start / stop / restart container |

## Configuration

WrongTop works with no configuration. To customize, run `wrongtop config-sample` and start from the annotated output.

```yaml
theme: gruvbox-dark       # gruvbox-dark | catppuccin-mocha | dracula
refresh: 1s               # sample interval, clamped to 250ms–10s

modules:                  # optional tabs
  processes: true         # show the process table
  docker: true            # show the Docker tab

thresholds:               # percent where values turn warning / critical
  cpu_warn: 70
  cpu_crit: 90
  mem_warn: 80
  mem_crit: 95

keys:                     # processes-tab overrides (single letters)
  kill: k                 # terminate key; its uppercase variant force-kills
  filter: /
```

Missing fields fall back to the defaults shown; a missing file is not an error. The `modules` flags control which tabs are built, and empty `keys` values fall back to the defaults shown.

## Project layout

| Package | Purpose |
|---|---|
| `cmd/wrongtop` | CLI entry point (cobra): flags and version commands |
| `internal/app` | Bubble Tea root model: tab bar, global keys, help overlay, layout |
| `internal/collector` | gopsutil polling into JSON-ready `Snapshot` values (darwin fast path via bulk sysctl) |
| `internal/config` | YAML config loading, defaults and clamping |
| `internal/dockerclient` | moby SDK wrapper: container list + stats, actions, log streaming |
| `internal/format` | byte-count, rate and uptime formatting helpers |
| `internal/procs` | process filter, sort, terminate/force-kill |
| `internal/theme` | palettes and derived lipgloss styles |
| `internal/ui` | tab contract and titled borders |
| `internal/ui/canvas` | braille time-series graphs and fraction bars |
| `internal/ui/dashboard` | the overview tab |
| `internal/ui/processes` | the process table tab |
| `internal/ui/docker` | the containers tab |
| `internal/ui/disks` | the filesystems and I/O tab |
| `internal/ui/network` | the per-interface traffic tab |

## Development

```sh
make build   # build
make test    # run tests (-race)
make vet     # static checks
make cross   # all 6 platform targets into dist/
```

Embed a version string at build time:

```sh
go build -ldflags "-X github.com/ersinkoc/wrongtop/cmd/wrongtop.version=$(git describe --tags --always)" -o wrongtop ./cmd/wrongtop
```

## Roadmap

- [x] Dashboard: host, CPU, memory, swap
- [x] Processes: filter, sort, terminate / force kill
- [x] Disks: filesystems and I/O rates
- [x] Network: per-interface traffic
- [x] Docker: containers, stats, actions, log streaming
- [ ] Process tree view
- [ ] Remote monitoring — collector snapshots are plain data, designed to cross a wire post-v1

## License

MIT — see [LICENSE](LICENSE).
