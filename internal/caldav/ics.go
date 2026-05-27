package caldav

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	ical "github.com/arran4/golang-ical"

	"pycalendar/internal/api"
)

// icsToEvent parses a VCALENDAR string and returns the first VEVENT as an api.Event.
func icsToEvent(data string) (api.Event, error) {
	cal, err := ical.ParseCalendar(strings.NewReader(data))
	if err != nil {
		return api.Event{}, fmt.Errorf("parse ics: %w", err)
	}
	for _, comp := range cal.Components {
		v, ok := comp.(*ical.VEvent)
		if !ok {
			continue
		}
		return veventToEvent(v)
	}
	return api.Event{}, fmt.Errorf("no VEVENT in ICS data")
}

func veventToEvent(v *ical.VEvent) (api.Event, error) {
	var e api.Event

	e.Title = icalPropText(v, ical.ComponentPropertySummary)
	if e.Title == "" {
		e.Title = "(no title)"
	}

	if t, allDay, err := icalParseDTProp(v, ical.ComponentPropertyDtStart); err == nil {
		e.StartTS = t.Unix()
		e.AllDay = allDay
		e.Timezone = t.Location().String()
	}
	if t, _, err := icalParseDTProp(v, ical.ComponentPropertyDtEnd); err == nil {
		e.EndTS = sql.NullInt64{Int64: t.Unix(), Valid: true}
	}
	if s := icalPropText(v, ical.ComponentPropertyDescription); s != "" {
		e.Notes = sql.NullString{String: s, Valid: true}
	}
	if s := icalPropText(v, ical.ComponentPropertyLocation); s != "" {
		e.Location = sql.NullString{String: s, Valid: true}
	}
	if s := icalPropText(v, ical.ComponentPropertyUrl); s != "" {
		e.URL = sql.NullString{String: s, Valid: true}
	}
	if s := icalPropText(v, ical.ComponentPropertyRrule); s != "" {
		e.RecurrenceRule = sql.NullString{String: s, Valid: true}
	}

	now := time.Now().Unix()
	e.CreatedTS = now
	e.UpdatedTS = now

	return e, nil
}

// eventToICS serialises an api.Event as a VCALENDAR byte slice for PUT to the server.
func eventToICS(e api.Event) ([]byte, error) {
	cal := ical.NewCalendar()
	cal.SetProductId("-//PyCalendar//PyCalendar//EN")

	ev := ical.NewEvent(eventUID(e))
	ev.SetDtStampTime(time.Now())
	ev.SetSummary(e.Title)

	start := time.Unix(e.StartTS, 0)
	if e.AllDay {
		ev.SetAllDayStartAt(start)
		if e.EndTS.Valid {
			ev.SetAllDayEndAt(time.Unix(e.EndTS.Int64, 0))
		}
	} else {
		ev.SetStartAt(start)
		if e.EndTS.Valid {
			ev.SetEndAt(time.Unix(e.EndTS.Int64, 0))
		}
	}

	if e.Notes.Valid && e.Notes.String != "" {
		ev.SetDescription(e.Notes.String)
	}
	if e.Location.Valid && e.Location.String != "" {
		ev.SetLocation(e.Location.String)
	}
	if e.URL.Valid && e.URL.String != "" {
		ev.SetURL(e.URL.String)
	}
	if e.RecurrenceRule.Valid && e.RecurrenceRule.String != "" {
		ev.AddRrule(e.RecurrenceRule.String)
	}

	cal.AddVEvent(ev)

	var buf strings.Builder
	if err := cal.SerializeTo(&buf); err != nil {
		return nil, fmt.Errorf("serialise ics: %w", err)
	}
	return []byte(buf.String()), nil
}

func eventUID(e api.Event) string {
	if e.ID > 0 {
		return fmt.Sprintf("pycalendar-%d@local", e.ID)
	}
	return fmt.Sprintf("pycalendar-new-%d@local", time.Now().UnixNano())
}

// ---- ICS parsing helpers ----

func icalPropText(v *ical.VEvent, prop ical.ComponentProperty) string {
	p := v.GetProperty(prop)
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.Value)
}

// icalParseDTProp parses a DATE or DATE-TIME property from a VEVENT.
// Returns (time, isAllDay, error).
func icalParseDTProp(v *ical.VEvent, prop ical.ComponentProperty) (time.Time, bool, error) {
	p := v.GetProperty(prop)
	if p == nil {
		return time.Time{}, false, fmt.Errorf("property %s missing", prop)
	}
	val := strings.TrimSpace(p.Value)

	// DATE-only: YYYYMMDD
	if len(val) == 8 {
		t, err := time.ParseInLocation("20060102", val, time.Local)
		return t, true, err
	}

	// DATE-TIME with explicit TZID parameter
	if tzid := icalParamValue(p, "TZID"); tzid != "" {
		loc, err := time.LoadLocation(tzid)
		if err != nil {
			loc = time.Local
		}
		t, err := time.ParseInLocation("20060102T150405", strings.TrimSuffix(val, "Z"), loc)
		return t, false, err
	}

	// DATE-TIME UTC (trailing Z)
	if strings.HasSuffix(val, "Z") {
		t, err := time.Parse("20060102T150405Z", val)
		if err == nil {
			t = t.In(time.Local)
		}
		return t, false, err
	}

	// DATE-TIME local (no Z, no TZID)
	t, err := time.ParseInLocation("20060102T150405", val, time.Local)
	return t, false, err
}

func icalParamValue(p *ical.IANAProperty, name string) string {
	if p == nil {
		return ""
	}
	vals, ok := p.ICalParameters[name]
	if !ok || len(vals) == 0 {
		return ""
	}
	return vals[0]
}
