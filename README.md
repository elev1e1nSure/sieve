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

Quit with `q` or `Ctrl+C`. sieve kills `winws.exe` and cleans WinDivert service leftovers on exit.

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
