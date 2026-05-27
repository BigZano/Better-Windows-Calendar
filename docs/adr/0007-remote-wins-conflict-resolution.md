# Remote-wins conflict resolution with toast confirmation and actionable queue

When both a local Event and its remote counterpart have changed since the last sync (detected via ETag change on remote + local `updated_ts` > `last_sync_at`), the default resolution policy is **remote-wins** — but the user is shown a toast notification with "Keep local" and "Accept remote" action buttons before the remote version is applied.

If the user takes no action within a configurable window (default: 5 minutes), remote-wins is applied automatically.

All conflicts — whether resolved via toast or auto-resolved — are written to a `conflicts` table in the DB. Both the local and remote versions are preserved as JSON blobs. The user can review and reverse any conflict resolution within a **30-day retention window**. Entries older than 30 days are pruned on startup. A badge count in the tray menu and an Alerts tab in the main app window surface pending conflicts for users who missed the toast.

`last-write-wins` (compare `updated_ts` on both sides; newer wins) is available as a power-user override via `[sync] conflict_resolution = "last-write-wins"` in `config.toml`.

## Why remote-wins as the default

The remote (Google, Outlook, CalDAV server) is the authoritative source. Remote-wins is the correct default for a calendar that syncs against a server that may receive edits from other devices (phone, web interface). The toast confirmation means the user is never silently overridden on a change they made in PyCalendar.

## Considered Options

- **Silent remote-wins:** safe, but no user control. Rejected: local changes can be discarded without notice.
- **Silent local-wins:** protects local edits but overwrites changes from phone/other devices. Rejected as default.
- **Last-write-wins as default:** intuitive but vulnerable to clock skew. Available as power-user option.
- **Manual resolution dialog (blocking):** most correct; wrong for a background sync service. Deferred — a dedicated conflict review UI may be added in a later milestone.
- **Remote-wins + toast + actionable queue (chosen):** non-blocking, user retains control, no data is permanently lost within the retention window.
