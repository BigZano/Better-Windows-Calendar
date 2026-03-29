package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"pycalendar/internal/api"
)

// waybarOutput matches the Waybar custom module JSON schema.
type waybarOutput struct {
	Text    string       `json:"text"`
	Tooltip string       `json:"tooltip"`
	Class   string       `json:"class"`
	Events  []eventEntry `json:"events"`
}

type eventEntry struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Start string `json:"start"`
	Notes string `json:"notes"`
}

// FormatText returns a human-readable bar string (emoji + pipe-separated events).
// Used for --format text.
func FormatText(events []api.Event, maxEvents int) string {
	if len(events) == 0 {
		return "No Upcoming Events"
	}

	parts := []string{"📅"}
	now := time.Now()

	for _, e := range events {
		if len(parts)-1 >= maxEvents {
			break
		}
		t := e.StartTime()
		var timeStr string
		if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
			timeStr = t.Format("15:04")
		} else {
			timeStr = t.Format("01/02")
		}
		title := e.Title
		if len(title) > 20 {
			title = title[:20]
		}
		parts = append(parts, timeStr+" "+title)
	}

	return strings.Join(parts, " | ")
}

// FormatJSON returns a Waybar-compatible JSON string.
// Used for --format json.
func FormatJSON(events []api.Event, maxEvents int) (string, error) {
	out := waybarOutput{
		Text:   FormatText(events, 3),
		Class:  "calendar",
		Events: make([]eventEntry, 0, len(events)),
	}

	var tooltipLines []string
	if len(events) > 0 {
		tooltipLines = append(tooltipLines, "Upcoming Events:")
	} else {
		out.Tooltip = "No upcoming events"
	}

	for _, e := range events {
		if len(out.Events) >= maxEvents {
			break
		}
		t := e.StartTime()
		notes := ""
		if e.Notes.Valid {
			notes = e.Notes.String
		}
		out.Events = append(out.Events, eventEntry{
			ID:    e.ID,
			Title: e.Title,
			Start: t.Format(time.RFC3339),
			Notes: notes,
		})
		tooltipLines = append(tooltipLines, fmt.Sprintf("• %s - %s", t.Format("01/02 15:04"), e.Title))
	}

	if len(tooltipLines) > 0 {
		out.Tooltip = strings.Join(tooltipLines, "\n")
	}

	b, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// FormatPolybar returns a plain-text one-liner for Polybar.
// Used for --format polybar.
func FormatPolybar(events []api.Event, maxEvents int) string {
	if len(events) == 0 {
		return "CAL: No events"
	}

	parts := []string{"CAL:"}
	now := time.Now()

	for _, e := range events {
		if len(parts)-1 >= maxEvents {
			break
		}
		t := e.StartTime()
		var timeStr string
		if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
			timeStr = t.Format("15:04")
		} else {
			timeStr = t.Format("01/02")
		}
		title := e.Title
		if len(title) > 20 {
			title = title[:20]
		}
		parts = append(parts, timeStr+" "+title)
	}

	return strings.Join(parts, " | ")
}
