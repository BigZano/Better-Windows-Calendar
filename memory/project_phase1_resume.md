---
name: project-phase1-resume
description: Phase 1 implementation — COMPLETE
metadata:
  type: project
---

Phase 1 goal: schema migrations, OS keychain w/ clean uninstall, window-close-to-tray, sound alerts.

**All steps complete as of 2026-05-14.**

**What was built:**
1. `internal/storage/storage.go` — migration framework, v1 (original schema), v2 (calendars, categories, event_categories, attendees, attachments, sync_state, credential_index tables; ALTER events for calendar_id/location/url; foreign_keys PRAGMA)
2. `internal/keychain/keychain.go` — Set/Get/Delete/DeleteAll backed by credential_index table; uses github.com/zalando/go-keyring
3. `internal/api/calendar.go` — Calendar struct + CreateCalendar/GetCalendars/DeleteCalendar CRUD
4. `internal/api/api.go` — Event struct extended with CalendarID/Location/URL; scanEvent and all three SELECT queries updated; CreateEvent signature extended with calendarID/location/url; allowedUpdateFields extended
5. `internal/notifier/notifier.go` — Notify now takes `playSound bool`; plays beeep.Beep(2000,200) when true
6. `internal/daemon/daemon.go` — passes cfg.Notifications.SoundEnabled to notifier.Notify
7. `ui/window.go` — SetCloseIntercept(Hide) added to ShowCalendarWindow; CreateEvent callers updated
8. `main.go` — --purge flag added; "uninstall" mode: DeleteAll keychain, disable autostart, optionally delete DB+config files

**Why:** uninstall reads credential_index BEFORE deleting DB so no keychain orphans remain.
