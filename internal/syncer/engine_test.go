package syncer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"pycalendar/internal/api"
	"pycalendar/internal/syncer"
	"pycalendar/internal/testutil"
)

// mockAdapter implements syncer.Adapter for engine tests.
type mockAdapter struct {
	changes  []syncer.RemoteChange
	pushErr  error
	fetchErr error
	pushed   []api.Event
	deleted  []string
}

func (m *mockAdapter) FetchChanges(_ context.Context, _ *syncer.SyncState) ([]syncer.RemoteChange, error) {
	return m.changes, m.fetchErr
}

func (m *mockAdapter) PushChange(_ context.Context, _ *syncer.SyncState, e api.Event) error {
	m.pushed = append(m.pushed, e)
	return m.pushErr
}

func (m *mockAdapter) DeleteRemote(_ context.Context, _ *syncer.SyncState, resourceURL string) error {
	m.deleted = append(m.deleted, resourceURL)
	return nil
}

func TestEngine_Sync_NoChanges_UpdatesStatus(t *testing.T) {
	testutil.NewTestDB(t)

	eng := syncer.New(5 * time.Minute)
	eng.RegisterAdapter(1, &mockAdapter{})

	if err := eng.Sync(context.Background(), 1); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	status := eng.Status(1)
	if status.LastSyncAt.IsZero() {
		t.Error("LastSyncAt should be set after sync")
	}
	if status.LastError != nil {
		t.Errorf("unexpected error in status: %v", status.LastError)
	}
}

func TestEngine_Sync_UpsertChange_CreatesLocalEvent(t *testing.T) {
	testutil.NewTestDB(t)

	remoteEvent := api.Event{
		Title:   "Remote Meeting",
		StartTS: time.Now().Unix(),
	}
	adapter := &mockAdapter{
		changes: []syncer.RemoteChange{
			{
				ResourceURL: "https://example.com/cal/meeting.ics",
				ETag:        `"etag-1"`,
				Type:        syncer.ChangeUpsert,
				Event:       &remoteEvent,
			},
		},
	}

	eng := syncer.New(5 * time.Minute)
	eng.RegisterAdapter(1, adapter)

	if err := eng.Sync(context.Background(), 1); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Verify event was created locally.
	ev, err := api.GetEventByResourceURL("https://example.com/cal/meeting.ics")
	if err != nil {
		t.Fatalf("GetEventByResourceURL: %v", err)
	}
	if ev.Title != "Remote Meeting" {
		t.Errorf("title: got %q, want Remote Meeting", ev.Title)
	}
}

func TestEngine_Sync_DeleteChange_RemovesLocalEvent(t *testing.T) {
	db := testutil.NewTestDB(t)

	// Seed a local event with resource_url so we have something to delete.
	var eventID int64
	row := db.QueryRow(`
		INSERT INTO events (title, start_ts, updated_ts, created_ts, calendar_id, resource_url)
		VALUES ('ToDelete', ?, ?, ?, 1, 'https://example.com/cal/delete-me.ics')
		RETURNING id`,
		time.Now().Unix(), time.Now().Unix(), time.Now().Unix(),
	)
	if err := row.Scan(&eventID); err != nil {
		t.Fatalf("seed event: %v", err)
	}

	adapter := &mockAdapter{
		changes: []syncer.RemoteChange{
			{
				ResourceURL: "https://example.com/cal/delete-me.ics",
				Type:        syncer.ChangeDelete,
			},
		},
	}

	eng := syncer.New(5 * time.Minute)
	eng.RegisterAdapter(1, adapter)

	if err := eng.Sync(context.Background(), 1); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	_, err := api.GetEventByResourceURL("https://example.com/cal/delete-me.ics")
	if !errors.Is(err, api.ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestEngine_Sync_UnknownCalendar_ReturnsError(t *testing.T) {
	testutil.NewTestDB(t)

	eng := syncer.New(5 * time.Minute)
	err := eng.Sync(context.Background(), 99)
	if err == nil {
		t.Error("expected error for unregistered calendar")
	}
}

func TestEngine_SyncAll_ContinuesAfterError(t *testing.T) {
	testutil.NewTestDB(t)

	failing := &mockAdapter{fetchErr: errors.New("server unavailable")}
	ok := &mockAdapter{}

	eng := syncer.New(5 * time.Minute)
	eng.RegisterAdapter(1, failing)
	eng.RegisterAdapter(2, ok)

	err := eng.SyncAll(context.Background())
	if err == nil {
		t.Error("expected error from failing adapter")
	}

	// Calendar 2 should still have been synced.
	status := eng.Status(2)
	if status.LastSyncAt.IsZero() {
		t.Error("calendar 2 should have synced even though calendar 1 failed")
	}
}

func TestEngine_Status_ReflectsInProgress(t *testing.T) {
	testutil.NewTestDB(t)

	eng := syncer.New(5 * time.Minute)
	eng.RegisterAdapter(1, &mockAdapter{})

	// Before any sync the status is zero.
	status := eng.Status(1)
	if status.InProgress {
		t.Error("InProgress should be false before first sync")
	}
}
