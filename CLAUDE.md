# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

This project uses [`just`](https://github.com/casey/just) as the command runner; `just` (no args) lists everything.

- `just build` — local dev build, outputs `sieve.exe` in the repo root
- `just run` — `go run .`
- `just run-timeout <seconds>` — `go run . --test-timeout <seconds>`
- `just release-build` — Windows/amd64 release build, outputs `dist/sieve-windows-amd64.exe`, version derived from `git describe --tags --abbrev=0` (or `$env:VERSION`)
- `just fmt` — `gofmt -w main.go internal`
- `just fmt-check` — `gofmt -l main.go internal`, fails if any file needs formatting (no writes)
- `just lint` — `go vet ./...` + `golangci-lint run` (config in `.golangci.yml`)
- `just check` — composite `fmt-check` + `lint` + `test` + `build`; the single recipe CI (`.github/workflows/ci.yml`) invokes
- `just test` — `go test ./...`; tests cover the pure logic (fsutil, cache ranking, version comparison, domain/list handling, log formatting, release fetch, connectivity checks) — the TUI, runner, and Win32 layers are untested by design
- `just clean` — removes `sieve.exe` and `dist/`
- `just icon` — regenerates `rsrc_windows_amd64.syso` from `assets/icon/icon.png` via [`go-winres`](https://github.com/tc-hib/go-winres); the `.syso` is committed and embedded automatically by `go build`/`go run`, so this only needs to run again after changing the source icon

The justfile shells out via PowerShell (`set shell := ["powershell.exe", "-NoProfile", "-Command"]`), so this project is developed/built on Windows.

## Architecture

sieve is a portable Windows TUI (Bubble Tea + Lip Gloss) that automates running [Flowseal's zapret-discord-youtube](https://github.com/Flowseal/zapret-discord-youtube) `winws` DPI-bypass configs: it tries every bundled config in turn, keeps the first one that actually gets Discord and YouTube traffic through, and remembers it for next time.

### Entry point and flag/TUI split

`main.go` → `internal/cli.Execute()` is the only entry point. The `internal/cli` package is split into `root.go` (command wiring, `runAppMode`/`runSieve`, auto-update), `command.go` (flag mode), `print.go` (styled terminal output), and `root_test.go`. The cobra command structure handles execution:
- **No flags / no subcommand (`sieve`)** → `runAppMode(ctx, false)`: requires admin (self-elevates via `internal/admin` if not), runs `autoUpdate()` (self-update check, see below), loads settings, shows the launcher menu (`ui.LauncherModel`: start sifting / settings / maintenance actions), and then runs the sifting TUI (`runSieve`).
- **`run` subcommand (`sieve run`)** → `runAppMode(ctx, true)`: skips the interactive launcher menu completely and immediately starts config selection and bypass activation (`runSieve`).
- **Any flag set without subcommand** → `runCommandMode()`: flags either persist to `settings.json` via `internal/settings.Store` (e.g. `--ipset`, `--game`, `--domain`, `--no-cache`) or perform one one-shot maintenance action via `internal/maintenance.Service` and exit (`--update`, `--stop`, `--reset-cache`, `--update-ipset`, `--diagnostics`, `--status`, `--clear-discord-cache`). Flags never start the TUI.

### `internal/ui` — the TUI

Single-screen Bubble Tea state machine, not multi-page navigation. One `Model`/`View` swaps its body based on `State` (`StateUpdating → StateTesting → StateRunning`, or `StateNoLuck` on failure; `StateClosing → StateBye` on quit).

- `model.go` — `App`/`Model` structs, `Init`/`Update` lifecycle, message types
- `view.go` — `View()` rendering and **all `lipgloss` style/color vars**. The palette mirrors the project's website (warm dark grays, rust accent `#8C6B52`) — when touching UI colors, keep that consistency rather than reverting to default `lipgloss`/ANSI colors.
- `flow.go` — the async pipeline: ensures assets are present (`internal/assets`), kills any leftover `winws.exe`, iterates cached-sorted configs (`internal/cache.Store.SortedConfigs`) starting one at a time via `internal/runner`, runs connectivity checks (`internal/tester`) after a warmup delay, and either keeps the first working process alive (`StateRunning`, streaming its logs) or falls through to `StateNoLuck`
- `logs.go` — turns raw `winws`/WinDivert stdout lines into the friendly "clean log" view (vs. raw mode, toggled with `ctrl+o`)
- `launcher.go` — the pre-run launcher menu (`LauncherModel`): start sifting, edit settings (auto-saved on change), or run maintenance actions via `internal/maintenance`

`ctrl+c` is the single quit path in the sifting TUI — don't reintroduce separate "quit" vs "cleanup" semantics in either code or copy. `t` minimises to the system tray (`internal/tray`) when sieve owns its console window.

### Config strategy data

`internal/configs/configs.go` is a large generated-feeling list of `Config{Name, Args}` — `Args` are literal `winws` CLI flags with `{BIN}`/`{LISTS}` placeholders resolved against the downloaded Flowseal asset paths. `Config.Name` is also the cache key in `internal/cache` — **don't rename these without considering that it invalidates users' cached "known working config" data.**

### Supporting packages

- `internal/assets` — downloads/extracts the Flowseal `zapret-discord-youtube` release zip (bin + lists) into `%APPDATA%\sieve`, reports progress via callback
- `internal/cache` — JSON-persisted per-config success/failure record at `%APPDATA%\sieve\cache.json`; `SortedConfigs` ranks previously-successful configs first so a second run finds a working config faster
- `internal/settings` — `RuntimeOptions` persisted to `%APPDATA%\sieve\settings.json` (ipset mode, domains, game filter, cache toggle) plus the ipset/domain list file manipulation (`lists.go`)
- `internal/maintenance` — `Service` behind both the launcher's maintenance menu and the one-shot CLI flags (stop, reset cache, update ipset, self-update); `diagnostics_windows.go`/`diagnostics_other.go` hold the `--diagnostics`/`--status`/`--clear-discord-cache` platform-specific checks (build-tag split, like `runner`)
- `internal/tray` — Windows system-tray icon via raw Win32 calls; only active when sieve owns its console (double-click launch), stubbed on other platforms
- `internal/runner` — starts/stops Sieve-owned `winws.exe` processes and streams their stdout/stderr; it intentionally does not stop/delete the global WinDivert service because other applications may share it
- `internal/tester` — HTTP reachability check against `discord.com` and `www.youtube.com`, used to judge whether a running config actually works
- `internal/selfupdate` — checks the latest sieve GitHub release, matches a compatible asset by name (`sieve-windows-amd64.exe` preferred, `sieve.exe` as legacy fallback — see naming note below), then swaps the running executable in place (rename aside → copy → hash-verify) and optionally relaunches into the same console; the `.old` backup is cleaned up on the next start
- `internal/admin`, `internal/paths` — UAC elevation and the `%APPDATA%\sieve` install dir, each with `_windows.go`/`_other.go` split
- `internal/github`, `internal/fsutil` — shared GitHub latest-release fetch (used by `assets` and `selfupdate`) and the atomic file-write helper (used by every JSON/list store)

Platform-specific files follow Go's `_windows.go` / `_other.go` filename build-tag convention throughout — when adding OS-specific behavior, follow that pattern rather than runtime `runtime.GOOS` checks.

### Release naming convention

CI (`.github/workflows/release.yml`) builds and attaches `sieve-windows-amd64.exe` to GitHub releases on `v*` tags — this is the canonical name going forward (Windows/amd64 convention). `internal/selfupdate/updater.go`'s `compatibleAsset()` whitelist must keep `sieve.exe` as a fallback for compatibility with older releases; don't drop it.

The Scoop bucket (`elev1e1nSure/scoop-bucket`, separate repo) has its own GitHub Actions workflow that polls `sieve`'s latest release and keeps `bucket/sieve.json`'s version/url/hash in sync automatically — no manual bucket maintenance needed after a release ships.

### Voice/copy

User-facing copy (README, `--help`, TUI chrome — title, empty/idle/exit states) intentionally carries a light literary tone built around the "sifting" metaphor, matching the project website. Diagnostic/error output (Windows service checks, self-update errors, etc.) stays plainly technical — don't extend the literary tone into troubleshooting text where clarity matters more than voice.
