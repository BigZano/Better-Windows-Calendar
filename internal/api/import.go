package api

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

// ImportedEvent carries the fields needed to insert an event that originated
// from an external source (a .ics file). Unlike user-created events it has a
// real timezone, an optional iCal UID for deduplication, and never gets an
// automatic reminder.
type ImportedEvent struct {
	UID            string
	Title          string
	Start          time.Time
	End            *time.Time
	Timezone       string // a real IANA tz string (e.g. "UTC"), never "import"
	Notes          string
	RecurrenceRule string
	AllDay         bool
	CalendarID     int64
	Location       string
	URL            string

	// ReminderMinutes is set only when the source event explicitly carries a
	// reminder offset (e.g. a parseable VALARM trigger). nil leaves
	// reminder_ts NULL — imported events do not get a default reminder.
	ReminderMinutes *int
}

// CreateImportedEvent inserts an event imported from an external source and
// returns its ID. It does NOT reuse CreateEvent: imported events carry a UID,
// use a real timezone, and only get a reminder when one is explicitly provided.
func CreateImportedEvent(ev ImportedEvent) (int64, error) {
	db, err := openDB()
	if err != nil {
		return 0, err
	}

	calendarID := ev.CalendarID
	if calendarID <= 0 {
		calendarID = 1
	}

	now := time.Now().Unix()
	startTS := ev.Start.Unix()

	var endTS sql.NullInt64
	if ev.End != nil {
		endTS = sql.NullInt64{Int64: ev.End.Unix(), Valid: true}
	}

	var reminderTS sql.NullInt64
	if ev.ReminderMinutes != nil {
		reminderTS = sql.NullInt64{Int64: startTS - int64(*ev.ReminderMinutes*60), Valid: true}
	}

	tz := ev.Timezone
	if tz == "" {
		tz = "UTC"
	}

	res, err := db.Exec(`
		INSERT INTO events
			(uid, title, start_ts, end_ts, timezone, notes, reminder_ts, created_ts, updated_ts,
			 recurrence_rule, all_day, calendar_id, location, url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableString(ev.UID), ev.Title, startTS, endTS, tz, nullableString(ev.Notes),
		reminderTS, now, now, nullableString(ev.RecurrenceRule), boolToInt(ev.AllDay),
		calendarID, nullableString(ev.Location), nullableString(ev.URL),
	)
	if err != nil {
		return 0, fmt.Errorf("create imported event: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	slog.Info("created imported event", "id", id, "title", ev.Title)
	return id, nil
}

// FindDuplicateEvent reports whether an event matching the given import identity
// already exists in the target calendar. Identity is the iCal UID when uid is
// non-empty, else the (title, start_ts) pair. The match is always scoped to
// calendarID, so the same event in a different calendar is not a duplicate.
func FindDuplicateEvent(calendarID int64, uid string, title string, startTS int64) (bool, error) {
	db, err := openDB()
	if err != nil {
		return false, err
	}

	var query string
	var args []any
	if uid != "" {
		query = `SELECT 1 FROM events WHERE calendar_id = ? AND uid = ? LIMIT 1`
		args = []any{calendarID, uid}
	} else {
		query = `SELECT 1 FROM events WHERE calendar_id = ? AND title = ? AND start_ts = ? LIMIT 1`
		args = []any{calendarID, title, startTS}
	}

	var one int
	err = db.QueryRow(query, args...).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("find duplicate event: %w", err)
	}
	return true, nil
}

// nullableString returns nil for empty strings so they store as SQL NULL,
// otherwise the string itself.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
