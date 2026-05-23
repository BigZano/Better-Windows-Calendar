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
	"title":            true,
	"start_ts":         true,
	"end_ts":           true,
	"timezone":         true,
	"notes":            true,
	"reminder_ts":      true,
	"recurrence_rule":  true,
	"all_day":          true,
	"calendar_id":      true,
	"location":         true,
	"url":              true,
	"parent_event_id":  true,
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
	CalendarID     sql.NullInt64
	Location       sql.NullString
	URL            sql.NullString
	ParentEventID  sql.NullInt64

	// Categories is populated on-demand by EnrichEventsWithCategories; nil if not loaded.
	Categories []Category
}

// StartTime returns the event start as a local time.Time.
func (e Event) StartTime() time.Time {
	return time.Unix(e.StartTS, 0)
}

func openDB() (*sql.DB, error) {
	return storage.Pool()
}

func scanEvent(row interface{ Scan(...any) error }) (Event, error) {
	var e Event
	var allDay int
	err := row.Scan(
		&e.ID, &e.Title, &e.StartTS, &e.EndTS, &e.Timezone,
		&e.Notes, &e.ReminderTS, &e.CreatedTS, &e.UpdatedTS,
		&e.RecurrenceRule, &allDay, &e.CalendarID, &e.Location, &e.URL,
		&e.ParentEventID,
	)
	if err != nil {
		return Event{}, err
	}
	e.AllDay = allDay != 0
	return e, nil
}

// CreateEvent inserts a new event and returns its ID.
// If reminderMinutes is nil the default from config (15 min) is used.
// calendarID <= 0 defaults to the built-in local calendar (id=1).
func CreateEvent(
	title string,
	startTime time.Time,
	endTime *time.Time,
	notes string,
	reminderMinutes *int,
	recurrenceRule string,
	allDay bool,
	tz string,
	calendarID int64,
	location string,
	url string,
) (int64, error) {
	db, err := openDB()
	if err != nil {
		return 0, err
	}


	if calendarID <= 0 {
		calendarID = 1
	}

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

	var locationVal sql.NullString
	if location != "" {
		locationVal = sql.NullString{String: location, Valid: true}
	}

	var urlVal sql.NullString
	if url != "" {
		urlVal = sql.NullString{String: url, Valid: true}
	}

	res, err := db.Exec(`
		INSERT INTO events
			(title, start_ts, end_ts, timezone, notes, reminder_ts, created_ts, updated_ts,
			 recurrence_rule, all_day, calendar_id, location, url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		title, startTS, endTS, tz, notesVal, reminderTS, now, now,
		rrule, boolToInt(allDay), calendarID, locationVal, urlVal,
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

// GetEvent returns a single event by ID.
func GetEvent(id int64) (Event, error) {
	db, err := openDB()
	if err != nil {
		return Event{}, err
	}


	row := db.QueryRow(`
		SELECT id, title, start_ts, end_ts, timezone, notes, reminder_ts,
		       created_ts, updated_ts, recurrence_rule, all_day,
		       calendar_id, location, url, parent_event_id
		FROM events WHERE id = ?`, id)
	return scanEvent(row)
}

// GetUpcoming returns the next limit events (including recurring occurrences) after now.
func GetUpcoming(limit int) ([]Event, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}


	now := time.Now()
	// Expand up to 1 year ahead to capture recurring occurrences.
	windowEnd := now.AddDate(1, 0, 0)

	rows, err := db.Query(`
		SELECT id, title, start_ts, end_ts, timezone, notes, reminder_ts,
		       created_ts, updated_ts, recurrence_rule, all_day,
		       calendar_id, location, url, parent_event_id
		FROM events
		WHERE start_ts >= ?
		ORDER BY start_ts ASC`, now.Unix())
	if err != nil {
		return nil, fmt.Errorf("get upcoming: %w", err)
	}
	defer rows.Close()

	var raw []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		raw = append(raw, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	earlyMasters := queryEarlyRecurringMasters(db, now.Unix())
	expanded := expandEvents(raw, earlyMasters, now, windowEnd)

	if len(expanded) > limit {
		expanded = expanded[:limit]
	}
	return expanded, nil
}

// GetEvents returns events (including recurring occurrences) within [startTS, endTS].
func GetEvents(startTS, endTS int64) ([]Event, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}


	rows, err := db.Query(`
		SELECT id, title, start_ts, end_ts, timezone, notes, reminder_ts,
		       created_ts, updated_ts, recurrence_rule, all_day,
		       calendar_id, location, url, parent_event_id
		FROM events WHERE start_ts >= ? AND start_ts <= ?
		ORDER BY start_ts ASC`, startTS, endTS)
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}
	defer rows.Close()

	var raw []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		raw = append(raw, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	earlyMasters := queryEarlyRecurringMasters(db, startTS)
	return expandEvents(raw, earlyMasters, time.Unix(startTS, 0), time.Unix(endTS, 0)), nil
}

// GetDueReminders returns events whose reminder_ts is within the next windowSeconds.
func GetDueReminders(windowSeconds int64) ([]Event, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}


	now := time.Now().Unix()
	rows, err := db.Query(`
		SELECT id, title, start_ts, end_ts, timezone, notes, reminder_ts,
		       created_ts, updated_ts, recurrence_rule, all_day,
		       calendar_id, location, url, parent_event_id
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


	_, err = db.Exec(`DELETE FROM events WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete event %d: %w", id, err)
	}
	slog.Info("deleted event", "id", id)
	return nil
}

// GetEventsByCalendar returns events for a specific calendar within [startTS, endTS],
// including recurring occurrences.
func GetEventsByCalendar(calendarID, startTS, endTS int64) ([]Event, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}


	rows, err := db.Query(`
		SELECT id, title, start_ts, end_ts, timezone, notes, reminder_ts,
		       created_ts, updated_ts, recurrence_rule, all_day,
		       calendar_id, location, url, parent_event_id
		FROM events
		WHERE calendar_id = ? AND start_ts >= ? AND start_ts <= ?
		ORDER BY start_ts ASC`, calendarID, startTS, endTS)
	if err != nil {
		return nil, fmt.Errorf("get events by calendar: %w", err)
	}
	defer rows.Close()

	var raw []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		raw = append(raw, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Early recurring masters for this calendar.
	earlyRows, err := db.Query(`
		SELECT id, title, start_ts, end_ts, timezone, notes, reminder_ts,
		       created_ts, updated_ts, recurrence_rule, all_day,
		       calendar_id, location, url, parent_event_id
		FROM events
		WHERE calendar_id = ? AND recurrence_rule IS NOT NULL
		  AND parent_event_id IS NULL AND start_ts < ?`, calendarID, startTS)
	if err != nil {
		return expandEvents(raw, nil, time.Unix(startTS, 0), time.Unix(endTS, 0)), nil
	}
	defer earlyRows.Close()

	var earlyMasters []Event
	for earlyRows.Next() {
		e, err := scanEvent(earlyRows)
		if err != nil {
			continue
		}
		earlyMasters = append(earlyMasters, e)
	}

	return expandEvents(raw, earlyMasters, time.Unix(startTS, 0), time.Unix(endTS, 0)), nil
}

// CountEventsForCalendar returns how many events belong to the given calendar.
func CountEventsForCalendar(calID int64) (int, error) {
	db, err := openDB()
	if err != nil {
		return 0, err
	}


	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE calendar_id = ?`, calID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count events for calendar: %w", err)
	}
	return count, nil
}

// ReassignCalendarEvents moves all events from one calendar to another.
func ReassignCalendarEvents(fromCalID, toCalID int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}


	if _, err := db.Exec(`UPDATE events SET calendar_id = ? WHERE calendar_id = ?`, toCalID, fromCalID); err != nil {
		return fmt.Errorf("reassign calendar events %d→%d: %w", fromCalID, toCalID, err)
	}
	return nil
}

// DeleteEventsByCalendar removes all events belonging to the given calendar.
func DeleteEventsByCalendar(calID int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}


	if _, err := db.Exec(`DELETE FROM events WHERE calendar_id = ?`, calID); err != nil {
		return fmt.Errorf("delete events for calendar %d: %w", calID, err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
