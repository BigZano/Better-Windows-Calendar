# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

PyCalendar is a Go desktop calendar application (rewritten from Python — the name and `.python-version`/`.venv` are remnants). It runs as a system tray app, a background reminder daemon, a status-bar formatter, or a CLI tool.

## Build & Run

```powershell
# Build
go build ./...

# Run in each mode
go run . --mode tray           # system tray + Fyne GUI (default)
go run . --mode daemon         # background reminder polling service
go run . --mode bar            # print upcoming events to stdout (for status bars)
go run . --mode bar --format json      # Waybar JSON output
go run . --mode bar --format polybar   # Polybar plain-text output

# CLI event management
go run . --mode cli add --title "Meeting" --start "2026-05-15 14:00" --reminder 10
go run . --mode cli list

# Tests
go test ./...
go test ./internal/api/...     # single package
```

## Architecture

The app has four run modes, all sharing the same `internal/` packages:

```
main.go          — flag parsing, dispatches to tray / daemon / bar / cli
internal/
  api/           — CRUD layer: CreateEvent, GetUpcoming, GetEvents, GetDueReminders, UpdateEvent, DeleteEvent
  storage/       — SQLite connection management (WAL mode, retry with backoff), schema init
  config/        — TOML config load/save; writes defaults on first run
  daemon/        — polling loop (30s interval); fires desktop + mobile push notifications
  notifier/      — thin wrapper around beeep; HTML-escapes inputs before delivery
  autostart/     — build-tagged: Windows (registry HKCU\...\Run) and Linux (systemd/XDG)
ui/
  tray.go        — systray entry point (RunTray), menu items, tooltip refresh
  window.go      — Fyne windows: calendar list, add-event form, settings dialog
  bar.go         — FormatText / FormatJSON / FormatPolybar for status bar output
```

**Data flow**: UI and daemon both call `internal/api`, which calls `internal/storage.Open()` for each operation (short-lived connections). The daemon suppresses duplicate reminders within a session using an in-memory `notified` map.

## Data & Config Locations

| Platform | Database | Config |
|----------|----------|--------|
| Windows  | `%LOCALAPPDATA%\PyCalendar\PyCalendar\pycalendar.db` | `%LOCALAPPDATA%\PyCalendar\PyCalendar\config.toml` |
| Linux    | `$XDG_DATA_HOME/PyCalendar/pycalendar.db` or `~/.local/share/PyCalendar/pycalendar.db` | `$XDG_CONFIG_HOME/PyCalendar/config.toml` or `~/.config/PyCalendar/config.toml` |

## Key Design Decisions

- **`UpdateEvent` uses an allowlist** (`allowedUpdateFields` in `api/api.go`) to prevent SQL column injection when building dynamic `SET` clauses.
- **Daemon reminder window**: checks for reminders due within the next 120 seconds on each 30-second tick — events can appear in up to 4 consecutive polls before their reminder fires.
- **Mobile push SSRF guard**: webhook URL must use `https://`; HTTP is rejected at `daemon/daemon.go`.
- **Notifier HTML-escaping**: beeep uses Windows Toast XML internally; inputs are HTML-escaped to prevent XML injection.
- **Fyne app singleton**: `ui/window.go` keeps a package-level `fyneApp` to avoid initializing multiple Fyne app instances when opening multiple windows.
- **Tray icon**: currently a no-op placeholder; a real icon requires embedding a PNG via `//go:embed assets/icon.png`.

## Dependencies

- `fyne.io/fyne/v2` — GUI framework (calendar window, add-event dialog, settings)
- `github.com/getlantern/systray` — system tray (separate from Fyne's own systray)
- `github.com/BurntSushi/toml` — config file encoding/decoding
- `github.com/gen2brain/beeep` — cross-platform desktop notifications
- `modernc.org/sqlite` — pure-Go SQLite driver (no CGo required)
- `golang.org/x/sys` — Windows registry access for autostart

## Agent skills

### Issue tracker

Issues live in GitHub Issues (`BigZano/Better-Windows-Calendar`). See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-label vocabulary (needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `CONTEXT.md` + `docs/adr/` at the repo root. See `docs/agents/domain.md`.
