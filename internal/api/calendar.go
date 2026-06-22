package api

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Calendar source types, stored in the calendars.type column.
const (
	CalendarTypeLocal   = "local"
	CalendarTypeCalDAV  = "caldav"
	CalendarTypeGoogle  = "google"  // deferred (ADR-0003); designed for, not yet implemented
	CalendarTypeOutlook = "outlook" // Outlook Calendar via Microsoft Graph
)

var allowedCalendarUpdateFields = map[string]bool{
	"name":         true,
	"color":        true,
	"sync_enabled": true,
	"sync_url":     true,
}

// Calendar mirrors the calendars table schema.
type Calendar struct {
	ID            int64
	Name          string
	Color         string
	Type          string // one of CalendarType* (local | caldav | google | outlook)
	SyncURL       sql.NullString
	Username      sql.NullString
	CredentialKey sql.NullString
	SyncEnabled   bool
	LastSyncedAt  sql.NullInt64
	CreatedTS     int64
}

// CreateCalendar inserts a new calendar and returns its ID.
func CreateCalendar(name, color, calType string) (int64, error) {
	db, err := openDB()
	if err != nil {
		return 0, err
	}

	res, err := db.Exec(
		`INSERT INTO calendars (name, color, type, created_ts) VALUES (?, ?, ?, ?)`,
		name, color, calType, time.Now().Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("create calendar: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	slog.Info("created calendar", "id", id, "name", name)
	return id, nil
}

// GetCalendars returns all calendars ordered by ID.
func GetCalendars() ([]Calendar, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(`
		SELECT id, name, color, type, sync_url, username, credential_key,
		       sync_enabled, last_synced_at, created_ts
		FROM calendars ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("get calendars: %w", err)
	}
	defer rows.Close()

	var cals []Calendar
	for rows.Next() {
		c, err := scanCalendar(rows)
		if err != nil {
			return nil, err
		}
		cals = append(cals, c)
	}
	return cals, rows.Err()
}

// GetCalendar returns the calendar with the given ID.
func GetCalendar(id int64) (Calendar, error) {
	db, err := openDB()
	if err != nil {
		return Calendar{}, err
	}

	row := db.QueryRow(`
		SELECT id, name, color, type, sync_url, username, credential_key,
		       sync_enabled, last_synced_at, created_ts
		FROM calendars WHERE id = ?`, id)
	c, err := scanCalendar(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Calendar{}, ErrNotFound
		}
		return Calendar{}, fmt.Errorf("get calendar %d: %w", id, err)
	}
	return c, nil
}

// UpdateCalendar updates only the whitelisted fields for the given calendar ID.
func UpdateCalendar(id int64, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	for k := range fields {
		if !allowedCalendarUpdateFields[k] {
			return fmt.Errorf("update_calendar: unknown or disallowed field %q", k)
		}
	}

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

	_, err = db.Exec(`UPDATE calendars SET `+strings.Join(setClauses, ", ")+` WHERE id = ?`, args...)
	if err != nil {
		return fmt.Errorf("update calendar %d: %w", id, err)
	}
	slog.Info("updated calendar", "id", id)
	return nil
}

// SetCalendarLastSynced records the timestamp of the most recent successful
// sync on the calendars row. Kept off the UpdateCalendar allowlist because it
// is engine-internal, not a user-editable field.
func SetCalendarLastSynced(id int64, ts int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE calendars SET last_synced_at = ? WHERE id = ?`, ts, id); err != nil {
		return fmt.Errorf("set last_synced_at for calendar %d: %w", id, err)
	}
	return nil
}

// DeleteCalendar removes the calendar with the given ID.
func DeleteCalendar(id int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}

	_, err = db.Exec(`DELETE FROM calendars WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete calendar %d: %w", id, err)
	}
	slog.Info("deleted calendar", "id", id)
	return nil
}

func scanCalendar(row interface{ Scan(...any) error }) (Calendar, error) {
	var c Calendar
	var syncEnabled int
	err := row.Scan(
		&c.ID, &c.Name, &c.Color, &c.Type,
		&c.SyncURL, &c.Username, &c.CredentialKey,
		&syncEnabled, &c.LastSyncedAt, &c.CreatedTS,
	)
	if err != nil {
		return Calendar{}, err
	}
	c.SyncEnabled = syncEnabled != 0
	return c, nil
}
