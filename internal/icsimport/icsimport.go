// Package icsimport parses iCalendar (.ics) files in two phases: Parse builds a
// Preview that the Import Dialog can display before any write, and Commit writes
// the previewed events into the local database via the api layer, skipping
// duplicates already present in the target calendar.
package icsimport

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	ical "github.com/arran4/golang-ical"

	"pycalendar/internal/api"
)

// maxImportBytes caps how much of the input stream Parse will read, as a
// defensive guard against accidentally huge files.
const maxImportBytes = 10 << 20 // 10MB

// ParsedEvent is a single VEVENT extracted from an .ics stream, normalised into
// the fields PyCalendar stores.
type ParsedEvent struct {
	UID      string
	Title    string
	Start    time.Time
	End      *time.Time
	AllDay   bool
	Notes    string
	Location string
	URL      string
	RRULE    string
	Timezone string // a real IANA tz string, never "import"
	Reminder *int   // minutes-before-start; nil unless a VALARM trigger was parsed
}

// Preview is the result of parsing an .ics stream without writing anything.
type Preview struct {
	Events    []ParsedEvent
	ProdID    string
	Organizer string
	Count     int
	SpanStart time.Time
	SpanEnd   time.Time
}

// Result summarises a Commit run.
type Result struct {
	Imported   int
	Skipped    int
	Duplicates int
	Errors     []string
}

// Parse reads an iCalendar stream and returns a Preview describing its events
// without writing anything to the database. The reader is wrapped in an
// io.LimitReader at 10MB as a defensive guard.
func Parse(r io.Reader) (*Preview, error) {
	cal, err := ical.ParseCalendar(io.LimitReader(r, maxImportBytes))
	if err != nil {
		return nil, fmt.Errorf("parse ics: %w", err)
	}

	p := &Preview{
		ProdID: calendarProperty(cal, ical.PropertyProductId),
	}

	for _, comp := range cal.Components {
		ev, ok := comp.(*ical.VEvent)
		if !ok {
			continue
		}

		title := propText(ev, ical.ComponentPropertySummary)
		if title == "" {
			// Missing-title events are skipped at commit time; surface the
			// count there. Keep them out of the preview list.
			p.Events = append(p.Events, ParsedEvent{Title: ""})
			continue
		}

		startTime, allDay, tz, err := parseDTStart(ev)
		if err != nil {
			// Record a placeholder so Commit can report the skip; a zero Start
			// with an empty Title would otherwise be ambiguous.
			p.Events = append(p.Events, ParsedEvent{Title: title})
			slog.Warn("ics parse: DTSTART failed", "title", title, "err", err)
			continue
		}

		if p.Organizer == "" {
			if org := propText(ev, ical.ComponentPropertyOrganizer); org != "" {
				p.Organizer = strings.TrimPrefix(org, "mailto:")
			}
		}

		parsed := ParsedEvent{
			UID:      propText(ev, ical.ComponentPropertyUniqueId),
			Title:    title,
			Start:    startTime,
			AllDay:   allDay,
			Notes:    propText(ev, ical.ComponentPropertyDescription),
			Location: propText(ev, ical.ComponentPropertyLocation),
			URL:      propText(ev, ical.ComponentPropertyUrl),
			RRULE:    propText(ev, ical.ComponentPropertyRrule),
			Timezone: tz,
			Reminder: alarmReminder(ev),
		}

		if et, _, _, err := parseDTProp(ev, ical.ComponentPropertyDtEnd); err == nil {
			parsed.End = &et
		}

		p.Events = append(p.Events, parsed)
	}

	// Count and span cover only the events that actually have a start time.
	for _, e := range p.Events {
		if e.Title == "" || e.Start.IsZero() {
			continue
		}
		p.Count++
		if p.SpanStart.IsZero() || e.Start.Before(p.SpanStart) {
			p.SpanStart = e.Start
		}
		end := e.Start
		if e.End != nil {
			end = *e.End
		}
		if p.SpanEnd.IsZero() || end.After(p.SpanEnd) {
			p.SpanEnd = end
		}
	}

	return p, nil
}

// Commit writes the previewed events into the given calendar. For each event it
// checks for an existing duplicate (by UID, else title+start) in the target
// calendar and skips it if found. Events with a missing title or unparseable
// start are skipped. Per-event failures are collected in Result.Errors and do
// not roll back the rest of the batch.
func Commit(p *Preview, calendarID int64) (Result, error) {
	var res Result
	if p == nil {
		return res, nil
	}

	for _, e := range p.Events {
		if e.Title == "" || e.Start.IsZero() {
			res.Skipped++
			continue
		}

		dup, err := api.FindDuplicateEvent(calendarID, e.UID, e.Title, e.Start.Unix())
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("dedup %q: %v", e.Title, err))
			continue
		}
		if dup {
			res.Duplicates++
			continue
		}

		id, err := api.CreateImportedEvent(api.ImportedEvent{
			UID:             e.UID,
			Title:           e.Title,
			Start:           e.Start,
			End:             e.End,
			Timezone:        e.Timezone,
			Notes:           e.Notes,
			RecurrenceRule:  e.RRULE,
			AllDay:          e.AllDay,
			CalendarID:      calendarID,
			Location:        e.Location,
			URL:             e.URL,
			ReminderMinutes: e.Reminder,
		})
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("create %q: %v", e.Title, err))
			continue
		}
		slog.Info("ics import: created event", "id", id, "title", e.Title)
		res.Imported++
	}

	return res, nil
}

// ---- helpers ----

func propText(ev *ical.VEvent, prop ical.ComponentProperty) string {
	p := ev.GetProperty(prop)
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.Value)
}

// calendarProperty reads a top-level VCALENDAR property (e.g. PRODID).
func calendarProperty(cal *ical.Calendar, prop ical.Property) string {
	for _, p := range cal.CalendarProperties {
		if p.IANAToken == string(prop) {
			return strings.TrimSpace(p.Value)
		}
	}
	return ""
}

// parseDTStart extracts the start time, all-day flag, and timezone from a VEVENT.
func parseDTStart(ev *ical.VEvent) (time.Time, bool, string, error) {
	return parseDTProp(ev, ical.ComponentPropertyDtStart)
}

// parseDTProp parses a DATE or DATE-TIME property from a VEVENT.
// Returns (time, isAllDay, timezone, error). The timezone is a real IANA name
// derived from the TZID parameter, the trailing-Z UTC marker, or "UTC" as a
// last resort — never the literal string "import".
func parseDTProp(ev *ical.VEvent, prop ical.ComponentProperty) (time.Time, bool, string, error) {
	p := ev.GetProperty(prop)
	if p == nil {
		return time.Time{}, false, "", fmt.Errorf("property %s missing", prop)
	}
	val := strings.TrimSpace(p.Value)

	// DATE-only: YYYYMMDD
	if len(val) == 8 {
		t, err := time.ParseInLocation("20060102", val, time.Local)
		return t, true, "UTC", err
	}

	// DATE-TIME with explicit timezone via TZID parameter
	if tzid := paramValue(p, "TZID"); tzid != "" {
		loc, err := time.LoadLocation(tzid)
		if err != nil {
			loc = time.Local
		}
		t, err := time.ParseInLocation("20060102T150405", strings.TrimSuffix(val, "Z"), loc)
		return t, false, tzid, err
	}

	// DATE-TIME UTC (trailing Z)
	if strings.HasSuffix(val, "Z") {
		t, err := time.Parse("20060102T150405Z", val)
		if err == nil {
			t = t.In(time.Local)
		}
		return t, false, "UTC", err
	}

	// DATE-TIME local (no Z, no TZID)
	t, err := time.ParseInLocation("20060102T150405", val, time.Local)
	return t, false, "UTC", err
}

// alarmReminder returns the reminder offset in minutes-before-start if the
// VEVENT contains a VALARM with a parseable negative duration trigger
// (e.g. "-PT15M"). It returns nil if there is no VALARM or the trigger cannot
// be parsed — imported events never get a default reminder.
func alarmReminder(ev *ical.VEvent) *int {
	for _, sub := range ev.SubComponents() {
		alarm, ok := sub.(*ical.VAlarm)
		if !ok {
			continue
		}
		tp := alarm.GetProperty(ical.ComponentPropertyTrigger)
		if tp == nil {
			continue
		}
		// Only relative-duration triggers are supported (VALUE=DATE-TIME
		// triggers are absolute and rarer); skip anything that isn't one.
		if v := paramValue(tp, "VALUE"); v != "" && !strings.EqualFold(v, "DURATION") {
			continue
		}
		if mins, ok := triggerMinutes(strings.TrimSpace(tp.Value)); ok {
			return &mins
		}
	}
	return nil
}

// triggerMinutes parses an iCal duration trigger like "-PT15M", "-PT1H",
// "-P1D" into minutes-before-start. Negative (before) durations yield a
// positive minute count; non-negative durations are treated as 0. Returns
// (minutes, true) on success.
func triggerMinutes(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	// Only "before start" (negative) triggers map to a minutes-before reminder.
	// A trigger at or after the start time is not a reminder we model, so it
	// yields no reminder rather than one that fires at start.
	if !neg {
		return 0, false
	}
	if len(s) == 0 || s[0] != 'P' {
		return 0, false
	}
	d, err := time.ParseDuration(durToGo(s))
	if err != nil {
		return 0, false
	}
	return int(d.Minutes()), true
}

// durToGo converts the numeric body of an iCal duration (without sign, starting
// at 'P', e.g. "PT15M", "P1DT2H") into a Go duration string ("15m", "26h"). It
// supports weeks, days, hours, minutes, and seconds. Returns "" on malformed
// input, which makes time.ParseDuration fail upstream.
func durToGo(s string) string {
	// strip leading P
	s = strings.TrimPrefix(s, "P")
	var b strings.Builder
	inTime := false
	num := ""
	flush := func(unit string) bool {
		if num == "" {
			return false
		}
		b.WriteString(num)
		b.WriteString(unit)
		num = ""
		return true
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			num += string(c)
		case c == 'T':
			inTime = true
		case c == 'W':
			// weeks → hours (Go has no week unit)
			if num == "" {
				return ""
			}
			var w int
			fmt.Sscanf(num, "%d", &w)
			fmt.Fprintf(&b, "%dh", w*7*24)
			num = ""
		case c == 'D':
			if num == "" {
				return ""
			}
			var d int
			fmt.Sscanf(num, "%d", &d)
			fmt.Fprintf(&b, "%dh", d*24)
			num = ""
		case c == 'H' && inTime:
			if !flush("h") {
				return ""
			}
		case c == 'M' && inTime:
			if !flush("m") {
				return ""
			}
		case c == 'S' && inTime:
			if !flush("s") {
				return ""
			}
		default:
			return ""
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String()
}

func paramValue(p *ical.IANAProperty, name string) string {
	if p == nil {
		return ""
	}
	if vals, ok := p.ICalParameters[name]; ok && len(vals) > 0 {
		return vals[0]
	}
	return ""
}
