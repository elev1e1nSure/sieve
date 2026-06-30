# sieve

sieve is a portable Windows TUI tool that downloads Flowseal zapret assets, tries bundled Discord + YouTube `winws` configs, keeps the first working config running, and remembers successful configs for faster next launches.

## Requirements

- Windows
- Administrator rights at runtime
- Go 1.26+
- [`just`](https://github.com/casey/just) for local dev commands

## Usage

Build and run:

```powershell
just build
.\sieve.exe
```

Run from source:

```powershell
just run
```

Use custom connectivity timeout:

```powershell
just run-timeout 10
.\sieve.exe --test-timeout 10
```

Flags do not start the TUI. They save settings or run one maintenance action, print the result, and exit.
Start the bypass only by running without flags:

```powershell
.\sieve.exe
```

Reset cached config results before running:

```powershell
.\sieve.exe --reset-cache
```

Disable config cache for the current run:

```powershell
.\sieve.exe --no-cache
```

Configure Flowseal lists before running:

```powershell
.\sieve.exe --update-ipset --ipset loaded
.\sieve.exe --ipset none
.\sieve.exe --ipset any
.\sieve.exe --domain discord.media --domain-file .\domains.txt
```

Enable game traffic filters:

```powershell
.\sieve.exe --game all
.\sieve.exe --game tcp
.\sieve.exe --game udp
```

Run maintenance checks:

```powershell
.\sieve.exe --diagnostics
.\sieve.exe --diagnostics --fix
.\sieve.exe --clear-discord-cache
```

Show build metadata:

```powershell
.\sieve.exe --version
```

Update sieve itself from the latest GitHub release:

```powershell
.\sieve.exe --update
```

The release must contain a compatible `sieve.exe` asset. Public releases work without extra setup. For private release testing, set `GH_TOKEN` or `GITHUB_TOKEN` before running `--update`. If an update is found during a normal no-flag launch, sieve replaces itself and restarts.

On startup, sieve adds its executable directory to the current user's `PATH`.
Skip that behavior when needed:

```powershell
.\sieve.exe --no-add-path
```

Quit with `q` or `Ctrl+C`. sieve kills `winws.exe`, cleans WinDivert service leftovers, and leaves only `Bye!` on exit.

## Dev Commands

List commands:

```powershell
just
```

Format:

```powershell
just fmt
```

Test:

```powershell
just test
```

Build local exe:

```powershell
just build
```

Build release exe:

```powershell
just release-build
```

Clean build output:

```powershell
just clean
```

## Runtime Data

sieve stores downloaded Flowseal assets and cache under:

```text
%APPDATA%\sieve
```

Saved CLI settings live in the same directory as `settings.json`.
