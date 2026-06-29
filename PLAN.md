# sieve — plan

TUI/CLI tool for auto-selecting a working zapret config for Discord + YouTube on Windows.
Single portable `.exe`. No dependencies on flowseal folder.

---

## What sieve does

Пользователь запускает `sieve.exe`. Дальше всё происходит само:

**1. Старт**
Программа проверяет права администратора. Если их нет — показывает UAC-запрос и перезапускает себя от админа.

**2. Обновление ассетов**
Проверяет последний релиз flowseal на GitHub. Если локальных файлов нет или версия устарела — скачивает архив (`winws.exe`, `.bin` файлы, листы хостов) и распаковывает в `%APPDATA%\sieve\`. Прогресс виден в TUI.

**3. Перебор конфигов**
Запускает 20 захардкоженных конфигов winws один за одним. Порядок умный:
- сначала тот, что сработал в прошлый раз
- потом те, что когда-либо работали (от свежих к старым)
- потом непроверенные
- в конце заведомо нерабочие

Каждый конфиг: запустить winws → подождать 1-2 сек → проверить HTTP-соединение к `discord.com` и `www.youtube.com` → если оба ответили — стоп. Таймаут проверки 5 сек по умолчанию, меняется флагом `--test-timeout N`.

**4а. Нашли рабочий**
winws остаётся запущенным с победившим конфигом. TUI переходит в режим "живого лога" — показывает имя конфига и стриминг вывода winws в реальном времени. Результат сохраняется в кэш (`%APPDATA%\sieve\cache.json`) чтобы в следующий раз начать с него.

**4б. Ничего не сработало**
TUI показывает сообщение что ни один конфиг не помог. winws не запущен. Пользователь может нажать `q` и выйти.

**Выход**
`q` или `Ctrl+C` — убивает winws если запущен, чисто завершается.

---

## Flags

- `--test-timeout N` — таймаут проверки соединения в секундах (default: 5)

---

## Phase 1 — Project skeleton

- [x] `go mod init github.com/your-name/sieve`
- [x] Add dependencies: bubbletea, lipgloss, bubbles
- [x] Directory structure: `internal/{admin,assets,configs,runner,tester,cache,ui}`
- [x] `main.go`: parse flags, wire everything together
- [x] `.gitignore`

## Phase 2 — Admin elevation

- [x] `internal/admin`: `IsAdmin() bool` via Windows API
- [x] `ElevateAndRestart()`: re-launch self with `ShellExecute` + "runas" verb if not admin
- [x] Called at startup before anything else

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
- [ ] Custom `http.Client` with `--test-timeout` timeout (default 5s)
- [ ] Both must return 2xx/3xx to count as success
- [ ] Returns `TestResult{Discord, YouTube bool, Err error}`

## Phase 8 — TUI

- [ ] `internal/ui`: bubbletea model
- [ ] States:
  - `StateUpdating` — checking/downloading flowseal assets
  - `StateTesting` — iterating configs, show current name + progress (N/20)
  - `StateRunning` — found working config, winws is live, show scrolling log
  - `StateNoLuck` — nothing worked, friendly message
- [ ] `q` / `ctrl+c` to quit (kills winws if running)
- [ ] Lipgloss for layout: header, status line, log pane

## Phase 9 — Wire up + polish

- [ ] Main loop: elevate → update assets → load cache → sort configs → test each → keep winner
- [ ] `--test-timeout N` flag (int seconds, default 5)
- [ ] Clean exit: kill winws on quit
- [ ] Build: `GOOS=windows GOARCH=amd64 go build -o sieve.exe .`

## Phase 10 — Release

- [ ] Tag v0.1.0
- [ ] GitHub Actions: build `.exe` on push to `main`, attach to release
- [ ] `README.md`: one-paragraph description + download link

---

## Decisions made

- `-H console` (default) — TUI требует консоль
- `--test-timeout` принимает int (секунды)
- История в TUI — только текущий прогон
- Ассеты качаются с flowseal репо, не с bol-van
