package syncer_test

import (
	"testing"
	"time"

	"pycalendar/internal/syncer"
	"pycalendar/internal/testutil"
)

func TestLoadSyncState_NewCalendar_ReturnsEmpty(t *testing.T) {
	testutil.NewTestDB(t)

	state, err := syncer.LoadSyncState(99)
	if err != nil {
		t.Fatalf("LoadSyncState: %v", err)
	}
	if state.CalendarID != 99 {
		t.Errorf("CalendarID: got %d, want 99", state.CalendarID)
	}
	if state.SyncToken != "" {
		t.Errorf("SyncToken: got %q, want empty", state.SyncToken)
	}
	if !state.LastSyncAt.IsZero() {
		t.Errorf("LastSyncAt: got %v, want zero", state.LastSyncAt)
	}
}

func TestSyncState_ETagRoundTrip(t *testing.T) {
	testutil.NewTestDB(t)

	state, _ := syncer.LoadSyncState(1)
	state.SetETag("https://example.com/event1.ics", `"abc123"`)
	state.SetETag("https://example.com/event2.ics", `"def456"`)

	if got := state.GetETag("https://example.com/event1.ics"); got != `"abc123"` {
		t.Errorf("etag1: got %q, want %q", got, `"abc123"`)
	}
	if got := state.GetETag("https://example.com/event2.ics"); got != `"def456"` {
		t.Errorf("etag2: got %q", got)
	}
	if got := state.GetETag("https://example.com/missing.ics"); got != "" {
		t.Errorf("missing etag: got %q, want empty", got)
	}
}

func TestSyncState_SaveAndLoad(t *testing.T) {
	testutil.NewTestDB(t)

	state, _ := syncer.LoadSyncState(2)
	state.SyncToken = "https://graph.microsoft.com/v1.0/me/calendars/AAA/events/delta?$deltaToken=xyz"
	state.LastSyncAt = time.Now().Truncate(time.Second)
	state.SetETag("https://caldav.example.com/cal/1.ics", `"etag-v1"`)

	if err := state.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := syncer.LoadSyncState(2)
	if err != nil {
		t.Fatalf("LoadSyncState after save: %v", err)
	}

	if loaded.SyncToken != state.SyncToken {
		t.Errorf("SyncToken: got %q, want %q", loaded.SyncToken, state.SyncToken)
	}
	if !loaded.LastSyncAt.Equal(state.LastSyncAt) {
		t.Errorf("LastSyncAt: got %v, want %v", loaded.LastSyncAt, state.LastSyncAt)
	}
	if got := loaded.GetETag("https://caldav.example.com/cal/1.ics"); got != `"etag-v1"` {
		t.Errorf("ETag: got %q, want %q", got, `"etag-v1"`)
	}
}

func TestSyncState_SaveIsIdempotent(t *testing.T) {
	testutil.NewTestDB(t)

	state, _ := syncer.LoadSyncState(3)
	state.SyncToken = "token-v1"
	if err := state.Save(); err != nil {
		t.Fatalf("first Save: %v", err)
	}

	state.SyncToken = "token-v2"
	if err := state.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	loaded, _ := syncer.LoadSyncState(3)
	if loaded.SyncToken != "token-v2" {
		t.Errorf("SyncToken after upsert: got %q, want token-v2", loaded.SyncToken)
	}
}
