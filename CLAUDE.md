# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

This project uses [`just`](https://github.com/casey/just) as the command runner; `just` (no args) lists everything.

- `just build` — local dev build, outputs `sieve.exe` in the repo root
- `just run` — `go run .`
- `just run-timeout <seconds>` — `go run . --test-timeout <seconds>`
- `just release-build` — Windows/amd64 release build, outputs `dist/sieve-windows-amd64.exe`, version derived from `git describe --tags --abbrev=0` (or `$env:VERSION`)
- `just fmt` — `gofmt -w main.go internal`
- `just test` — `go test ./...` (no test files exist yet; this is the command to use once they do)
- `just clean` — removes `sieve.exe` and `dist/`
- `just icon` — regenerates `rsrc_windows_amd64.syso` from `assets/icon/icon.png` via [`go-winres`](https://github.com/tc-hib/go-winres); the `.syso` is committed and embedded automatically by `go build`/`go run`, so this only needs to run again after changing the source icon

There is no separate lint command; `go vet ./...` and `gofmt -l .` are the checks used in practice.

The justfile shells out via PowerShell (`set shell := ["powershell.exe", "-NoProfile", "-Command"]`), so this project is developed/built on Windows.

## Architecture

sieve is a portable Windows TUI (Bubble Tea + Lip Gloss) that automates running [Flowseal's zapret-discord-youtube](https://github.com/Flowseal/zapret-discord-youtube) `winws` DPI-bypass configs: it tries every bundled config in turn, keeps the first one that actually gets Discord and YouTube traffic through, and remembers it for next time.

### Entry point and flag/TUI split

`main.go` → `internal/cli.Execute()` is the only entry point. The root cobra command (`internal/cli/root.go`) branches on whether any flags were explicitly set (`hasChangedFlags`):
- **No flags** → `runApp()`: requires admin (self-elevates via `internal/admin` if not), runs `autoUpdate()` (self-update check, see below), loads settings, then launches the Bubble Tea program (`internal/ui`). This is the actual DPI-bypass flow.
- **Any flag set** → `runCommandMode()`: flags either persist to `settings.json` via `internal/settings.Store` (e.g. `--ipset`, `--game`, `--domain`, `--no-cache`) or perform one one-shot maintenance action and exit (`--update`, `--reset-cache`, `--update-ipset`, `--diagnostics`, `--clear-discord-cache`). Flags never start the TUI.

### `internal/ui` — the TUI

Single-screen Bubble Tea state machine, not multi-page navigation. One `Model`/`View` swaps its body based on `State` (`StateUpdating → StateTesting → StateRunning`, or `StateNoLuck` on failure; `StateClosing → StateBye` on quit).

- `model.go` — `App`/`Model` structs, `Init`/`Update` lifecycle, message types
- `view.go` — `View()` rendering and **all `lipgloss` style/color vars**. The palette mirrors the project's website (warm dark grays, rust accent `#8C6B52`) — when touching UI colors, keep that consistency rather than reverting to default `lipgloss`/ANSI colors.
- `flow.go` — the async pipeline: ensures assets are present (`internal/assets`), kills any leftover `winws.exe`, iterates cached-sorted configs (`internal/cache.Store.SortedConfigs`) starting one at a time via `internal/runner`, runs connectivity checks (`internal/tester`) after a warmup delay, and either keeps the first working process alive (`StateRunning`, streaming its logs) or falls through to `StateNoLuck`
- `logs.go` — turns raw `winws`/WinDivert stdout lines into the friendly "clean log" view (vs. raw mode, toggled with `ctrl+o`)

`q` and `ctrl+c` are intentionally aliased to the same quit path (one `case` in `Update()`) — don't reintroduce separate "quit" vs "cleanup" semantics in either code or copy.

### Config strategy data

`internal/configs/configs.go` is a large generated-feeling list of `Config{Name, Args}` — `Args` are literal `winws` CLI flags with `{BIN}`/`{LISTS}` placeholders resolved against the downloaded Flowseal asset paths. `Config.Name` is also the cache key in `internal/cache` — **don't rename these without considering that it invalidates users' cached "known working config" data.**

### Supporting packages

- `internal/assets` — downloads/extracts the Flowseal `zapret-discord-youtube` release zip (bin + lists) into `%APPDATA%\sieve`, reports progress via callback
- `internal/cache` — JSON-persisted per-config success/failure record at `%APPDATA%\sieve\cache.json`; `SortedConfigs` ranks previously-successful configs first so a second run finds a working config faster
- `internal/settings` — `RuntimeOptions` persisted to `%APPDATA%\sieve\settings.json` (ipset mode, domains, game filter, cache/PATH toggles); `system_windows.go`/`system_other.go` hold the `--diagnostics`/`--clear-discord-cache` platform-specific checks (build-tag split, like `runner` and `envpath`)
- `internal/runner` — starts/stops `winws.exe`, streams its stdout/stderr as a log channel, platform-specific WinDivert/service cleanup
- `internal/tester` — HTTP reachability check against `discord.com` and `www.youtube.com`, used to judge whether a running config actually works
- `internal/selfupdate` — checks `GET /repos/elev1e1nSure/sieve/releases/latest`, matches a compatible asset by name (`sieve-windows-amd64.exe` preferred, `sieve.exe` as legacy fallback — see naming note below), then launches a hidden temporary helper that waits for sieve to exit, retries and verifies the replacement, records the result, and restarts only after success
- `internal/envpath`, `internal/admin`, `internal/paths` — Windows PATH auto-add, UAC elevation, and the `%APPDATA%\sieve` install dir, each with `_windows.go`/`_other.go` split

Platform-specific files follow Go's `_windows.go` / `_other.go` filename build-tag convention throughout — when adding OS-specific behavior, follow that pattern rather than runtime `runtime.GOOS` checks.

### Release naming convention

CI (`.github/workflows/release.yml`) builds and attaches `sieve-windows-amd64.exe` to GitHub releases on `v*` tags — this is the canonical name going forward (Windows/amd64 convention). `internal/selfupdate/updater.go`'s `compatibleAsset()` whitelist must keep `sieve.exe` as a fallback for compatibility with older releases; don't drop it.

The Scoop bucket (`elev1e1nSure/scoop-bucket`, separate repo) has its own GitHub Actions workflow that polls `sieve`'s latest release and keeps `bucket/sieve.json`'s version/url/hash in sync automatically — no manual bucket maintenance needed after a release ships.

### Voice/copy

User-facing copy (README, `--help`, TUI chrome — title, empty/idle/exit states) intentionally carries a light literary tone built around the "sifting" metaphor, matching the project website. Diagnostic/error output (Windows service checks, self-update errors, etc.) stays plainly technical — don't extend the literary tone into troubleshooting text where clarity matters more than voice.
