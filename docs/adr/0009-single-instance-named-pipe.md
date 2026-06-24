# Single-instance + `.ics` file-association launch via named-mutex + named pipe

PyCalendar supports double-clicking a `.ics` file to open it in the Import
Dialog of the **already-running** tray app, while guaranteeing only one tray
instance runs at a time. This is the named-pipe use case ADR-0008 deferred when
it moved OAuth off the `pycalendar://` / single-instance path onto a loopback
redirect.

## Decision

Two primitives in `internal/singleinstance` (Windows-only; no-op on other OSes):

- **Named mutex = single-instance authority.** `CreateMutexW` (kernel32 lazy
  proc) on `Local\PyCalendar-SingleInstance`. The creator is the **primary**;
  `ERROR_ALREADY_EXISTS` from the call ⇒ **secondary**. The mutex settles
  primary-vs-secondary because exactly one creator can win; a pipe listener is
  not exclusive (several processes could each open one) and so cannot prove
  single-instance on its own.
- **Named pipe = the arg channel.** `github.com/Microsoft/go-winio`
  `ListenPipe`/`DialPipe` on `\\.\pipe\PyCalendar`. The primary listens and calls
  `onMessage(payload)` per newline-delimited connection; a secondary dials it,
  writes its `.ics` path, and exits.

`Local\` (per-session) scope is chosen over `Global\` so single-instance is
scoped to the interactive desktop session — avoiding cross-session collisions on
multi-user / RDP machines.

## Scope

- **Windows-only.** `singleinstance_other.go` (`!windows`) returns
  `primary=true` with a no-op `release`, and `Forward` is a no-op, so every
  non-Windows launch behaves as before.
- **Tray mode only.** `bar` and `cli` are invoked repeatedly (a status bar runs
  `--mode bar` every second); only the `tray` case in `main.go` takes the mutex
  or touches the pipe. IPC failure never blocks startup — the app degrades to
  running as primary.

## Flow

1. `main.go` tray case detects the first positional `.ics` arg (`firstICSArg`).
2. `singleinstance.Acquire(onMessage)` where `onMessage = ui.OpenImportPath`.
3. Secondary ⇒ `Forward(path)` then exit. Primary ⇒ `ui.SetPendingImportPath`
   (opened once the Fyne loop is live in `RunTray`), and the listener opens any
   forwarded path. `OpenImportPath` marshals all UI work via `fyne.Do` since it
   runs on the pipe-server goroutine.

## Installer

`setup.iss` registers a `PyCalendar.ics` ProgID (`shell\open\command` =
`"{app}\pycalendar.exe" "%1"`) and advertises it for `.ics` via
`OpenWithProgids`, with `uninsdeletekey`/`uninsdeletevalue` for clean uninstall.
Windows 10/11 **UserChoice** blocks forcing the default handler programmatically,
so association is opt-in via "Open with" / Default Apps — expected, not a bug.

## Related

- ADR-0005 — original single-instance/named-pipe design (for OAuth URI scheme).
- ADR-0008 — moved OAuth to loopback+PKCE and explicitly deferred the
  single-instance / `.ics` file-association launch to a later slice; this ADR is
  that slice. OAuth uses the loopback HTTP listener, **not** this pipe.
