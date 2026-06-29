# sieve — plan

TUI/CLI tool for auto-selecting a working zapret config for Discord + YouTube on Windows.
Single portable `.exe`. No dependencies on flowseal folder.

---

## Phase 1 — Project skeleton

- [ ] `go mod init github.com/your-name/sieve`
- [ ] Add dependencies: bubbletea, lipgloss, bubbles
- [ ] Directory structure: `internal/{admin,assets,configs,runner,tester,cache,ui}`
- [ ] `main.go`: parse flags (`--test-timeout`), wire everything together
- [ ] `.gitignore`

## Phase 2 — Admin elevation

- [ ] `internal/admin`: `IsAdmin() bool` via Windows API
- [ ] `ElevateAndRestart()`: re-launch self with `ShellExecute` + "runas" verb if not admin
- [ ] Called at startup before anything else

## Phase 3 — Asset manager

- [ ] `internal/assets`: define install dir (`%APPDATA%\sieve`)
- [ ] On every launch: fetch latest release metadata from flowseal GitHub API
- [ ] Compare with local version file (`version.txt`)
- [ ] If outdated or missing: download release zip, extract `bin/` and `lists/` into install dir
- [ ] Show progress in TUI during download

## Phase 4 — Configs

- [ ] `internal/configs`: hardcode all 20 winws configs extracted from flowseal bat files
- [ ] Each config: `Name string`, `Args []string` with `{BIN}` and `{LISTS}` placeholders
- [ ] `Resolve(binDir, listsDir string) []string` — fills in real paths at runtime

## Phase 5 — Cache

- [ ] `internal/cache`: JSON file at `%APPDATA%\sieve\cache.json`
- [ ] Stores: last working config name, per-config success/fail counts, last success timestamp
- [ ] `SortedConfigs(all []Config) []Config`:
  - 1st: last working config (if any)
  - 2nd: previously successful, sorted by recency
  - 3rd: untested
  - 4th: previously failed
- [ ] Load on startup, save after each test result

## Phase 6 — Runner

- [ ] `internal/runner`: start `winws.exe` with given args as a subprocess
- [ ] Capture stdout+stderr → channel for live log streaming
- [ ] `Stop()`: kill process, wait for WFP filters to clear (small sleep)
- [ ] At startup: kill any existing winws.exe before starting new one

## Phase 7 — Tester

- [ ] `internal/tester`: HTTPS GET to `discord.com` and `www.youtube.com`
- [ ] Custom `http.Client` with `--test-timeout` timeout (default 5s), no redirects limit
- [ ] Both must return 2xx/3xx to count as success
- [ ] Returns `TestResult{Discord, YouTube bool, Err error}`

## Phase 8 — TUI

- [ ] `internal/ui`: bubbletea model
- [ ] States:
  - `StateUpdating` — checking/downloading flowseal assets
  - `StateTesting` — iterating configs, show current name + progress (N/20)
  - `StateRunning` — found working config, winws is live, show scrolling log
  - `StateNoLuck` — nothing worked, friendly message
- [ ] `q` / `ctrl+c` to quit (and kill winws if running)
- [ ] Lipgloss for layout: header, status line, log pane

## Phase 9 — Wire up + polish

- [ ] Main loop: elevate → update assets → load cache → sort configs → test each → keep winner
- [ ] `--test-timeout n` flag (seconds)
- [ ] Clean exit: kill winws on quit
- [ ] Build: `GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui" -o sieve.exe .`
  (or `-H console` — decide based on TUI behavior)

## Phase 10 — Release

- [ ] Tag v0.1.0
- [ ] GitHub Actions: build `.exe` on push to `main`, attach to release
- [ ] `README.md`: one-paragraph description + download link

---

## Open questions

- [ ] Should `--test-timeout` accept float (e.g. `2.5`) or just int seconds?
- [ ] Show per-config test history in TUI or just current run?
- [ ] `-H windowsgui` hides console but TUI still works in Windows Terminal — test this
