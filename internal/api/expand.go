package api

import (
	"database/sql"
	"sort"
	"time"

	rrulelib "github.com/teambition/rrule-go"
)

// expandEvents merges raw in-window DB events with virtual occurrences generated
// from RRULE masters. earlyMasters are recurring events whose dtstart predates
// the window but whose series may still produce occurrences within it.
//
// Virtual occurrences have ID=0 and ParentEventID pointing to their master so
// that the UI can fetch the master for editing. Full scope-dialog exception
// handling is deferred to issue #18.
func expandEvents(raw []Event, earlyMasters []Event, windowStart, windowEnd time.Time) []Event {
	// Build exception map: parentID → set of LOCAL day-start timestamps covered by exception rows.
	// Day-based matching lets users reschedule individual occurrences (e.g. 2 PM → 3 PM same day)
	// without generating a duplicate virtual occurrence for the original time.
	exDates := make(map[int64]map[int64]bool)
	for _, e := range raw {
		if e.ParentEventID.Valid {
			pid := e.ParentEventID.Int64
			if exDates[pid] == nil {
				exDates[pid] = make(map[int64]bool)
			}
			exDates[pid][localDayKey(e.StartTS)] = true
		}
	}

	result := make([]Event, 0, len(raw))
	mastersSeen := make(map[int64]bool)

	for _, e := range raw {
		result = append(result, e)
		if e.RecurrenceRule.Valid && !e.ParentEventID.Valid {
			mastersSeen[e.ID] = true
		}
	}

	// Expand both in-window masters and early masters.
	for _, e := range raw {
		if e.RecurrenceRule.Valid && !e.ParentEventID.Valid {
			result = append(result, expandMaster(e, windowStart, windowEnd, exDates)...)
		}
	}
	for _, m := range earlyMasters {
		if mastersSeen[m.ID] {
			continue
		}
		mastersSeen[m.ID] = true
		result = append(result, expandMaster(m, windowStart, windowEnd, exDates)...)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].StartTS < result[j].StartTS
	})
	return result
}

// expandMaster generates virtual occurrence Events for master within [windowStart, windowEnd].
// The dtstart occurrence is skipped because the master row already represents it.
func expandMaster(master Event, windowStart, windowEnd time.Time, exDates map[int64]map[int64]bool) []Event {
	if !master.RecurrenceRule.Valid || master.RecurrenceRule.String == "" {
		return nil
	}

	r, err := rrulelib.StrToRRule(master.RecurrenceRule.String)
	if err != nil {
		return nil
	}
	r.DTStart(time.Unix(master.StartTS, 0).UTC())

	times := r.Between(windowStart.Add(-time.Second), windowEnd, true)

	var duration int64
	if master.EndTS.Valid {
		duration = master.EndTS.Int64 - master.StartTS
	}
	var reminderOffset int64
	if master.ReminderTS.Valid {
		reminderOffset = master.StartTS - master.ReminderTS.Int64
	}

	var result []Event
	for _, t := range times {
		ts := t.Unix()
		// Master row already represents the dtstart occurrence.
		if ts == master.StartTS {
			continue
		}
		// Skip dates covered by a real exception row (day-based match).
		if m, ok := exDates[master.ID]; ok && m[localDayKey(ts)] {
			continue
		}

		occ := master
		occ.ID = 0 // virtual — not in DB; UI fetches master for editing
		occ.ParentEventID = sql.NullInt64{Int64: master.ID, Valid: true}
		occ.StartTS = ts
		if master.EndTS.Valid {
			occ.EndTS = sql.NullInt64{Int64: ts + duration, Valid: true}
		}
		if master.ReminderTS.Valid {
			occ.ReminderTS = sql.NullInt64{Int64: ts - reminderOffset, Valid: true}
		}
		result = append(result, occ)
	}
	return result
}

// localDayKey returns the Unix timestamp of the LOCAL midnight for the given ts,
// used for day-based exception matching so that rescheduled occurrences (same day,
// different time) still suppress the original virtual occurrence.
func localDayKey(ts int64) int64 {
	t := time.Unix(ts, 0).Local()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local).Unix()
}

// queryEarlyRecurringMasters returns recurring master events (no parent) whose
// dtstart is before beforeTS, so they may still have occurrences in a later window.
func queryEarlyRecurringMasters(db *sql.DB, beforeTS int64) []Event {
	rows, err := db.Query(`
		SELECT id, title, start_ts, end_ts, timezone, notes, reminder_ts,
		       created_ts, updated_ts, recurrence_rule, all_day,
		       calendar_id, location, url, parent_event_id
		FROM events
		WHERE recurrence_rule IS NOT NULL
		  AND parent_event_id IS NULL
		  AND start_ts < ?`, beforeTS)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			continue
		}
		events = append(events, e)
	}
	return events
}
