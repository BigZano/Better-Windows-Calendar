// Package icsimport parses iCalendar (.ics) files and bulk-inserts the events
// into the local database via the api layer.
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

// Result summarises an import run.
type Result struct {
	Imported int
	Skipped  int
	Errors   []string
}

// Import reads an iCalendar stream and creates events in the given calendar.
// All-day events and timed events are both supported. RRULE strings are
// preserved as-is (they are already in iCal format). Returns a Result
// describing how many events were imported, skipped, or failed.
func Import(r io.Reader, calendarID int64) (Result, error) {
	cal, err := ical.ParseCalendar(r)
	if err != nil {
		return Result{}, fmt.Errorf("parse ics: %w", err)
	}

	var res Result
	for _, comp := range cal.Components {
		ev, ok := comp.(*ical.VEvent)
		if !ok {
			continue
		}

		title := propText(ev, ical.ComponentPropertySummary)
		if title == "" {
			res.Skipped++
			continue
		}

		startTime, allDay, err := parseDTStart(ev)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("DTSTART for %q: %v", title, err))
			res.Skipped++
			continue
		}

		var endTime *time.Time
		if et, _, err := parseDTProp(ev, ical.ComponentPropertyDtEnd); err == nil {
			endTime = &et
		}

		notes := propText(ev, ical.ComponentPropertyDescription)
		location := propText(ev, ical.ComponentPropertyLocation)
		url := propText(ev, ical.ComponentPropertyUrl)
		rrule := propText(ev, ical.ComponentPropertyRrule)

		id, err := api.CreateEvent(
			title, startTime, endTime,
			notes, nil,
			rrule, allDay,
			"import", calendarID,
			location, url,
		)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("create %q: %v", title, err))
			res.Skipped++
			continue
		}
		slog.Info("ics import: created event", "id", id, "title", title)
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

// parseDTStart extracts the start time and all-day flag from a VEVENT.
func parseDTStart(ev *ical.VEvent) (time.Time, bool, error) {
	return parseDTProp(ev, ical.ComponentPropertyDtStart)
}

// parseDTProp parses a DATE or DATE-TIME property from a VEVENT.
// Returns (time, isAllDay, error).
func parseDTProp(ev *ical.VEvent, prop ical.ComponentProperty) (time.Time, bool, error) {
	p := ev.GetProperty(prop)
	if p == nil {
		return time.Time{}, false, fmt.Errorf("property %s missing", prop)
	}
	val := strings.TrimSpace(p.Value)

	// DATE-only: YYYYMMDD
	if len(val) == 8 {
		t, err := time.ParseInLocation("20060102", val, time.Local)
		return t, true, err
	}

	// DATE-TIME with explicit timezone via TZID parameter
	if tzid := paramValue(p, "TZID"); tzid != "" {
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

func paramValue(p *ical.IANAProperty, name string) string {
	if p == nil {
		return ""
	}
	for _, param := range p.ICalParameters {
		for _, v := range param {
			return v
		}
	}
	_ = name
	return ""
}
