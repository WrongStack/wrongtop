# Contributing to WrongTop

Thanks for helping! This document covers everything needed to build,
test and release.

## Development

Requires Go 1.26+ (see `go.mod`).

```sh
make build          # ./wrongtop, version from git
make test           # go test -race ./...
make vet            # go vet ./...
make lint           # golangci-lint (falls back to vet)
make cross          # local cross-compile smoke build into dist/
```

On macOS build with CGO enabled (the default) — it links IOKit for
AppleSMC temperatures, fans and battery. `CGO_ENABLED=0` builds run
everywhere but skip those sensors.

## Code conventions

- Match the surrounding style; comments explain **why**, not what.
- UI code: colors and ramps come from `internal/theme` — never hardcode
  hex values in a tab. Value→color gradients live in `Theme.Ramps`.
- Every table `columns()` and its row builder must consult the same
  width predicates, and `ui.SetTableColumns` + `rebuild()` must be used
  in `SetSize` (see CONTRIBUTING history: resize panics).
- New messages that can change the frame are automatically covered by
  the app's frame cache; only `tea.MouseMotionMsg` is ignored.
- Add or update pinned tests when touching rendering — most regressions
  in this repo were caught by string-pinned tests.

## Commits

Conventional Commits (`feat:`, `fix:`, `perf:`, `docs:`, `chore:` …).
`docs:`, `test:` and `chore:` commits are excluded from release notes.

## Releasing

Releases are fully automated:

1. Merge to `main` (CI must be green: vet, race tests, gofmt,
   golangci-lint).
2. `git tag v2.x.y && git push origin v2.x.y`
3. GitHub Actions runs goreleaser on a macOS runner and publishes the
   release: darwin arm64/amd64 (cgo, IOKit), linux and windows
   (amd64/arm64), archives, checksums and the Homebrew cask.

Version comes from the tag (`-X main.version=`).

To enable Homebrew tap uploads: create the `wrongstack/homebrew-tap`
repository, add a `HOMEBREW_TAP_TOKEN` secret (PAT with repo scope on
the tap) here, then set `skip_upload: false` in the `homebrew_casks`
section of `.goreleaser.yaml`.
