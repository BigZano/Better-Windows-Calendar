package caldav

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"pycalendar/internal/api"
)

func TestICSRoundTrip_TitleAndTimes(t *testing.T) {
	now := time.Date(2026, 5, 27, 14, 0, 0, 0, time.UTC)
	end := now.Add(time.Hour)
	in := api.Event{
		ID:       1,
		Title:    "Team Standup",
		StartTS:  now.Unix(),
		EndTS:    sql.NullInt64{Int64: end.Unix(), Valid: true},
		Timezone: "UTC",
	}

	icsData, err := eventToICS(in)
	if err != nil {
		t.Fatalf("eventToICS: %v", err)
	}

	out, err := icsToEvent(string(icsData))
	if err != nil {
		t.Fatalf("icsToEvent: %v", err)
	}

	if out.Title != in.Title {
		t.Errorf("title: got %q, want %q", out.Title, in.Title)
	}
	if out.StartTS != in.StartTS {
		t.Errorf("start: got %d, want %d", out.StartTS, in.StartTS)
	}
	if !out.EndTS.Valid || out.EndTS.Int64 != in.EndTS.Int64 {
		t.Errorf("end: got %v, want %d", out.EndTS, in.EndTS.Int64)
	}
}

func TestICSRoundTrip_OptionalFields(t *testing.T) {
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	in := api.Event{
		ID:       2,
		Title:    "Workshop",
		StartTS:  now.Unix(),
		Notes:    sql.NullString{String: "Bring laptop", Valid: true},
		Location: sql.NullString{String: "Room 42", Valid: true},
		URL:      sql.NullString{String: "https://example.com/meet", Valid: true},
	}

	icsData, err := eventToICS(in)
	if err != nil {
		t.Fatalf("eventToICS: %v", err)
	}
	out, err := icsToEvent(string(icsData))
	if err != nil {
		t.Fatalf("icsToEvent: %v", err)
	}

	if out.Notes.String != in.Notes.String {
		t.Errorf("notes: got %q, want %q", out.Notes.String, in.Notes.String)
	}
	if out.Location.String != in.Location.String {
		t.Errorf("location: got %q, want %q", out.Location.String, in.Location.String)
	}
	if out.URL.String != in.URL.String {
		t.Errorf("url: got %q, want %q", out.URL.String, in.URL.String)
	}
}

func TestICSRoundTrip_AllDay(t *testing.T) {
	day := time.Date(2026, 12, 25, 0, 0, 0, 0, time.Local)
	in := api.Event{
		ID:      3,
		Title:   "Christmas",
		StartTS: day.Unix(),
		AllDay:  true,
	}

	icsData, err := eventToICS(in)
	if err != nil {
		t.Fatalf("eventToICS: %v", err)
	}
	out, err := icsToEvent(string(icsData))
	if err != nil {
		t.Fatalf("icsToEvent: %v", err)
	}

	if !out.AllDay {
		t.Errorf("AllDay: got false, want true")
	}
	if out.Title != in.Title {
		t.Errorf("title: got %q, want %q", out.Title, in.Title)
	}
}

func TestICSRoundTrip_RecurrenceRule(t *testing.T) {
	now := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	in := api.Event{
		ID:             4,
		Title:          "Weekly Sync",
		StartTS:        now.Unix(),
		RecurrenceRule: sql.NullString{String: "FREQ=WEEKLY;BYDAY=MO", Valid: true},
	}

	icsData, err := eventToICS(in)
	if err != nil {
		t.Fatalf("eventToICS: %v", err)
	}
	out, err := icsToEvent(string(icsData))
	if err != nil {
		t.Fatalf("icsToEvent: %v", err)
	}

	if !out.RecurrenceRule.Valid {
		t.Fatal("recurrence rule not preserved")
	}
	if !strings.Contains(out.RecurrenceRule.String, "FREQ=WEEKLY") {
		t.Errorf("rrule: got %q, want to contain FREQ=WEEKLY", out.RecurrenceRule.String)
	}
}

func TestICSToEvent_NoVEVENT(t *testing.T) {
	_, err := icsToEvent("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n")
	if err == nil {
		t.Error("expected error for VCALENDAR with no VEVENT")
	}
}
