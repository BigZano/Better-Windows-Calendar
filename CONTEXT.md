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

The process of pulling Events from a remote CalDAV or Google Calendar into the local SQLite store, and pushing local changes back. Sync state (sync token, per-resource ETags) is persisted in `sync_state` so incremental syncs are possible.

**Milestone 2 (sync milestone).** The schema is ready; the sync engine is not. `.ics` file import and `webcal://` link handling are also scoped to this milestone.

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
