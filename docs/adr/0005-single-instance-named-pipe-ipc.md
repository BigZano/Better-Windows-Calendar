# Single-instance enforcement with named pipe IPC for URI scheme handling

PyCalendar enforces single-instance at the OS level using a named mutex. When Windows activates the `pycalendar://` custom URI scheme (e.g., after an OAuth redirect), it may launch a second process. That second instance detects the running mutex, forwards its startup URI to the first instance via a named pipe, and exits immediately. The running tray app receives the URI, handles the OAuth token exchange, shows a confirmation toast, and updates the calendar sidebar in real time.

The alternative — letting the second instance handle the OAuth callback standalone (write token to keyring, exit) — was rejected because:
- Failed token exchanges are invisible to the user (no toast, no error surface)
- The running tray app cannot immediately confirm the connection or refresh the calendar list
- Two instances writing to the keyring concurrently introduces a race on the credential_index table

The named mutex + named pipe pattern is the standard Windows approach for protocol-activated single-instance apps.

## URI scheme registration

`pycalendar://oauth/callback` is registered in `setup.iss` via `[Registry]` entries pointing to `pycalendar.exe "%1"`. The scheme is also used for `.ics` file association (see setup.iss `[Registry]` section).

## Considered Options

- **Second instance handles OAuth standalone:** simpler, no IPC. Rejected: silent failure mode, no real-time UI update.
- **Localhost loopback redirect server:** no OS registration needed; dynamic port. Rejected: firewall noise, dynamic port requires Azure app registration to whitelist a pattern rather than a specific URI.
- **Single-instance + named pipe (chosen):** standard Windows pattern; real-time UI feedback; clean failure surface.
