package api

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"pycalendar/internal/storage"
)

// allowedUpdateFields is the whitelist of column names permitted in UpdateEvent.
// This prevents SQL column injection via dynamic field names.
var allowedUpdateFields = map[string]bool{
	"title":           true,
	"start_ts":        true,
	"end_ts":          true,
	"timezone":        true,
	"notes":           true,
	"reminder_ts":     true,
	"recurrence_rule": true,
	"all_day":         true,
}

// Event mirrors the events table schema.
type Event struct {
	ID             int64
	Title          string
	StartTS        int64
	EndTS          sql.NullInt64
	Timezone       string
	Notes          sql.NullString
	ReminderTS     sql.NullInt64
	CreatedTS      int64
	UpdatedTS      int64
	RecurrenceRule sql.NullString
	AllDay         bool
}

// StartTime returns the event start as a local time.Time.
func (e Event) StartTime() time.Time {
	return time.Unix(e.StartTS, 0)
}

func openDB() (*sql.DB, error) {
	return storage.Open(5)
}

func scanEvent(row interface{ Scan(...any) error }) (Event, error) {
	var e Event
	var allDay int
	err := row.Scan(
		&e.ID, &e.Title, &e.StartTS, &e.EndTS, &e.Timezone,
		&e.Notes, &e.ReminderTS, &e.CreatedTS, &e.UpdatedTS,
		&e.RecurrenceRule, &allDay,
	)
	if err != nil {
		return Event{}, err
	}
	e.AllDay = allDay != 0
	return e, nil
}

// CreateEvent inserts a new event and returns its ID.
// If reminderMinutes is nil the default from config (15 min) is used.
func CreateEvent(
	title string,
	startTime time.Time,
	endTime *time.Time,
	notes string,
	reminderMinutes *int,
	recurrenceRule string,
	allDay bool,
	tz string,
) (int64, error) {
	db, err := openDB()
	if err != nil {
		return 0, err
	}
	defer db.Close()

	now := time.Now().Unix()
	startTS := startTime.Unix()

	var endTS sql.NullInt64
	if endTime != nil {
		endTS = sql.NullInt64{Int64: endTime.Unix(), Valid: true}
	}

	defMinutes := 15
	if reminderMinutes == nil {
		reminderMinutes = &defMinutes
	}
	reminderTS := sql.NullInt64{
		Int64: startTS - int64(*reminderMinutes*60),
		Valid: true,
	}

	var rrule sql.NullString
	if recurrenceRule != "" {
		rrule = sql.NullString{String: recurrenceRule, Valid: true}
	}

	var notesVal sql.NullString
	if notes != "" {
		notesVal = sql.NullString{String: notes, Valid: true}
	}

	res, err := db.Exec(`
		INSERT INTO events
			(title, start_ts, end_ts, timezone, notes, reminder_ts, created_ts, updated_ts, recurrence_rule, all_day)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		title, startTS, endTS, tz, notesVal, reminderTS, now, now, rrule, boolToInt(allDay),
	)
	if err != nil {
		return 0, fmt.Errorf("create event: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	slog.Info("created event", "id", id, "title", title)
	return id, nil
}

// GetUpcoming returns the next limit events whose start_ts is in the future.
func GetUpcoming(limit int) ([]Event, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT id, title, start_ts, end_ts, timezone, notes, reminder_ts,
		       created_ts, updated_ts, recurrence_rule, all_day
		FROM events
		WHERE start_ts >= ?
		ORDER BY start_ts ASC
		LIMIT ?`, time.Now().Unix(), limit)
	if err != nil {
		return nil, fmt.Errorf("get upcoming: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// GetEvents returns events whose start_ts falls within [startTS, endTS].
func GetEvents(startTS, endTS int64) ([]Event, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT id, title, start_ts, end_ts, timezone, notes, reminder_ts,
		       created_ts, updated_ts, recurrence_rule, all_day
		FROM events WHERE start_ts >= ? AND start_ts <= ?
		ORDER BY start_ts ASC`, startTS, endTS)
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// GetDueReminders returns events whose reminder_ts is within the next windowSeconds.
func GetDueReminders(windowSeconds int64) ([]Event, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	now := time.Now().Unix()
	rows, err := db.Query(`
		SELECT id, title, start_ts, end_ts, timezone, notes, reminder_ts,
		       created_ts, updated_ts, recurrence_rule, all_day
		FROM events
		WHERE reminder_ts IS NOT NULL
		  AND reminder_ts >= ?
		  AND reminder_ts <= ?
		ORDER BY reminder_ts ASC`, now, now+windowSeconds)
	if err != nil {
		return nil, fmt.Errorf("get due reminders: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// UpdateEvent updates only the whitelisted fields in fields for the given event ID.
// Unknown field names are rejected to prevent SQL column injection.
func UpdateEvent(id int64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	for k := range fields {
		if !allowedUpdateFields[k] {
			return fmt.Errorf("update_event: unknown or disallowed field %q", k)
		}
	}

	fields["updated_ts"] = time.Now().Unix()

	setClauses := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields)+1)
	for k, v := range fields {
		setClauses = append(setClauses, k+" = ?")
		args = append(args, v)
	}
	args = append(args, id)

	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`UPDATE events SET `+strings.Join(setClauses, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return fmt.Errorf("update event %d: %w", id, err)
	}
	slog.Info("updated event", "id", id)
	return nil
}

// DeleteEvent removes the event with the given ID.
func DeleteEvent(id int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`DELETE FROM events WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete event %d: %w", id, err)
	}
	slog.Info("deleted event", "id", id)
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
