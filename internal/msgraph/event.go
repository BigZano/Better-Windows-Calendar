package msgraph

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"pycalendar/internal/api"
)

// graphEvent mirrors the subset of the Microsoft Graph event resource we use.
type graphEvent struct {
	ID                   string               `json:"id"`
	Subject              string               `json:"subject"`
	Start                graphDateTimeZone    `json:"start"`
	End                  graphDateTimeZone    `json:"end"`
	IsAllDay             bool                 `json:"isAllDay"`
	Body                 graphItemBody        `json:"body"`
	Location             graphLocation        `json:"location"`
	WebLink              string               `json:"webLink"`
	LastModifiedDateTime string               `json:"lastModifiedDateTime"`
	Removed              *graphRemoved        `json:"@removed,omitempty"`
}

type graphDateTimeZone struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type graphItemBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type graphLocation struct {
	DisplayName string `json:"displayName"`
}

type graphRemoved struct {
	Reason string `json:"reason"`
}

// parse converts a Graph date/time + timezone pair to a time.Time.
// Graph returns "2026-05-26T14:00:00.0000000"; fractional precision varies.
// Windows timezone IDs (e.g. "Pacific Standard Time") are not supported by
// time.LoadLocation; we fall back to UTC in that case.
func (dt graphDateTimeZone) parse() (time.Time, error) {
	loc := time.UTC
	if dt.TimeZone != "" && dt.TimeZone != "tzone://Microsoft/UTC" {
		if l, err := time.LoadLocation(dt.TimeZone); err == nil {
			loc = l
		}
	}

	s := dt.DateTime
	// Trim fractional seconds to at most 9 digits (Go handles up to nanoseconds).
	if idx := strings.IndexByte(s, '.'); idx >= 0 {
		frac := s[idx:]
		if len(frac) > 10 {
			s = s[:idx+10]
		}
	}

	for _, layout := range []string{
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("msgraph: cannot parse datetime %q in zone %q", dt.DateTime, dt.TimeZone)
}

// graphEventToEvent converts a Graph event to an api.Event.
// Recurrence is not converted — Graph's JSON recurrence schema differs from RRULE;
// recurring events round-trip without their recurrence rule for now.
func graphEventToEvent(ge graphEvent) (api.Event, error) {
	var e api.Event

	e.Title = ge.Subject
	if e.Title == "" {
		e.Title = "(no title)"
	}
	e.AllDay = ge.IsAllDay

	start, err := ge.Start.parse()
	if err != nil {
		return api.Event{}, err
	}
	e.StartTS = start.Unix()
	e.Timezone = start.Location().String()

	if end, err := ge.End.parse(); err == nil {
		e.EndTS = sql.NullInt64{Int64: end.Unix(), Valid: true}
	}

	if ge.Body.Content != "" {
		e.Notes = sql.NullString{String: ge.Body.Content, Valid: true}
	}
	if ge.Location.DisplayName != "" {
		e.Location = sql.NullString{String: ge.Location.DisplayName, Valid: true}
	}
	if ge.WebLink != "" {
		e.URL = sql.NullString{String: ge.WebLink, Valid: true}
	}

	now := time.Now().Unix()
	e.CreatedTS = now
	e.UpdatedTS = now
	if ge.LastModifiedDateTime != "" {
		if t, err := time.Parse(time.RFC3339, ge.LastModifiedDateTime); err == nil {
			e.UpdatedTS = t.Unix()
		}
	}

	return e, nil
}

// graphEventBody is the JSON request body for Graph POST/PATCH.
// Both start and end are required by the Graph API.
type graphEventBody struct {
	Subject  string            `json:"subject"`
	Start    graphDateTimeZone `json:"start"`
	End      graphDateTimeZone `json:"end"`
	IsAllDay bool              `json:"isAllDay"`
	Body     *graphItemBody    `json:"body,omitempty"`
	Location *graphLocation    `json:"location,omitempty"`
}

// eventToGraphBody serialises an api.Event to a Graph POST/PATCH JSON body.
// When no end time is stored, defaults to start+1h (timed) or start+1day (all-day).
func eventToGraphBody(e api.Event) ([]byte, error) {
	tz := e.Timezone
	if tz == "" || tz == "Local" {
		tz = "UTC"
	}

	body := graphEventBody{
		Subject:  e.Title,
		IsAllDay: e.AllDay,
	}

	if e.AllDay {
		startDate := time.Unix(e.StartTS, 0).UTC()
		endDate := startDate.AddDate(0, 0, 1)
		if e.EndTS.Valid {
			endDate = time.Unix(e.EndTS.Int64, 0).UTC()
		}
		body.Start = graphDateTimeZone{DateTime: startDate.Format("2006-01-02"), TimeZone: tz}
		body.End = graphDateTimeZone{DateTime: endDate.Format("2006-01-02"), TimeZone: tz}
	} else {
		loc := time.UTC
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
		startT := time.Unix(e.StartTS, 0).In(loc)
		endT := startT.Add(time.Hour)
		if e.EndTS.Valid {
			endT = time.Unix(e.EndTS.Int64, 0).In(loc)
		}
		body.Start = graphDateTimeZone{DateTime: startT.Format("2006-01-02T15:04:05"), TimeZone: tz}
		body.End = graphDateTimeZone{DateTime: endT.Format("2006-01-02T15:04:05"), TimeZone: tz}
	}

	if e.Notes.Valid && e.Notes.String != "" {
		body.Body = &graphItemBody{ContentType: "text", Content: e.Notes.String}
	}
	if e.Location.Valid && e.Location.String != "" {
		body.Location = &graphLocation{DisplayName: e.Location.String}
	}

	return json.Marshal(body)
}
