# Go-Deploy

Go-Deploy packages a Go app into a desktop-installable distribution with optional service mode.

It is designed for apps that should feel native after install:
- macOS: drag `.app` to `/Applications` from `.dmg`
- Ubuntu/Linux: install `.deb`
- Windows: run setup `.exe`

## What Go-Deploy Builds

Go-Deploy takes a **Go source directory** and produces a wrapper executable that embeds your app binary.

Supported output formats:
- `binary`: raw wrapped executable
- `dmg` (macOS targets)
- `deb` (Linux targets)
- `exe` installer (Windows targets, NSIS)
- `zip`

## Runtime Behavior

For generated apps:

- Two run modes:
  - `standalone`: run app process directly
  - `service`: install/start as OS service; if permission is unavailable from user launch, fallback to standalone
- Browser behavior:
  - opens configured `BrowserURL` on launch
  - defaults to `http://localhost:8080` when no URL is provided
  - if app is already running, re-launch opens browser again instead of starting another app instance
- Control behavior:
  - tray menu support for `Open`, `Stop`, `Quit Tray`
  - CLI fallback: `<app> --stop`

## Prerequisites

- Go `1.25+`
- Webview dependencies for builder UI (for example `webkit2gtk` on Linux)

Packaging tools by format:
- macOS `.dmg`: `hdiutil`
- Linux `.deb`: `dpkg-deb`
- Windows setup `.exe`: `makensis` (NSIS)

Tray/runtime notes:
- Linux may require appindicator/GTK tray dependencies for systray support

## Quick Start

### 1. Run builder

```bash
cd go-deploy
go mod tidy
go run main.go
```

### 2. Build release package

In the UI:
1. Set **Source Directory** to your Go project folder (with `go.mod`)
2. Set app metadata (Name, Version, Description)
3. Choose targets (OS/arch)
4. Choose default run mode (`standalone` or `service`)
5. Choose formats (`dmg`/`deb`/`exe`/etc.)
6. Build

Output goes to `./builds` by default.

## Install and Run

### macOS (`.dmg`)
1. Open DMG
2. Drag app to `/Applications`
3. Launch from Applications

### Ubuntu/Linux (`.deb`)
```bash
sudo dpkg -i your-app_<version>_<arch>.deb
```

### Windows (`setup .exe`)
1. Run installer `.exe`
2. Launch from Start Menu/desktop shortcut

## Stop the App

- Tray: use `Stop`
- CLI fallback:

```bash
/path/to/app --stop
```

## Production Checklist

Before shipping publicly, complete these steps:

1. Run `go mod tidy` to lock dependencies (`go.sum`) in the release branch.
2. Validate install/run/stop flows on all target OSes for both modes (`standalone` and `service`).
3. Sign release artifacts:
   - macOS: codesign + notarization for `.app`/`.dmg`
   - Windows: Authenticode signing for installers
4. Verify service install behavior with and without elevated permissions.
5. Add CI smoke tests for packaging outputs (`dmg`, `deb`, `exe`) and runtime relaunch behavior.

## Project Structure

- `main.go`: builder desktop app entrypoint
- `frontend/`: embedded UI
- `internal/builder/`: wrapper generation + packaging
- `pkg/wrapper/`: runtime behavior (run/service/browser/tray/stop)

## Sample App

Use `examples/sample-app` to test full flow end-to-end.

For browser-based tests, set `BrowserURL` to the sample app URL (for example `http://localhost:8080`).
