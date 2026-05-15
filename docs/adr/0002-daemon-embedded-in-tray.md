# Daemon embedded in tray process; headless mode retained for Linux

On Windows and Linux desktop installs, the reminder Daemon runs as a goroutine inside the tray process — one binary, one autostart entry, reminders work out of the box.

The obvious alternative — two separate processes (tray + daemon), each with their own autostart entry — was rejected because it creates unnecessary install friction: users would have to launch or configure both, and a missed daemon autostart entry means silent reminder failures with no visible error.

`--mode daemon` is retained as a first-class run mode for **headless Linux installs** (servers, machines without a display). The first-run setup detects whether a display is present and registers either a tray autostart entry or a systemd unit accordingly. Ease of use is the deciding constraint in both cases.

## Consequences

- Tray mode carries a slightly larger footprint (polling goroutine always running).
- Power users who want daemon-without-tray on a desktop Linux machine must use `--mode daemon` explicitly — there is no headless-desktop autostart path out of the box.
