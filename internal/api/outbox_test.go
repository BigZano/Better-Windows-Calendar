package api

import (
	"testing"
	"time"

	"pycalendar/internal/testutil"
)

func TestCreateEvent_LocalCalendar_DoesNotEnqueue(t *testing.T) {
	testutil.NewTestDB(t) // default Local calendar is id=1

	if _, err := CreateEvent("Local event", time.Now(), nil, "", nil, "", false, "UTC", 1, "", ""); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}
	if pend, _ := PendingOutbox(1); len(pend) != 0 {
		t.Errorf("local-calendar create enqueued %d outbox entries, want 0", len(pend))
	}
}

func TestCreateEvent_SyncedCalendar_EnqueuesUpsert(t *testing.T) {
	testutil.NewTestDB(t)

	calID, err := CreateCalendar("Work", "#3B82F6", CalendarTypeCalDAV)
	if err != nil {
		t.Fatalf("CreateCalendar: %v", err)
	}

	if _, err := CreateEvent("Synced event", time.Now(), nil, "", nil, "", false, "UTC", calID, "", ""); err != nil {
		t.Fatalf("CreateEvent: %v", err)
	}

	pend, err := PendingOutbox(calID)
	if err != nil {
		t.Fatalf("PendingOutbox: %v", err)
	}
	if len(pend) != 1 {
		t.Fatalf("got %d outbox entries, want 1", len(pend))
	}
	if pend[0].Op != OutboxUpsert {
		t.Errorf("op = %q, want %q", pend[0].Op, OutboxUpsert)
	}
}

func TestDeleteEvent_EnqueuesDeleteOnlyWhenRemote(t *testing.T) {
	db := testutil.NewTestDB(t)

	calID, err := CreateCalendar("Work", "#3B82F6", CalendarTypeCalDAV)
	if err != nil {
		t.Fatalf("CreateCalendar: %v", err)
	}

	// Event never pushed (no resource_url): deleting it should NOT enqueue.
	var localOnlyID int64
	db.QueryRow(`
		INSERT INTO events (title, start_ts, updated_ts, created_ts, calendar_id)
		VALUES ('NeverPushed', ?, ?, ?, ?) RETURNING id`,
		time.Now().Unix(), time.Now().Unix(), time.Now().Unix(), calID,
	).Scan(&localOnlyID)
	if err := DeleteEvent(localOnlyID); err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}
	if pend, _ := PendingOutbox(calID); len(pend) != 0 {
		t.Fatalf("deleting an unpushed event enqueued %d entries, want 0", len(pend))
	}

	// Event with a resource_url: deleting it should enqueue a delete.
	var remoteID int64
	db.QueryRow(`
		INSERT INTO events (title, start_ts, updated_ts, created_ts, calendar_id, resource_url)
		VALUES ('WasPushed', ?, ?, ?, ?, 'https://remote/x.ics') RETURNING id`,
		time.Now().Unix(), time.Now().Unix(), time.Now().Unix(), calID,
	).Scan(&remoteID)
	if err := DeleteEvent(remoteID); err != nil {
		t.Fatalf("DeleteEvent: %v", err)
	}
	pend, _ := PendingOutbox(calID)
	if len(pend) != 1 {
		t.Fatalf("got %d entries after deleting a pushed event, want 1", len(pend))
	}
	if pend[0].Op != OutboxDelete || pend[0].ResourceURL != "https://remote/x.ics" {
		t.Errorf("entry = {op:%q url:%q}, want {delete, https://remote/x.ics}", pend[0].Op, pend[0].ResourceURL)
	}
}
