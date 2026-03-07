# Go-Deploy

Go-Deploy packages a Go app into a desktop-installable distribution with optional service mode. It provides a visual builder for managing build configurations across multiple target platforms.

It is designed for apps that should feel native after install:
- **macOS**: drag `.app` to `/Applications` from `.dmg`
- **Ubuntu/Linux**: install `.deb` or `.rpm`
- **Windows**: run setup `.exe`

## What Go-Deploy Builds

Go-Deploy takes a **Go source directory** and produces a lightweight wrapper executable that embeds your app binary. This wrapper handles:
- **Auto-Installation**: Optional service mode for background operation.
- **Runtime Persistence**: Built-in persistence for user preferences.
- **Inter-Process Communication**: Graceful stopping and relaunching behavior.

Supported output formats:
- `binary`: Raw wrapped executable
- `dmg`: macOS installer
- `deb`: Debian/Ubuntu package
- `rpm`: Fedora/RHEL package
- `exe`: Windows setup installer (NSIS)
- `zip`: Portable compressed archive

## Runtime Behavior

Generated applications support:

- **Two Run Modes**:
  - `standalone`: Runs the app process directly in the foreground.
  - `service`: Installs and starts as an OS service (auto-starts on boot).
- **Browser Integration**:
  - Launches the configured `BrowserURL` on startup.
  - Defaults to `http://localhost:8080`.
- **System Tray**:
  - Desktop integration with `Open`, `Stop`, and `Quit` options.
- **CLI Management**:
  - `<app> --stop`: Signals the running instance to exit.
  - `<app> --version`: Displays the application version.

## Prerequisites

- Go `1.25+`
- **WebView Dependencies**: Required for the builder GUI (e.g., `webkit2gtk` on Linux).

### Packaging Tools
Ensure target-specific tools are in your `PATH`:
- **macOS `.dmg`**: `hdiutil`
- **Linux `.deb`**: `dpkg-deb`
- **Linux `.rpm`**: `rpmbuild`
- **Windows `.exe`**: `makensis` (NSIS)

## Quick Start

### 1. Launch the Builder
```bash
go run main.go
```
This opens the Go-Deploy GUI.

### 2. Manage Projects
- **Dashboard**: View and reload recent build configurations.
- **Configuration**:
    1. Select **Source Directory** (must contain `go.mod`).
    2. Set **App Metadata** (Name, Version, Description).
    3. Configure **Deliverables**: Select target OS/Arch combinations and formats.
    4. Define **Environment Variables**: Add key-value pairs or load from a `.env` file.
    5. Choose **Default Mode**: Standalone or Service.

### 3. Build
Click **Build Release**. Output is generated in `./builds` (or your custom directory).

## Project Structure

- `main.go`: Entrypoint for the WebView-based builder GUI.
- `frontend/`: Vanilla JS/CSS UI for the builder.
- `internal/builder/`: Modular engine for wrapper generation and packaging.
- `internal/db/`: Persistent storage for build configurations (BadgerDB).
- `pkg/wrapper/`: Runtime logic for the generated distribution (service management, tray, lifecycle).

## Sample App

Use `examples/sample-app` to test the full end-to-end packaging flow.

