# PyCalendar

A desktop calendar for Windows and Linux. No Microsoft account required.

<!-- screenshot placeholder -->

## Features

- **System tray** — lives quietly in the tray; reminder daemon runs embedded
- **Recurring events** — full RRULE engine; edit just this / this and following / all occurrences
- **Multiple calendars** — create, color-code, and overlay any number of calendars
- **Categories** — tag events and filter by category across all calendars
- **Attachments** — attach meeting links (Zoom, Teams, etc.) to events
- **Status bar output** — pipe upcoming events to komorebi-bar, Waybar, or Polybar
- **Reminders** — desktop notifications + optional mobile push webhook
- **Autostart** — start with Windows/Linux from Settings

---

## Install

### Windows

Download **`PyCalendarSetup-vX.Y.Z.exe`** from the [latest release](https://github.com/BigZano/Better-Windows-Calendar/releases/latest) and run it.

Or grab the standalone exe (no installer, just run it):

```
pycalendar.exe
```

### Linux

```bash
curl -fsSL https://github.com/BigZano/Better-Windows-Calendar/releases/latest/download/install.sh | bash
```

Installs to `/usr/local/bin/pycalendar` and drops a `.desktop` entry.

---

## Usage

```
pycalendar                        # tray mode (default)
pycalendar --mode tray            # system tray + calendar window
pycalendar --mode daemon          # headless reminder daemon (Linux servers)
pycalendar --mode bar             # print upcoming events (status bars)
pycalendar --mode bar --format json      # Waybar JSON
pycalendar --mode bar --format polybar   # Polybar plain text
```

### CLI event management

```
pycalendar --mode cli add --title "Team sync" --start "2026-06-01 14:00" --reminder 10
pycalendar --mode cli list
```

### Uninstall

```
pycalendar --mode uninstall          # remove keychain entries + autostart
pycalendar --mode uninstall --purge  # also delete database + config
```

---

## Status bar integration

Open **Settings → General → Set up bar integration**.

PyCalendar detects installed bar apps and injects the widget config non-destructively:

| Platform | Bar | Format |
|----------|-----|--------|
| Windows  | komorebi-bar | `--format json` |
| Linux    | Waybar | `--format json` |
| Linux    | Polybar | `--format polybar` |

Manual config snippet (if auto-setup doesn't cover your bar):

```bash
# Waybar custom module in ~/.config/waybar/config
"custom/pycalendar": {
    "exec": "pycalendar --mode bar --format json",
    "interval": 60,
    "return-type": "json"
}
```

---

## Data locations

| Platform | Database | Config |
|----------|----------|--------|
| Windows  | `%LOCALAPPDATA%\PyCalendar\PyCalendar\pycalendar.db` | `%LOCALAPPDATA%\PyCalendar\PyCalendar\config.toml` |
| Linux    | `~/.local/share/PyCalendar/pycalendar.db` | `~/.config/PyCalendar/config.toml` |

---

## Build from source

Requires Go 1.22+. No CGo, no system libraries needed on any platform.

```bash
git clone https://github.com/BigZano/Better-Windows-Calendar
cd Better-Windows-Calendar
go build ./...
go run . --mode tray
```

**Windows release build** (produces `dist/pycalendar.exe` with icon + version info):

```powershell
.\scripts\build-windows.ps1 -Version "1.0.0"
```

Add `-Installer` to also produce a Setup.exe (requires [Inno Setup 6](https://jrsoftware.org/isinfo.php)):

```powershell
.\scripts\build-windows.ps1 -Version "1.0.0" -Installer
```

**Linux release build:**

```bash
bash scripts/build-linux.sh 1.0.0
# -> dist/pycalendar-1.0.0-linux-amd64.tar.gz
```

**Tests:**

```bash
go test ./...
```

---

## Attributions

See [ATTRIBUTIONS.md](ATTRIBUTIONS.md).
