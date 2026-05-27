package syncer

import (
	"encoding/json"
	"fmt"
	"time"

	"pycalendar/internal/storage"
)

// SyncState holds the per-calendar sync state persisted in the sync_state table.
// Use GetETag / SetETag to access the ETag map — the JSON serialisation of the
// underlying map is an implementation detail not exposed to callers.
type SyncState struct {
	CalendarID int64
	SyncToken  string
	LastSyncAt time.Time

	etags map[string]string
}

// GetETag returns the stored ETag for resourceURL, or "" if not found.
func (s *SyncState) GetETag(resourceURL string) string {
	return s.etags[resourceURL]
}

// SetETag stores etag for resourceURL.
func (s *SyncState) SetETag(resourceURL, etag string) {
	if s.etags == nil {
		s.etags = make(map[string]string)
	}
	s.etags[resourceURL] = etag
}

// LoadSyncState loads (or creates) the SyncState for calendarID.
// Returns an empty state if no row exists yet — never returns an error for missing rows.
func LoadSyncState(calendarID int64) (*SyncState, error) {
	db, err := storage.Pool()
	if err != nil {
		return nil, err
	}

	var syncToken, etagJSON string
	var lastSyncTS int64
	err = db.QueryRow(
		`SELECT COALESCE(sync_token,''), COALESCE(etag_map,'{}'), COALESCE(last_sync_at,0)
		   FROM sync_state WHERE calendar_id = ?`, calendarID,
	).Scan(&syncToken, &etagJSON, &lastSyncTS)

	if err != nil {
		// Row does not exist yet — return empty state.
		return &SyncState{CalendarID: calendarID, etags: make(map[string]string)}, nil
	}

	etags := make(map[string]string)
	if jerr := json.Unmarshal([]byte(etagJSON), &etags); jerr != nil {
		etags = make(map[string]string)
	}

	var lastSync time.Time
	if lastSyncTS > 0 {
		lastSync = time.Unix(lastSyncTS, 0)
	}

	return &SyncState{
		CalendarID: calendarID,
		SyncToken:  syncToken,
		LastSyncAt: lastSync,
		etags:      etags,
	}, nil
}

// Save upserts the SyncState into the sync_state table.
func (s *SyncState) Save() error {
	db, err := storage.Pool()
	if err != nil {
		return err
	}

	b, err := json.Marshal(s.etags)
	if err != nil {
		return fmt.Errorf("sync state: marshal etags: %w", err)
	}

	var lastSyncTS int64
	if !s.LastSyncAt.IsZero() {
		lastSyncTS = s.LastSyncAt.Unix()
	}

	_, err = db.Exec(
		`INSERT INTO sync_state (calendar_id, sync_token, etag_map, last_sync_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(calendar_id) DO UPDATE SET
		   sync_token   = excluded.sync_token,
		   etag_map     = excluded.etag_map,
		   last_sync_at = excluded.last_sync_at`,
		s.CalendarID, s.SyncToken, string(b), lastSyncTS,
	)
	return err
}
