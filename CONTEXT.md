# PyCalendar — Domain Glossary

A Windows/Linux calendar replacement that works without a Microsoft account. Events and calendars are stored locally by default; external calendars (CalDAV, Google) sync into the same local store. First-class integration with bar apps (Waybar, Komorebi, Windows Taskbar/Tray).

---

## Calendar

A named, color-coded collection of Events. Every Calendar has a **source type**:

- `local` — events live only in the local SQLite database
- `caldav` — events sync from a CalDAV server (e.g. Nextcloud, iCloud, Fastmail)
- `google` — events sync from Google Calendar

A user may have multiple Calendars. The **Active Calendar** is the one currently shown as the base layer. Additional calendars may be **overlaid** on top of it (see Calendar Stack). The default Calendar created on first run is named "Local" (id=1, type=local).

**Do not say:** "calendar source", "calendar provider". Say: **Calendar** or **Calendar type**.

---

## Calendar Stack

The set of Calendars currently visible in the UI. It always has exactly one **Active Calendar** (the base layer). Zero or more additional Calendars may be **overlaid** on top. Each Calendar in the stack is distinguished by its color.

Example: C1 is the Active Calendar; the user toggles C2 on → Stack is [C1, C2]. Toggle C3 on → Stack is [C1, C2, C3].

---

## Event

A time-bounded occurrence that belongs to exactly one Calendar. An Event has:

- a required **Title** and **Start time**
- an optional **End time**, **Notes**, **Location**, **URL**
- an optional **Reminder** (see below)
- an optional **Recurrence rule** (iCal RRULE string)
- an **All-day** flag

---

## BSP Event Layout

When two or more Events occupy the same time slot in the calendar view, they are displayed as side-by-side columns using a Binary Space Partitioning rule:

- 1 event: full width
- 2 events: split 50/50 left-right
- 3+ events: always split the **widest** (largest) existing block in half

Each block shows the Event's **Title** and its Calendar's **color**. Splitting the largest block keeps the layout balanced and maximally legible as events accumulate. A **Pop-out View** is available for crowded slots (see below).

**Do not say:** "overlapping events", "collision columns". Say: **BSP layout** or **event columns**.

---

## Category

A freeform tag applied to an Event at creation time (or edit time). Categories are cross-cutting — they work across all Calendars. An Event may have zero or more Categories. Categories are the primary mechanism for searching and filtering Events.

The `categories` table holds the canonical tag names; `event_categories` is the join table. An index on `event_categories(category_id)` enables fast "find all events with tag X" queries.

**Do not say:** "label", "tag type", "calendar type". Say: **Category**.

**Differs from Calendar**: a Calendar owns and sources Events; a Category labels them across ownership boundaries.

---

## Pop-out View

A secondary window opened from a busy time slot that displays all Events in that slot in a full-detail, scrollable list. Triggered when the BSP columns become too narrow to read comfortably.

---

## Recurring Event

An Event with a non-null `recurrence_rule` (iCal RRULE string). The **master event** row holds the RRULE; individual occurrences are generated at query time by an RRULE expansion engine.

When a user edits a single occurrence of a Recurring Event, a prompt asks which scope to apply the change to:

1. **Just this occurrence** (default) — creates an **Exception** row linked back to the master via `parent_event_id`; the master RRULE is unchanged
2. **This and all following** — splits the series: master RRULE gains an `UNTIL` bound before this date; a new master is created from this occurrence forward with the edit applied
3. **All occurrences** — modifies the master event directly; all existing exceptions are cleared

**Exception**: an Event row with a non-null `parent_event_id` pointing to its master Recurring Event. Exceptions override the master for their specific date.

**Not yet implemented.** The `recurrence_rule` column exists; the expansion engine and `parent_event_id` field do not.

---

## Reminder

A desktop notification (and optionally a mobile push) dispatched N minutes before an Event's Start time. Every Event has at most one Reminder. The offset is stored as an absolute Unix timestamp (`reminder_ts`) computed at creation time.

---

## Daemon

The background polling service. Wakes every 30 seconds, queries for Events whose `reminder_ts` falls within the next 120 seconds, and fires the Reminder. Suppresses duplicate notifications within a session using an in-memory set.

When running in **tray mode**, the Daemon runs as an embedded goroutine inside the same process — no separate launch required.

`--mode daemon` remains available for **headless Linux installs** (servers, no display). In that case the install flow registers a systemd unit instead of a tray autostart entry. The first-run setup detects which mode applies and configures accordingly.

---

## Attendee

A person associated with an Event. Stored as `name`, `email`, and `status` (e.g. accepted/declined/unknown). Attendees are **informational by default** — they record who is in a meeting without requiring any action.

Two additional behaviors layer on top:

1. **Import preservation** — when Events are pulled in via CalDAV, Google Calendar, or `.ics` import, existing attendee data is stored as-is.
2. **Invite prompt** — when a user creates or edits an Event on a synced Calendar (CalDAV or Google) and Attendees are present, a dismissible prompt asks whether to push invites through that external service. This prompt can be permanently muted per-Calendar in Settings.

**Requires an external account** for the invite-push path. Purely local Calendars never trigger the prompt.

---

## Attachment

A URL or file blob linked to an Event. Primarily used for **meeting join-links** (Zoom, Teams, etc.) but can hold any URL or binary file. An Event may have multiple Attachments. Attachments are displayed in the Event detail view.

Blocked on the Event detail view existing — not low value, just sequentially dependent.

---

## Sync

The process of pulling Events from a remote CalDAV, Google Calendar, or Outlook Calendar (Microsoft Graph) into the local SQLite store, and pushing local changes back. Sync state (sync token, per-resource ETags) is persisted in `sync_state` so incremental syncs are possible.

**Milestone 2 (sync milestone).** The schema, both Adapters (CalDAV, Microsoft Graph), and the Sync Engine are implemented and wired into the tray and daemon run modes via the `syncwire` boot package. CalDAV is reachable end-to-end from the UI today: the Calendars settings tab adds a CalDAV Calendar (URL + username + app-password → keyring) and offers a per-Calendar **Sync Now** button. Calendars added or removed at runtime register with the live engine immediately (`syncwire.RegisterCalendar`/`UnregisterCalendar`); the engine also runs a background sync on its timer. Microsoft Graph is implemented but inert until the OAuth login flow (ADR-0005: custom URI scheme, single-instance mutex, named-pipe IPC) lands — there is no way to obtain a refresh token yet. `.ics` file import is implemented as an in-app Import Dialog (see below); file-association/double-click launch is deferred to the OAuth slice.

Sync is two-way. Pull: each sync `FetchChanges` from the remote and applies upserts/deletes to the local store. Push: local create/update/delete on a synced Calendar enqueue an entry in the `sync_outbox` table (`upsert`/`delete`); each sync drains the outbox before pulling, calling `PushChange`/`DeleteRemote`. A create-push links the local Event to its newly assigned remote resource (`resource_url`) so the next fetch recognises it rather than re-importing a duplicate; the matching ETag also lets `applyChange` skip our own change when the remote echoes it back. Remote-originated writes use the non-enqueueing `…FromRemote` API helpers so they are never echoed back to the server.

Sync runs on its own goroutine with its own configurable timer (default 5 minutes), independent of the reminder Daemon. See ADR-0002 for the embedded goroutine decision. `syncwire` lives above the protocol adapters because they import `syncer`; placing the adapter factory there avoids an import cycle.

---

## Sync Engine

The module responsible for orchestrating Sync for one or all Calendars. Exposes three methods to callers (Daemon, "Sync Now" UI button):

- `Sync(ctx, calendarID) error` — sync a single Calendar
- `SyncAll(ctx) error` — sync all sync-enabled Calendars
- `Status(calendarID) SyncStatus` — last sync time, in-progress flag, last error

A companion `SyncEventSource` interface is defined alongside for future event-driven sync (callers type-assert to it when available). Behind the seam the engine dispatches to a protocol **Adapter** (`FetchChanges`, `PushChange`, `DeleteRemote`). CalDAV and Microsoft Graph are distinct Adapters; Google Calendar is a third Adapter, deferred but designed for.

**Do not say:** "sync service", "sync worker". Say: **Sync Engine** or **Adapter**.

---

## SyncState

A typed wrapper around the per-Calendar sync state stored in the `sync_state` table. Exposes `GetETag(resourceURL string) string` and `SetETag(resourceURL, etag string)` methods. The JSON serialization of the underlying ETag map is an implementation detail hidden behind this type — callers never marshal or unmarshal raw JSON.

Also carries the sync token (used for incremental CalDAV and Graph fetches) and `last_sync_at` timestamp.

---

## CredentialStore

The module that manages OAuth tokens and Basic Auth credentials for sync-enabled Calendars. Typed methods per Calendar type:

- `StoreOAuthToken(calendarID, token OAuthToken) error`
- `GetOAuthToken(calendarID) (OAuthToken, error)`
- `StoreCalDAV(calendarID, username, password string) error`
- `GetCalDAV(calendarID) (username, password string, error)`

`OAuthToken` holds `{refresh_token []byte, scope string, obtained_at time.Time}`. The access token is never stored — it is fetched fresh from the provider on every sync operation and zeroed after use (see ADR-0004). All token values are `[]byte`, not `string`, to allow explicit memory zeroing.

OAuth client IDs ship as built-in defaults in the binary. Per-provider overrides live in `config.toml` under `[oauth]` (`microsoft_client_id`, `google_client_id`). The OS keyring (via `internal/keychain`) is the backing store; the `credential_index` table tracks every keyring entry for clean uninstall.

**Do not say:** "credential manager", "token store". Say: **CredentialStore**.

---

## Conflict Queue

The persistent record of sync conflicts that have not yet been reviewed by the user. When both a local Event and its remote counterpart have changed since the last sync, the conflict is written to the `conflicts` table (both versions as JSON blobs) before the default resolution (remote-wins) is applied. The user has a **30-day retention window** to reverse the decision. Entries are pruned on startup after the window expires.

Surfaced in two places:
1. **Toast notification** at conflict time — "Keep local" / "Accept remote" action buttons. *(Not yet implemented — pending the OAuth/desktop-notification path.)*
2. **Alerts tab** — implemented. A scrollable list of pending conflicts (in the Settings window and as a standalone window opened from the tray), each showing the local-vs-remote summary with **Keep local** / **Accept remote** buttons. A "⚠ N sync conflicts" item appears in the tray menu when conflicts are pending (refreshed after each sync and on a 60-second tick). "Accept remote" just marks the conflict resolved (remote-wins was already applied); "Keep local" restores the preserved local version (`syncer.ResolveKeepLocal`).

`last-write-wins` is available as a power-user override via `[sync] conflict_resolution = "last-write-wins"` in `config.toml`. See ADR-0007.

**Do not say:** "conflict log", "merge queue". Say: **Conflict Queue**.

---

## EventPatch

A struct used to partially update an Event. Every field is a pointer; `nil` means "leave this field untouched." `UpdateEvent(id, patch EventPatch)` applies all non-nil fields in a single SQL transaction. Replaces the previous `map[string]any` pattern.

The atomicity guarantee is the primary motivation: the Sync Engine builds one EventPatch from the winning version of a conflicted event and applies it as a single atomic write. See ADR-0006.

---

## Import Dialog

The in-app UI for importing a `.ics` file, launched from the Settings **Import** tab (a thin launcher that calls `ui.ShowImportDialog`). Implemented in two phases so a preview is shown before any write: `icsimport.Parse(r)` returns a `Preview` (no DB writes); `icsimport.Commit(preview, calendarID)` writes the events. Displays:

- PRODID / organizer from the ICS header
- Event count and date span ("47 events, Jan 2025 – Dec 2025")
- Scrollable preview list (title + start date per event)
- Calendar picker (defaults to Local calendar)

Safeguards: 10MB file size limit (rejected before parsing); warning banner for imports over 50 events; **deduplication** against the target calendar — identity is the iCal **UID**, falling back to **(title, start timestamp)** when the UID is absent, always scoped to the chosen calendar (the same event in a different Calendar is not a duplicate). Duplicates are skipped and summarised in a post-import report ("Imported 44, 3 skipped (already exist)"). Imported events **carry no auto-reminder** — `reminder_ts` is left NULL unless the VEVENT has a parseable VALARM trigger — and store a real timezone (from DTSTART's TZID, else UTC), never the literal "import". No URLs or alarm actions embedded in the ICS are auto-executed. Escape closes the dialog without importing; Enter does not confirm.

Dedup relies on the `uid` column added in **migration v6** (`internal/storage/storage.go`), with `idx_events_uid` for lookups.

**Deferred:** `.ics` file association (double-click to open), positional-arg routing in `main.go`, and the single-instance mutex / named-pipe IPC are part of the later OAuth slice (ADR-0005), not the in-app dialog.

**Do not say:** "import wizard", "ICS importer". Say: **Import Dialog**.

---

## Calendar View

The primary UI surface for browsing Events. Three views are available:

- **Day view** — shows all Events for a single day in a time-grid (default on open)
- **Week view** — shows a 7-day time-grid
- **Month view** — shows a traditional month grid; each day cell shows event titles truncated to fit

The active view persists across sessions. BSP Event Layout applies within any view when Events share a time slot.

---

## Settings Panel

A dedicated configuration window (separate from any main calendar window). Stores user preferences in `config.toml`. Settings include at minimum:

- Default calendar view (Day / Week / Month)
- Theme (System / Light / Dark / Retro)
- Desktop notification toggle and default reminder offset
- Sound toggle
- Autostart toggle
- Mute invite prompts (per-Calendar)
- Sync tab: per-Calendar sync URL, auth flow trigger, sync interval, conflict resolution policy (`remote-wins` default; `last-write-wins` power-user option)
- OAuth tab: optional per-provider client ID overrides (`microsoft_client_id`, `google_client_id`)

---

## Theme

Controls the visual appearance of the UI. Four options:

- **System** (default) — reads the OS light/dark preference and follows it automatically
- **Light** — explicit light mode
- **Dark** — explicit dark mode
- **Retro** — an 80s arcade aesthetic (phosphor greens, warm ambers, pixel-adjacent typography) designed to be distinctive without causing eye strain

Theme is selected in the Settings Panel and stored in `config.toml`. Implementation order: System first, Light/Dark/Retro added after.

---

## Bar Setup

The process of configuring installed bar apps to display Bar Output from PyCalendar. Runs automatically:

- **Windows**: targets `komorebi-bar` (configured via `komorebic --bar`). Bar Setup writes a widget entry into the komorebi-bar JSON config.
- **Linux**: an interactive terminal prompt detects installed bar apps (Waybar, Polybar, etc.) and writes the appropriate config snippets to their config directories (e.g. `~/.config/waybar/config`).

Goal: zero manual config editing required from the user.

---

## Bar Output

A compact, read-only representation of upcoming Events formatted for status bar apps. Supported formats: `text` (emoji pipe-separated), `json` (Waybar custom module), `polybar` (plain text). Produced by `--mode bar`.
