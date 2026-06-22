package msgraph

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"pycalendar/internal/api"
)

func TestGraphDateTimeZone_Parse_UTC(t *testing.T) {
	dt := graphDateTimeZone{DateTime: "2026-05-27T14:00:00.0000000", TimeZone: "UTC"}
	got, err := dt.parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := time.Date(2026, 5, 27, 14, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestGraphDateTimeZone_Parse_DateOnly(t *testing.T) {
	dt := graphDateTimeZone{DateTime: "2026-12-25", TimeZone: "UTC"}
	got, err := dt.parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Year() != 2026 || got.Month() != 12 || got.Day() != 25 {
		t.Errorf("got %v, want 2026-12-25", got)
	}
}

func TestGraphDateTimeZone_Parse_NoFractional(t *testing.T) {
	dt := graphDateTimeZone{DateTime: "2026-01-15T09:30:00", TimeZone: "UTC"}
	got, err := dt.parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := time.Date(2026, 1, 15, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestGraphDateTimeZone_Parse_UnknownTZ_FallsBackToUTC(t *testing.T) {
	dt := graphDateTimeZone{DateTime: "2026-05-27T10:00:00", TimeZone: "Pacific Standard Time"}
	got, err := dt.parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.Hour() != 10 || got.Minute() != 0 {
		t.Errorf("got %v, want hour=10 min=0", got)
	}
}

func TestGraphEventToEvent_BasicFields(t *testing.T) {
	ge := graphEvent{
		ID:       "AAMkAG123",
		Subject:  "Board Meeting",
		Start:    graphDateTimeZone{DateTime: "2026-06-01T13:00:00", TimeZone: "UTC"},
		End:      graphDateTimeZone{DateTime: "2026-06-01T14:00:00", TimeZone: "UTC"},
		Body:     graphItemBody{ContentType: "text", Content: "Agenda attached"},
		Location: graphLocation{DisplayName: "Conference Room A"},
		WebLink:  "https://outlook.office.com/event/AAMkAG123",
	}

	e, err := graphEventToEvent(ge)
	if err != nil {
		t.Fatalf("graphEventToEvent: %v", err)
	}

	if e.Title != "Board Meeting" {
		t.Errorf("title: got %q", e.Title)
	}
	wantStart := time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC).Unix()
	if e.StartTS != wantStart {
		t.Errorf("start: got %d, want %d", e.StartTS, wantStart)
	}
	if !e.EndTS.Valid {
		t.Error("end not set")
	}
	if e.Notes.String != "Agenda attached" {
		t.Errorf("notes: got %q", e.Notes.String)
	}
	if e.Location.String != "Conference Room A" {
		t.Errorf("location: got %q", e.Location.String)
	}
	if e.URL.String != "https://outlook.office.com/event/AAMkAG123" {
		t.Errorf("url: got %q", e.URL.String)
	}
}

func TestGraphEventToEvent_EmptySubjectBecomesNoTitle(t *testing.T) {
	ge := graphEvent{
		Start: graphDateTimeZone{DateTime: "2026-06-01T08:00:00", TimeZone: "UTC"},
		End:   graphDateTimeZone{DateTime: "2026-06-01T09:00:00", TimeZone: "UTC"},
	}
	e, err := graphEventToEvent(ge)
	if err != nil {
		t.Fatalf("graphEventToEvent: %v", err)
	}
	if e.Title != "(no title)" {
		t.Errorf("title: got %q, want \"(no title)\"", e.Title)
	}
}

func TestGraphEventToEvent_AllDay(t *testing.T) {
	ge := graphEvent{
		Subject:  "Holiday",
		IsAllDay: true,
		Start:    graphDateTimeZone{DateTime: "2026-07-04", TimeZone: "UTC"},
		End:      graphDateTimeZone{DateTime: "2026-07-05", TimeZone: "UTC"},
	}
	e, err := graphEventToEvent(ge)
	if err != nil {
		t.Fatalf("graphEventToEvent: %v", err)
	}
	if !e.AllDay {
		t.Error("AllDay not set")
	}
}

func TestEventToGraphBody_TimedEvent(t *testing.T) {
	now := time.Date(2026, 6, 1, 13, 0, 0, 0, time.UTC)
	ev := api.Event{
		Title:    "Sync",
		StartTS:  now.Unix(),
		EndTS:    sql.NullInt64{Int64: now.Add(time.Hour).Unix(), Valid: true},
		Timezone: "UTC",
	}
	b, err := eventToGraphBody(ev)
	if err != nil {
		t.Fatalf("eventToGraphBody: %v", err)
	}
	body := string(b)
	if !strings.Contains(body, `"Sync"`) {
		t.Errorf("subject missing: %s", body)
	}
	if !strings.Contains(body, "2026-06-01T13:00:00") {
		t.Errorf("start datetime missing: %s", body)
	}
}

func TestEventToGraphBody_AllDayUsesDateFormat(t *testing.T) {
	day := time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC)
	ev := api.Event{
		Title:    "Christmas",
		AllDay:   true,
		StartTS:  day.Unix(),
		Timezone: "UTC",
	}
	b, err := eventToGraphBody(ev)
	if err != nil {
		t.Fatalf("eventToGraphBody: %v", err)
	}
	body := string(b)
	// All-day: start should be date-only (no 'T')
	if !strings.Contains(body, `"2026-12-25"`) {
		t.Errorf("expected date-only format in: %s", body)
	}
	if strings.Contains(body, "T00:00:00") {
		t.Errorf("all-day should not have time component: %s", body)
	}
}
