package api

import (
	"database/sql"
	"fmt"
	"time"

	rrulelib "github.com/teambition/rrule-go"
)

// AddUntilToRRule parses rruleStr and returns it with UNTIL set to one second
// before cutoffTS. Used when "this and following" caps the master's series.
func AddUntilToRRule(rruleStr string, cutoffTS int64) (string, error) {
	r, err := rrulelib.StrToRRule(rruleStr)
	if err != nil {
		return "", fmt.Errorf("parse rrule: %w", err)
	}
	until := time.Unix(cutoffTS, 0).Add(-time.Second).UTC()
	r.Until(until) // updates OrigOptions and rebuilds
	return r.OrigOptions.RRuleString(), nil
}

// DeleteExceptionsForMaster removes all exception rows (child events) for masterID.
func DeleteExceptionsForMaster(masterID int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}

	_, err = db.Exec(`DELETE FROM events WHERE parent_event_id = ?`, masterID)
	return err
}

// DeleteExceptionsOnOrAfter removes exception rows for masterID with start_ts >= fromTS.
func DeleteExceptionsOnOrAfter(masterID, fromTS int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}

	_, err = db.Exec(
		`DELETE FROM events WHERE parent_event_id = ? AND start_ts >= ?`,
		masterID, fromTS,
	)
	return err
}

// CreateExceptionEvent inserts a new exception row for a recurring series.
// The row has parent_event_id = masterID and no recurrence_rule, overriding
// the virtual occurrence that would otherwise appear on the same day.
func CreateExceptionEvent(masterID int64, title string, startTime time.Time,
	endTime *time.Time, notes, location, url string,
	allDay bool, calendarID int64, reminderMinutes int) (int64, error) {

	db, err := openDB()
	if err != nil {
		return 0, err
	}


	now := time.Now().Unix()
	startTS := startTime.Unix()

	var endTS sql.NullInt64
	if endTime != nil {
		endTS = sql.NullInt64{Int64: endTime.Unix(), Valid: true}
	}
	reminderTS := sql.NullInt64{Int64: startTS - int64(reminderMinutes*60), Valid: true}

	toNullStr := func(s string) sql.NullString {
		return sql.NullString{String: s, Valid: s != ""}
	}
	allDayInt := 0
	if allDay {
		allDayInt = 1
	}

	res, err := db.Exec(`
		INSERT INTO events
			(title, start_ts, end_ts, timezone, notes, reminder_ts, created_ts, updated_ts,
			 recurrence_rule, all_day, calendar_id, location, url, parent_event_id)
		VALUES (?, ?, ?, '', ?, ?, ?, ?,
		        NULL, ?, ?, ?, ?, ?)`,
		title, startTS, endTS,
		toNullStr(notes), reminderTS, now, now,
		allDayInt, calendarID, toNullStr(location), toNullStr(url),
		masterID,
	)
	if err != nil {
		return 0, fmt.Errorf("create exception event: %w", err)
	}
	return res.LastInsertId()
}

// CloneRRuleForNewSeries returns an RRULE string suitable for a new master
// starting at newStart, inheriting the recurrence pattern from rruleStr but
// with DTSTART-dependent fields reset. COUNT is cleared; UNTIL is preserved
// only if it is after newStart.
func CloneRRuleForNewSeries(rruleStr string, newStart time.Time) (string, error) {
	r, err := rrulelib.StrToRRule(rruleStr)
	if err != nil {
		return "", fmt.Errorf("parse rrule: %w", err)
	}
	r.DTStart(newStart.UTC())
	// If the original had an UNTIL that is now before the new start, clear it.
	if !r.OrigOptions.Until.IsZero() && r.OrigOptions.Until.Before(newStart.UTC()) {
		r.OrigOptions.Until = time.Time{}
	}
	return r.OrigOptions.RRuleString(), nil
}
