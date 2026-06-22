package ui

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"pycalendar/internal/api"
	"pycalendar/internal/syncer"
)

// ShowAlertsWindow opens a standalone window listing pending sync conflicts.
// Used by the tray badge.
func ShowAlertsWindow() {
	a := getFyneApp()
	w := a.NewWindow("Sync Conflicts")
	w.Resize(fyne.NewSize(560, 480))
	w.SetContent(buildAlertsTab())
	w.Show()
	w.RequestFocus()
}

// buildAlertsTab renders the Conflict Queue: each unresolved conflict with a
// local-vs-remote summary and Keep local / Accept remote actions (ADR-0007).
func buildAlertsTab() fyne.CanvasObject {
	listBox := container.NewVBox()

	var rebuild func()
	rebuild = func() {
		listBox.Objects = nil

		conflicts, err := syncer.GetAllPendingConflicts()
		if err != nil {
			listBox.Add(widget.NewLabel("Failed to load conflicts: " + err.Error()))
			listBox.Refresh()
			return
		}
		if len(conflicts) == 0 {
			listBox.Add(widget.NewLabel("No sync conflicts."))
			listBox.Refresh()
			return
		}

		cals, _ := api.GetCalendars()
		calNames := calendarMap(cals)
		for _, conf := range conflicts {
			listBox.Add(buildConflictCard(conf, calNames, rebuild))
			listBox.Add(widget.NewSeparator())
		}
		listBox.Refresh()
	}
	rebuild()

	return container.NewVScroll(listBox)
}

func buildConflictCard(c syncer.Conflict, calNames map[int64]api.Calendar, onResolve func()) fyne.CanvasObject {
	calName := "Calendar"
	if cal, ok := calNames[c.CalendarID]; ok {
		calName = cal.Name
	}

	header := widget.NewLabelWithStyle(
		fmt.Sprintf("%s — detected %s", calName, c.DetectedAt.Format("Jan 2 15:04")),
		fyne.TextAlignLeading, fyne.TextStyle{Bold: true},
	)
	localLbl := widget.NewLabel("Local:   " + eventSummary(c.LocalJSON))
	remoteLbl := widget.NewLabel("Remote:  " + eventSummary(c.RemoteJSON))

	finish := func() {
		onResolve()
		RefreshTrayBadge()
		if calendarWindowReload != nil {
			calendarWindowReload()
		}
	}

	keepBtn := widget.NewButton("Keep local", func() {
		if err := syncer.ResolveKeepLocal(c); err != nil {
			slog.Warn("resolve keep-local failed", "id", c.ID, "err", err)
		}
		finish()
	})
	acceptBtn := widget.NewButton("Accept remote", func() {
		if err := syncer.ResolveAcceptRemote(c); err != nil {
			slog.Warn("resolve accept-remote failed", "id", c.ID, "err", err)
		}
		finish()
	})
	acceptBtn.Importance = widget.HighImportance

	return container.NewVBox(header, localLbl, remoteLbl, container.NewHBox(keepBtn, acceptBtn))
}

// eventSummary renders a one-line description of a conflict side from its JSON
// blob, tolerating an unparseable payload.
func eventSummary(js string) string {
	var e api.Event
	if err := json.Unmarshal([]byte(js), &e); err != nil {
		return "(unreadable)"
	}
	s := e.Title
	if s == "" {
		s = "(untitled)"
	}
	if e.StartTS > 0 {
		s += "  " + time.Unix(e.StartTS, 0).Format("Jan 2 15:04")
	}
	if e.Location.Valid && e.Location.String != "" {
		s += "  @ " + e.Location.String
	}
	return s
}
