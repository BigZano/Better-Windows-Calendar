package icsimport

import (
	"database/sql"
	"strings"
	"testing"

	"pycalendar/internal/api"
	"pycalendar/internal/testutil"
)

// sampleICS is a small calendar with: a timed event with TZID + UID + VALARM,
// an all-day event, an event with no UID, and an event missing a title.
const sampleICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Test Corp//Test Suite//EN
BEGIN:VEVENT
UID:evt-1@test
ORGANIZER:mailto:boss@test
SUMMARY:Timed Meeting
DTSTART;TZID=America/New_York:20250115T090000
DTEND;TZID=America/New_York:20250115T100000
DESCRIPTION:Quarterly sync
LOCATION:Room 4
BEGIN:VALARM
ACTION:DISPLAY
TRIGGER:-PT15M
END:VALARM
END:VEVENT
BEGIN:VEVENT
UID:evt-2@test
SUMMARY:All Day Holiday
DTSTART;VALUE=DATE:20250704
END:VEVENT
BEGIN:VEVENT
SUMMARY:No UID Event
DTSTART:20251201T120000Z
END:VEVENT
BEGIN:VEVENT
DTSTART:20251215T120000Z
END:VEVENT
END:VCALENDAR`

func parseSample(t *testing.T) *Preview {
	t.Helper()
	p, err := Parse(strings.NewReader(sampleICS))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return p
}

func TestParse_CountAndProdID(t *testing.T) {
	p := parseSample(t)
	if p.ProdID != "-//Test Corp//Test Suite//EN" {
		t.Errorf("ProdID = %q, want test prodid", p.ProdID)
	}
	if p.Organizer != "boss@test" {
		t.Errorf("Organizer = %q, want boss@test", p.Organizer)
	}
	// 3 importable events (the title-less one is excluded from Count).
	if p.Count != 3 {
		t.Errorf("Count = %d, want 3", p.Count)
	}
}

func TestParse_Span(t *testing.T) {
	p := parseSample(t)
	if p.SpanStart.IsZero() || p.SpanEnd.IsZero() {
		t.Fatalf("span not computed: start=%v end=%v", p.SpanStart, p.SpanEnd)
	}
	if p.SpanStart.Year() != 2025 || p.SpanStart.Month() != 1 {
		t.Errorf("SpanStart = %v, want Jan 2025", p.SpanStart)
	}
	// Latest start is the title-less Dec 15 event — but it's skipped, so the
	// span end should reflect the last *titled* event (Dec 1).
	if p.SpanEnd.Month() != 12 {
		t.Errorf("SpanEnd = %v, want December", p.SpanEnd)
	}
}

func findEvent(p *Preview, title string) *ParsedEvent {
	for i := range p.Events {
		if p.Events[i].Title == title {
			return &p.Events[i]
		}
	}
	return nil
}

func TestParse_TimedEventDetails(t *testing.T) {
	p := parseSample(t)
	ev := findEvent(p, "Timed Meeting")
	if ev == nil {
		t.Fatal("Timed Meeting not parsed")
	}
	if ev.UID != "evt-1@test" {
		t.Errorf("UID = %q, want evt-1@test", ev.UID)
	}
	if ev.AllDay {
		t.Error("Timed Meeting should not be all-day")
	}
	if ev.Timezone != "America/New_York" {
		t.Errorf("Timezone = %q, want America/New_York", ev.Timezone)
	}
	if ev.End == nil {
		t.Error("Timed Meeting should have an end time")
	}
	if ev.Reminder == nil || *ev.Reminder != 15 {
		t.Errorf("Reminder = %v, want 15", ev.Reminder)
	}
	if ev.Location != "Room 4" {
		t.Errorf("Location = %q, want Room 4", ev.Location)
	}
}

func TestParse_AllDayEvent(t *testing.T) {
	p := parseSample(t)
	ev := findEvent(p, "All Day Holiday")
	if ev == nil {
		t.Fatal("All Day Holiday not parsed")
	}
	if !ev.AllDay {
		t.Error("All Day Holiday should be all-day")
	}
	if ev.Reminder != nil {
		t.Errorf("all-day event got reminder %v, want nil", ev.Reminder)
	}
}

func TestParse_TimezoneNeverImport(t *testing.T) {
	p := parseSample(t)
	for _, e := range p.Events {
		if e.Timezone == "import" {
			t.Errorf("event %q has timezone %q (must never be \"import\")", e.Title, e.Timezone)
		}
	}
	// The UTC (trailing-Z) event should report UTC.
	ev := findEvent(p, "No UID Event")
	if ev == nil {
		t.Fatal("No UID Event not parsed")
	}
	if ev.Timezone != "UTC" {
		t.Errorf("UTC event timezone = %q, want UTC", ev.Timezone)
	}
}

func TestCommit_MissingTitleSkipped(t *testing.T) {
	testutil.NewTestDB(t)
	p := parseSample(t)

	res, err := Commit(p, 1)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if res.Imported != 3 {
		t.Errorf("Imported = %d, want 3", res.Imported)
	}
	if res.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (title-less event)", res.Skipped)
	}
	if res.Duplicates != 0 {
		t.Errorf("Duplicates = %d, want 0", res.Duplicates)
	}
}

func TestCommit_NoDefaultReminderAndRealTimezone(t *testing.T) {
	db := testutil.NewTestDB(t)
	p := parseSample(t)
	if _, err := Commit(p, 1); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// The all-day event must have a NULL reminder_ts and a non-"import" tz.
	var reminder sql.NullInt64
	var tz string
	err := db.QueryRow(
		`SELECT reminder_ts, timezone FROM events WHERE title = ?`, "All Day Holiday",
	).Scan(&reminder, &tz)
	if err != nil {
		t.Fatalf("query all-day event: %v", err)
	}
	if reminder.Valid {
		t.Errorf("imported event reminder_ts = %d, want NULL (no default reminder)", reminder.Int64)
	}
	if tz == "import" {
		t.Errorf("timezone = %q, must never be \"import\"", tz)
	}

	// The VALARM-bearing event SHOULD carry a reminder_ts.
	var alarmReminderTS sql.NullInt64
	if err := db.QueryRow(
		`SELECT reminder_ts FROM events WHERE title = ?`, "Timed Meeting",
	).Scan(&alarmReminderTS); err != nil {
		t.Fatalf("query timed event: %v", err)
	}
	if !alarmReminderTS.Valid {
		t.Error("event with VALARM should have a non-NULL reminder_ts")
	}
}

func TestCommit_DedupSameUID(t *testing.T) {
	db := testutil.NewTestDB(t)
	p := parseSample(t)

	first, err := Commit(p, 1)
	if err != nil {
		t.Fatalf("Commit (first): %v", err)
	}
	if first.Imported != 3 {
		t.Fatalf("first import = %d, want 3", first.Imported)
	}

	// Re-parse and re-import the same file: all titled events are duplicates.
	p2 := parseSample(t)
	second, err := Commit(p2, 1)
	if err != nil {
		t.Fatalf("Commit (second): %v", err)
	}
	if second.Imported != 0 {
		t.Errorf("second import Imported = %d, want 0 (all dups)", second.Imported)
	}
	if second.Duplicates != 3 {
		t.Errorf("second import Duplicates = %d, want 3", second.Duplicates)
	}

	// Total event rows should still be 3, not 6.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if n != 3 {
		t.Errorf("event count after re-import = %d, want 3", n)
	}
}

func TestCommit_DedupNoUIDOnTitleAndStart(t *testing.T) {
	testutil.NewTestDB(t)
	p := parseSample(t)
	if _, err := Commit(p, 1); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// "No UID Event" has no UID; dedup must fall back to (title, start).
	exists, err := api.FindDuplicateEvent(1, "", "No UID Event", findStart(t, p, "No UID Event"))
	if err != nil {
		t.Fatalf("FindDuplicateEvent: %v", err)
	}
	if !exists {
		t.Error("UID-absent event should be found by (title, start) fallback")
	}
}

func TestCommit_DifferentCalendarNotDuplicate(t *testing.T) {
	testutil.NewTestDB(t)
	other, err := api.CreateCalendar("Work", "#3B82F6", api.CalendarTypeLocal)
	if err != nil {
		t.Fatalf("CreateCalendar: %v", err)
	}

	if _, err := Commit(parseSample(t), 1); err != nil {
		t.Fatalf("Commit cal 1: %v", err)
	}
	// Same file into a different calendar: nothing should be treated as a dup.
	res, err := Commit(parseSample(t), other)
	if err != nil {
		t.Fatalf("Commit cal %d: %v", other, err)
	}
	if res.Imported != 3 {
		t.Errorf("import into other calendar Imported = %d, want 3", res.Imported)
	}
	if res.Duplicates != 0 {
		t.Errorf("import into other calendar Duplicates = %d, want 0", res.Duplicates)
	}
}

func findStart(t *testing.T, p *Preview, title string) int64 {
	t.Helper()
	ev := findEvent(p, title)
	if ev == nil {
		t.Fatalf("event %q not found", title)
	}
	return ev.Start.Unix()
}
