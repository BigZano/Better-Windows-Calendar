package api

import (
	"fmt"
	"time"
)

// Outbox operation kinds.
const (
	OutboxUpsert = "upsert"
	OutboxDelete = "delete"
)

// OutboxEntry is a queued local change awaiting push to a remote calendar.
type OutboxEntry struct {
	ID          int64
	CalendarID  int64
	EventID     int64
	Op          string
	ResourceURL string
	QueuedTS    int64
}

// enqueueOutbox appends a pending push operation for a synced calendar.
func enqueueOutbox(calendarID, eventID int64, op, resourceURL string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	if _, err := db.Exec(
		`INSERT INTO sync_outbox (calendar_id, event_id, op, resource_url, queued_ts)
		 VALUES (?, ?, ?, ?, ?)`,
		calendarID, eventID, op, resourceURL, time.Now().Unix(),
	); err != nil {
		return fmt.Errorf("enqueue outbox: %w", err)
	}
	return nil
}

// PendingOutbox returns queued operations for calendarID, oldest first so they
// are pushed in the order they happened.
func PendingOutbox(calendarID int64) ([]OutboxEntry, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(
		`SELECT id, calendar_id, COALESCE(event_id, 0), op, COALESCE(resource_url, ''), queued_ts
		   FROM sync_outbox
		  WHERE calendar_id = ?
		  ORDER BY id ASC`, calendarID,
	)
	if err != nil {
		return nil, fmt.Errorf("pending outbox: %w", err)
	}
	defer rows.Close()

	var out []OutboxEntry
	for rows.Next() {
		var e OutboxEntry
		if err := rows.Scan(&e.ID, &e.CalendarID, &e.EventID, &e.Op, &e.ResourceURL, &e.QueuedTS); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteOutboxEntry removes a queued operation once it has been pushed.
func DeleteOutboxEntry(id int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM sync_outbox WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete outbox entry %d: %w", id, err)
	}
	return nil
}

// isSyncedCalendar reports whether a calendar pushes its local changes to a
// remote — i.e. it is not the local-only type. Lookup failures yield false so a
// transient error never enqueues junk.
func isSyncedCalendar(calendarID int64) bool {
	if calendarID <= 0 {
		return false
	}
	cal, err := GetCalendar(calendarID)
	if err != nil {
		return false
	}
	return cal.Type != CalendarTypeLocal
}
