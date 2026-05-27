package syncer

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"pycalendar/internal/api"
	"pycalendar/internal/storage"
)

const conflictRetentionDays = 30

// Conflict records a sync conflict: both the local and remote versions of an
// event changed since the last sync. The default resolution is remote-wins
// (applied automatically if the user takes no action), but the local version
// is preserved in LocalJSON for 30 days so the decision can be reversed.
type Conflict struct {
	ID         int64
	CalendarID int64
	EventID    int64
	LocalJSON  string
	RemoteJSON string
	DetectedAt time.Time
	ResolvedAt *time.Time
	Resolution string
}

// RecordConflict writes a new conflict row for the given local/remote event pair.
func RecordConflict(calendarID int64, local, remote api.Event) error {
	db, err := storage.Pool()
	if err != nil {
		return err
	}

	localJSON, err := json.Marshal(local)
	if err != nil {
		return fmt.Errorf("conflict: marshal local: %w", err)
	}
	remoteJSON, err := json.Marshal(remote)
	if err != nil {
		return fmt.Errorf("conflict: marshal remote: %w", err)
	}

	_, err = db.Exec(
		`INSERT INTO conflicts (calendar_id, event_id, local_json, remote_json, detected_at)
		 VALUES (?, ?, ?, ?, ?)`,
		calendarID, local.ID, string(localJSON), string(remoteJSON), time.Now().Unix(),
	)
	return err
}

// GetPendingConflicts returns unresolved conflicts for calendarID, newest first.
func GetPendingConflicts(calendarID int64) ([]Conflict, error) {
	db, err := storage.Pool()
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(
		`SELECT id, calendar_id, event_id, local_json, remote_json,
		        detected_at, resolved_at, COALESCE(resolution,'')
		   FROM conflicts
		  WHERE calendar_id = ? AND resolved_at IS NULL
		  ORDER BY detected_at DESC`, calendarID,
	)
	if err != nil {
		return nil, fmt.Errorf("conflict: query pending: %w", err)
	}
	defer rows.Close()
	return scanConflicts(rows)
}

// ResolveConflict marks a conflict as resolved.
// resolution should be "remote-wins" or "keep-local".
func ResolveConflict(id int64, resolution string) error {
	db, err := storage.Pool()
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`UPDATE conflicts SET resolved_at = ?, resolution = ? WHERE id = ?`,
		time.Now().Unix(), resolution, id,
	)
	return err
}

// PruneStaleConflicts deletes resolved conflicts older than 30 days.
// Should be called on tray startup.
func PruneStaleConflicts() error {
	db, err := storage.Pool()
	if err != nil {
		return err
	}
	cutoff := time.Now().AddDate(0, 0, -conflictRetentionDays).Unix()
	_, err = db.Exec(
		`DELETE FROM conflicts WHERE resolved_at IS NOT NULL AND resolved_at < ?`, cutoff,
	)
	return err
}

func scanConflicts(rows *sql.Rows) ([]Conflict, error) {
	var out []Conflict
	for rows.Next() {
		var c Conflict
		var detectedTS int64
		var resolvedTS sql.NullInt64
		if err := rows.Scan(
			&c.ID, &c.CalendarID, &c.EventID,
			&c.LocalJSON, &c.RemoteJSON,
			&detectedTS, &resolvedTS, &c.Resolution,
		); err != nil {
			return nil, err
		}
		c.DetectedAt = time.Unix(detectedTS, 0)
		if resolvedTS.Valid && resolvedTS.Int64 > 0 {
			t := time.Unix(resolvedTS.Int64, 0)
			c.ResolvedAt = &t
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
