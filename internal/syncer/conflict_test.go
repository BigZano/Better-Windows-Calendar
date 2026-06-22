package syncer_test

import (
	"testing"
	"time"

	"pycalendar/internal/api"
	"pycalendar/internal/syncer"
	"pycalendar/internal/testutil"
)

func TestRecordAndGetPendingConflicts(t *testing.T) {
	testutil.NewTestDB(t)

	local := api.Event{ID: 10, Title: "My version", StartTS: time.Now().Unix()}
	remote := api.Event{ID: 10, Title: "Their version", StartTS: time.Now().Unix()}

	if err := syncer.RecordConflict(1, local, remote); err != nil {
		t.Fatalf("RecordConflict: %v", err)
	}

	conflicts, err := syncer.GetPendingConflicts(1)
	if err != nil {
		t.Fatalf("GetPendingConflicts: %v", err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("got %d conflicts, want 1", len(conflicts))
	}
	c := conflicts[0]
	if c.CalendarID != 1 {
		t.Errorf("CalendarID: got %d, want 1", c.CalendarID)
	}
	if c.EventID != 10 {
		t.Errorf("EventID: got %d, want 10", c.EventID)
	}
	if c.LocalJSON == "" || c.RemoteJSON == "" {
		t.Error("LocalJSON or RemoteJSON empty")
	}
	if c.ResolvedAt != nil {
		t.Error("ResolvedAt should be nil for new conflict")
	}
}

func TestGetPendingConflicts_ExcludesOtherCalendars(t *testing.T) {
	testutil.NewTestDB(t)

	ev := api.Event{ID: 1, Title: "Event", StartTS: time.Now().Unix()}
	syncer.RecordConflict(1, ev, ev)
	syncer.RecordConflict(2, ev, ev)

	conflicts, _ := syncer.GetPendingConflicts(1)
	if len(conflicts) != 1 {
		t.Errorf("got %d conflicts for calendar 1, want 1", len(conflicts))
	}
}

func TestResolveConflict(t *testing.T) {
	testutil.NewTestDB(t)

	ev := api.Event{ID: 5, Title: "Event", StartTS: time.Now().Unix()}
	syncer.RecordConflict(1, ev, ev)

	pending, _ := syncer.GetPendingConflicts(1)
	if len(pending) != 1 {
		t.Fatalf("setup: got %d pending, want 1", len(pending))
	}

	if err := syncer.ResolveConflict(pending[0].ID, "keep-local"); err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}

	after, _ := syncer.GetPendingConflicts(1)
	if len(after) != 0 {
		t.Errorf("got %d pending after resolve, want 0", len(after))
	}
}

func TestPruneStaleConflicts(t *testing.T) {
	db := testutil.NewTestDB(t)

	ev := api.Event{ID: 7, Title: "Old Event", StartTS: time.Now().Unix()}
	syncer.RecordConflict(1, ev, ev)
	pending, _ := syncer.GetPendingConflicts(1)
	syncer.ResolveConflict(pending[0].ID, "remote-wins")

	// Recent resolved conflict — prune should leave it.
	if err := syncer.PruneStaleConflicts(); err != nil {
		t.Fatalf("PruneStaleConflicts: %v", err)
	}

	// Age the resolved_at timestamp beyond 30 days.
	old := time.Now().AddDate(0, 0, -31).Unix()
	if _, err := db.Exec(`UPDATE conflicts SET resolved_at = ? WHERE resolved_at IS NOT NULL`, old); err != nil {
		t.Fatalf("age timestamp: %v", err)
	}

	if err := syncer.PruneStaleConflicts(); err != nil {
		t.Fatalf("PruneStaleConflicts after aging: %v", err)
	}

	// All resolved conflicts older than 30 days should be gone.
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM conflicts`).Scan(&count)
	if count != 0 {
		t.Errorf("got %d rows after prune, want 0", count)
	}
}
