package api

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"pycalendar/internal/storage"
)

// ErrNotFound is returned by lookup functions when the requested record does not exist.
var ErrNotFound = errors.New("not found")

// GetEventByResourceURL returns the event whose resource_url matches resourceURL.
// Returns ErrNotFound if no such event exists.
func GetEventByResourceURL(resourceURL string) (Event, error) {
	db, err := storage.Pool()
	if err != nil {
		return Event{}, err
	}

	row := db.QueryRow(`
		SELECT id, title, start_ts, end_ts, timezone, notes, reminder_ts,
		       created_ts, updated_ts, recurrence_rule, all_day,
		       calendar_id, location, url, parent_event_id, resource_url
		FROM events WHERE resource_url = ?`, resourceURL)

	e, err := scanEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	return e, err
}

// CreateEventFromRemote inserts an event sourced from a remote calendar adapter.
// resource_url is set for future sync lookups; the local reminder default (15 min) is used.
func CreateEventFromRemote(e Event, calendarID int64, resourceURL string) (int64, error) {
	db, err := storage.Pool()
	if err != nil {
		return 0, err
	}

	if calendarID <= 0 {
		calendarID = 1
	}
	now := time.Now().Unix()

	res, err := db.Exec(`
		INSERT INTO events
			(title, start_ts, end_ts, timezone, notes, reminder_ts, created_ts, updated_ts,
			 recurrence_rule, all_day, calendar_id, location, url, resource_url)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Title, e.StartTS, e.EndTS, e.Timezone, e.Notes, e.ReminderTS, now, now,
		e.RecurrenceRule, boolToInt(e.AllDay), calendarID, e.Location, e.URL, resourceURL,
	)
	if err != nil {
		return 0, fmt.Errorf("create event from remote: %w", err)
	}
	return res.LastInsertId()
}

// SetEventResourceURL links a local event to its remote resource after a push,
// so the next fetch recognises it instead of re-importing a duplicate.
func SetEventResourceURL(id int64, resourceURL string) error {
	db, err := storage.Pool()
	if err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE events SET resource_url = ? WHERE id = ?`, resourceURL, id); err != nil {
		return fmt.Errorf("set resource_url for event %d: %w", id, err)
	}
	return nil
}

// DeleteEventFromRemote deletes a local event without enqueuing an outbox
// delete. Used when applying a remote-originated deletion, so it is not echoed
// back to the server.
func DeleteEventFromRemote(id int64) error {
	db, err := storage.Pool()
	if err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM events WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete event from remote %d: %w", id, err)
	}
	return nil
}

// UpdateEventFromRemote overwrites a local event's fields with data from the
// winning remote version. updated_ts is refreshed; resource_url is preserved.
func UpdateEventFromRemote(id int64, e Event) error {
	db, err := storage.Pool()
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		UPDATE events SET
			title           = ?,
			start_ts        = ?,
			end_ts          = ?,
			timezone        = ?,
			notes           = ?,
			reminder_ts     = ?,
			recurrence_rule = ?,
			all_day         = ?,
			location        = ?,
			url             = ?,
			updated_ts      = ?
		WHERE id = ?`,
		e.Title, e.StartTS, e.EndTS, e.Timezone, e.Notes, e.ReminderTS,
		e.RecurrenceRule, boolToInt(e.AllDay), e.Location, e.URL,
		time.Now().Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("update event from remote %d: %w", id, err)
	}
	return nil
}
