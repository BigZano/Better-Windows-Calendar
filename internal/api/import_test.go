package api

import (
	"database/sql"
	"testing"
	"time"

	"pycalendar/internal/testutil"
)

func TestCreateImportedEvent_NoDefaultReminder(t *testing.T) {
	db := testutil.NewTestDB(t)

	start := time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC)
	id, err := CreateImportedEvent(ImportedEvent{
		UID:        "uid-1",
		Title:      "Imported",
		Start:      start,
		Timezone:   "America/New_York",
		CalendarID: 1,
	})
	if err != nil {
		t.Fatalf("CreateImportedEvent: %v", err)
	}

	var reminder sql.NullInt64
	var tz, uid string
	err = db.QueryRow(
		`SELECT reminder_ts, timezone, uid FROM events WHERE id = ?`, id,
	).Scan(&reminder, &tz, &uid)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if reminder.Valid {
		t.Errorf("reminder_ts = %d, want NULL", reminder.Int64)
	}
	if tz != "America/New_York" {
		t.Errorf("timezone = %q, want America/New_York", tz)
	}
	if uid != "uid-1" {
		t.Errorf("uid = %q, want uid-1", uid)
	}
}

func TestCreateImportedEvent_ExplicitReminder(t *testing.T) {
	db := testutil.NewTestDB(t)

	start := time.Date(2025, 3, 1, 9, 0, 0, 0, time.UTC)
	mins := 30
	id, err := CreateImportedEvent(ImportedEvent{
		Title:           "Reminded",
		Start:           start,
		CalendarID:      1,
		ReminderMinutes: &mins,
	})
	if err != nil {
		t.Fatalf("CreateImportedEvent: %v", err)
	}

	var reminder sql.NullInt64
	if err := db.QueryRow(`SELECT reminder_ts FROM events WHERE id = ?`, id).Scan(&reminder); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !reminder.Valid {
		t.Fatal("reminder_ts should be set when ReminderMinutes is provided")
	}
	if want := start.Unix() - int64(mins*60); reminder.Int64 != want {
		t.Errorf("reminder_ts = %d, want %d", reminder.Int64, want)
	}
}

func TestFindDuplicateEvent_UIDAndFallback(t *testing.T) {
	testutil.NewTestDB(t)

	start := time.Date(2025, 5, 1, 12, 0, 0, 0, time.UTC)
	if _, err := CreateImportedEvent(ImportedEvent{
		UID: "dup-uid", Title: "Dup", Start: start, CalendarID: 1,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Match by UID.
	if ok, _ := FindDuplicateEvent(1, "dup-uid", "anything", 0); !ok {
		t.Error("should match by UID")
	}
	// Different UID, same title+start → not a UID match, and the title/start
	// branch is not taken because a UID was provided.
	if ok, _ := FindDuplicateEvent(1, "other-uid", "Dup", start.Unix()); ok {
		t.Error("different UID should not be a duplicate")
	}

	// UID-absent event dedups on title+start.
	noUIDStart := time.Date(2025, 6, 1, 8, 0, 0, 0, time.UTC)
	if _, err := CreateImportedEvent(ImportedEvent{
		Title: "NoUID", Start: noUIDStart, CalendarID: 1,
	}); err != nil {
		t.Fatalf("create no-uid: %v", err)
	}
	if ok, _ := FindDuplicateEvent(1, "", "NoUID", noUIDStart.Unix()); !ok {
		t.Error("should match by title+start when UID absent")
	}
	if ok, _ := FindDuplicateEvent(1, "", "NoUID", noUIDStart.Unix()+1); ok {
		t.Error("different start should not match")
	}
}

func TestFindDuplicateEvent_ScopedToCalendar(t *testing.T) {
	testutil.NewTestDB(t)

	other, err := CreateCalendar("Other", "#3B82F6", CalendarTypeLocal)
	if err != nil {
		t.Fatalf("CreateCalendar: %v", err)
	}

	start := time.Date(2025, 5, 1, 12, 0, 0, 0, time.UTC)
	if _, err := CreateImportedEvent(ImportedEvent{
		UID: "scoped", Title: "Scoped", Start: start, CalendarID: 1,
	}); err != nil {
		t.Fatalf("create: %v", err)
	}

	if ok, _ := FindDuplicateEvent(1, "scoped", "", 0); !ok {
		t.Error("should be a duplicate in its own calendar")
	}
	if ok, _ := FindDuplicateEvent(other, "scoped", "", 0); ok {
		t.Error("same UID in a different calendar must not be a duplicate")
	}
}
