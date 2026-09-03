# WrongTop

**WrongTop** is a cross-platform terminal system monitor for macOS, Linux and Windows. It watches CPU, memory and processes in a tabbed TUI with live braille charts — inspired by htop and btop, built with Go, [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [gopsutil](https://github.com/shirou/gopsutil).

## Features

- **Dashboard** — host identity (OS, kernel, uptime, load average), total CPU with a scrolling braille graph, per-core usage bars, and memory/swap panels with history graphs
- **Process table** — sortable, live-filterable list of every process with PID, name, CPU%, MEM%, RSS, user, thread count and htop-style state letter; terminate (SIGTERM) or force kill (SIGKILL) behind a confirmation prompt
- **Threshold colors** — values shift from normal → warning → critical based on configurable CPU/memory thresholds
- **Themes** — three built-in palettes: `gruvbox-dark` (default), `catppuccin-mocha`, `dracula`
- **Zero-config** — runs fine without a config file; YAML overrides are optional
- Mouse wheel scrolling and alt-screen rendering, responsive down to narrow terminals

Planned: disk, network and Docker tabs (placeholders are already wired into the tab bar).

## Installation

Requires Go 1.26+.

```sh
go install github.com/ersinkoc/wrongtop/cmd/wrongtop@latest
```

Or build from source:

```sh
git clone https://github.com/ersinkoc/wrongtop
cd wrongtop
go build -o wrongtop ./cmd/wrongtop
```

## Usage

```sh
wrongtop                  # start the monitor
wrongtop -c config.yaml   # use a specific config file
wrongtop version          # print the version
```

The config file can also be set with the `WRONGTOP_CONFIG` environment variable; otherwise `~/.config/wrongtop/config.yaml` is used if present.

### Keybindings

Global (work on every tab):

| Key | Action |
|---|---|
| `1`–`5` | jump to tab |
| `tab` / `shift+tab` | next / previous tab |
| `q` / `ctrl+c` | quit |

Processes tab:

| Key | Action |
|---|---|
| `↑` `↓` / mouse wheel | move selection |
| `/` | filter — matches name, user, or PID prefix for all-digit queries |
| `s` | cycle sort key: cpu → mem → pid → name → user |
| `S` | reverse sort order |
| `k` | terminate selected process (SIGTERM) |
| `K` | force kill selected process (SIGKILL) |
| `y` / `f` / `n` `esc` | confirm / upgrade to `kill -9` / cancel |

WrongTop refuses to kill its own process.

## Configuration

WrongTop works with no configuration. To customize, create `~/.config/wrongtop/config.yaml` (or point `WRONGTOP_CONFIG` / `--config` at a file):

```yaml
theme: gruvbox-dark       # gruvbox-dark | catppuccin-mocha | dracula
refresh: 1s               # sample interval, clamped to 250ms–10s

thresholds:               # percent where values turn warning / critical
  cpu_warn: 70
  cpu_crit: 90
  mem_warn: 80
  mem_crit: 95
```

Missing fields fall back to the defaults shown; a missing file is not an error.

> Note: the `modules` and `keys` config sections are parsed by the loader but not yet enforced by the UI.

## Project layout

| Package | Purpose |
|---|---|
| `cmd/wrongtop` | CLI entry point (cobra): flags and version command |
| `internal/app` | Bubble Tea root model: tab bar, global keys, layout |
| `internal/collector` | gopsutil polling into JSON-ready `Snapshot` values |
| `internal/config` | YAML config loading, defaults and clamping |
| `internal/format` | byte-count and uptime formatting helpers |
| `internal/procs` | process filter, sort, terminate/force-kill |
| `internal/theme` | palettes and derived lipgloss styles |
| `internal/ui` | tab contract, titled borders, braille canvas graphs |
| `internal/ui/dashboard` | the overview tab |
| `internal/ui/processes` | the process table tab |

## Development

```sh
go build ./cmd/wrongtop   # build
go test ./...             # run tests
go vet ./...              # static checks
```

Embed a version string at build time:

```sh
go build -ldflags "-X main.version=$(git describe --tags --always)" -o wrongtop ./cmd/wrongtop
```

## Roadmap

- [x] Dashboard: host, CPU, memory, swap
- [x] Processes: filter, sort, terminate / force kill
- [ ] Disks: filesystems and I/O rates
- [ ] Network: per-interface traffic
- [ ] Docker: containers, stats and actions
- [ ] Remote monitoring — collector snapshots are plain data, designed to cross a wire post-v1

## License

No license has been added yet.
