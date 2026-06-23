package syncer_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pycalendar/internal/api"
	"pycalendar/internal/syncer"
	"pycalendar/internal/testutil"
)

// mockAdapter implements syncer.Adapter for engine tests.
type mockAdapter struct {
	changes    []syncer.RemoteChange
	pushErr    error
	fetchErr   error
	pushResult syncer.PushResult
	pushed     []api.Event
	deleted    []string
}

func (m *mockAdapter) FetchChanges(_ context.Context, _ *syncer.SyncState) ([]syncer.RemoteChange, error) {
	return m.changes, m.fetchErr
}

func (m *mockAdapter) PushChange(_ context.Context, _ *syncer.SyncState, e api.Event) (syncer.PushResult, error) {
	m.pushed = append(m.pushed, e)
	return m.pushResult, m.pushErr
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

func TestEngine_Sync_PushesOutboxUpsert_AndLinksResource(t *testing.T) {
	testutil.NewTestDB(t)

	calID, err := api.CreateCalendar("Work", "#3B82F6", api.CalendarTypeCalDAV)
	if err != nil {
		t.Fatalf("CreateCalendar: %v", err)
	}
	// Creating an event on a synced calendar enqueues an upsert.
	evID, err := api.CreateEvent("Pushed", time.Now(), nil, "", nil, "", false, "UTC", calID, "", "")
	if err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	adapter := &mockAdapter{pushResult: syncer.PushResult{ResourceURL: "https://remote/ev.ics", ETag: "etag-x"}}
	eng := syncer.New(time.Minute)
	eng.RegisterAdapter(calID, adapter)

	if err := eng.Sync(context.Background(), calID); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if len(adapter.pushed) != 1 {
		t.Fatalf("adapter received %d pushes, want 1", len(adapter.pushed))
	}
	if pend, _ := api.PendingOutbox(calID); len(pend) != 0 {
		t.Errorf("outbox has %d entries after push, want 0", len(pend))
	}
	ev, err := api.GetEvent(evID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if ev.ResourceURL.String != "https://remote/ev.ics" {
		t.Errorf("event resource_url = %q, want the pushed URL (so the next fetch won't duplicate it)", ev.ResourceURL.String)
	}
}

func TestEngine_Sync_PushesOutboxDelete(t *testing.T) {
	db := testutil.NewTestDB(t)

	calID, err := api.CreateCalendar("Work", "#3B82F6", api.CalendarTypeCalDAV)
	if err != nil {
		t.Fatalf("CreateCalendar: %v", err)
	}
	// Seed an already-synced event (has resource_url) directly, then delete it.
	var evID int64
	row := db.QueryRow(`
		INSERT INTO events (title, start_ts, updated_ts, created_ts, calendar_id, resource_url)
		VALUES ('ToDelete', ?, ?, ?, ?, 'https://remote/del.ics')
		RETURNING id`,
		time.Now().Unix(), time.Now().Unix(), time.Now().Unix(), calID,
	)
	if err := row.Scan(&evID); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	if err := api.DeleteEvent(evID); err != nil { // enqueues a delete
		t.Fatalf("DeleteEvent: %v", err)
	}

	adapter := &mockAdapter{}
	eng := syncer.New(time.Minute)
	eng.RegisterAdapter(calID, adapter)

	if err := eng.Sync(context.Background(), calID); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if len(adapter.deleted) != 1 || adapter.deleted[0] != "https://remote/del.ics" {
		t.Errorf("adapter.deleted = %v, want [https://remote/del.ics]", adapter.deleted)
	}
	if pend, _ := api.PendingOutbox(calID); len(pend) != 0 {
		t.Errorf("outbox has %d entries after delete push, want 0", len(pend))
	}
}

func TestEngine_RegisterUnregisterAdapter(t *testing.T) {
	testutil.NewTestDB(t)

	eng := syncer.New(time.Minute)
	if eng.HasAdapter(1) {
		t.Fatal("HasAdapter should be false before registration")
	}
	eng.RegisterAdapter(1, &mockAdapter{})
	if !eng.HasAdapter(1) {
		t.Fatal("HasAdapter should be true after registration")
	}
	eng.UnregisterAdapter(1)
	if eng.HasAdapter(1) {
		t.Fatal("HasAdapter should be false after unregistration")
	}
	if err := eng.Sync(context.Background(), 1); err == nil {
		t.Error("expected error syncing an unregistered calendar")
	}
}

// blockingAdapter blocks inside FetchChanges until released, and records the
// peak number of concurrent FetchChanges calls.
type blockingAdapter struct {
	active        int32
	maxConcurrent int32
	enter         chan struct{}
	release       chan struct{}
}

func (b *blockingAdapter) FetchChanges(_ context.Context, _ *syncer.SyncState) ([]syncer.RemoteChange, error) {
	n := atomic.AddInt32(&b.active, 1)
	for {
		old := atomic.LoadInt32(&b.maxConcurrent)
		if n <= old || atomic.CompareAndSwapInt32(&b.maxConcurrent, old, n) {
			break
		}
	}
	b.enter <- struct{}{}
	<-b.release
	atomic.AddInt32(&b.active, -1)
	return nil, nil
}

func (b *blockingAdapter) PushChange(_ context.Context, _ *syncer.SyncState, _ api.Event) (syncer.PushResult, error) {
	return syncer.PushResult{}, nil
}
func (b *blockingAdapter) DeleteRemote(_ context.Context, _ *syncer.SyncState, _ string) error {
	return nil
}

// TestEngine_Sync_SerializesSameCalendar verifies that two concurrent syncs of
// the same calendar never overlap (which would race on insert in applyChange).
// Run with -race for full effect.
func TestEngine_Sync_SerializesSameCalendar(t *testing.T) {
	testutil.NewTestDB(t)

	b := &blockingAdapter{enter: make(chan struct{}), release: make(chan struct{})}
	eng := syncer.New(time.Minute)
	eng.RegisterAdapter(1, b)

	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			_ = eng.Sync(context.Background(), 1)
		}()
	}

	<-b.enter // first sync is inside FetchChanges, holding the per-calendar lock
	select {
	case <-b.enter:
		t.Fatal("second sync entered FetchChanges before the first released — not serialized")
	case <-time.After(100 * time.Millisecond):
	}
	b.release <- struct{}{} // release first
	<-b.enter               // second now proceeds
	b.release <- struct{}{} // release second

	wg.Wait()
	if got := atomic.LoadInt32(&b.maxConcurrent); got != 1 {
		t.Errorf("peak concurrent FetchChanges = %d, want 1", got)
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
