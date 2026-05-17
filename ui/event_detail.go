package ui

import (
	"fmt"
	"image/color"
	"log/slog"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"pycalendar/internal/api"
)

// ShowEventDetailWindow opens an edit window for an existing event.
// If e is a virtual recurring occurrence (ID=0), the master is fetched first.
// onSave is called after a successful save.
func ShowEventDetailWindow(e api.Event, onSave func()) {
	// Virtual occurrence: ID=0, ParentEventID points to master.
	if e.ID == 0 && e.ParentEventID.Valid {
		master, err := api.GetEvent(e.ParentEventID.Int64)
		if err == nil {
			e = master
		}
	}

	a := getFyneApp()
	w := a.NewWindow("Edit Event — " + e.Title)
	w.Resize(fyne.NewSize(460, 520))

	cals, _ := api.GetCalendars()
	calNames := make([]string, len(cals))
	calIDs := make([]int64, len(cals))
	selectedCalIdx := 0
	for i, c := range cals {
		calNames[i] = c.Name
		calIDs[i] = c.ID
		if e.CalendarID.Valid && c.ID == e.CalendarID.Int64 {
			selectedCalIdx = i
		}
	}
	if len(calNames) == 0 {
		calNames = []string{"Local"}
		calIDs = []int64{1}
	}

	titleEntry := widget.NewEntry()
	titleEntry.SetText(e.Title)

	startEntry := widget.NewEntry()
	startEntry.SetText(time.Unix(e.StartTS, 0).Format("2006-01-02 15:04"))

	endEntry := widget.NewEntry()
	endEntry.SetPlaceHolder("YYYY-MM-DD HH:MM (optional)")
	if e.EndTS.Valid {
		endEntry.SetText(time.Unix(e.EndTS.Int64, 0).Format("2006-01-02 15:04"))
	}

	allDayCheck := widget.NewCheck("All-day event", nil)
	allDayCheck.SetChecked(e.AllDay)

	notesEntry := widget.NewMultiLineEntry()
	notesEntry.SetMinRowsVisible(3)
	if e.Notes.Valid {
		notesEntry.SetText(e.Notes.String)
	}

	locationEntry := widget.NewEntry()
	locationEntry.SetPlaceHolder("Location (optional)")
	if e.Location.Valid {
		locationEntry.SetText(e.Location.String)
	}

	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("URL (optional)")
	if e.URL.Valid {
		urlEntry.SetText(e.URL.String)
	}

	reminderEntry := widget.NewEntry()
	if e.ReminderTS.Valid {
		offsetMin := max(int((e.StartTS-e.ReminderTS.Int64)/60), 0)
		reminderEntry.SetText(strconv.Itoa(offsetMin))
	} else {
		reminderEntry.SetText("15")
	}

	calSelect := widget.NewSelect(calNames, nil)
	calSelect.SetSelectedIndex(selectedCalIdx)

	// Pre-check existing categories for this event.
	existingCats, _ := api.GetEventCategories(e.ID)
	checkedCats := make(map[int64]bool, len(existingCats))
	for _, c := range existingCats {
		checkedCats[c.ID] = true
	}
	catChecks, catIDs, catWidget := buildCategoryChecklist(checkedCats)

	errorLabel := canvas.NewText("", color.RGBA{R: 200, A: 255})

	saveBtn := widget.NewButton("Save", func() {
		title := titleEntry.Text
		if title == "" {
			errorLabel.Text = "Title is required"
			errorLabel.Refresh()
			return
		}

		startTime, err := time.ParseInLocation("2006-01-02 15:04", startEntry.Text, time.Local)
		if err != nil {
			errorLabel.Text = "Invalid start (YYYY-MM-DD HH:MM)"
			errorLabel.Refresh()
			return
		}

		fields := map[string]any{
			"title":    title,
			"start_ts": startTime.Unix(),
			"notes":    nullableString(notesEntry.Text),
			"location": nullableString(locationEntry.Text),
			"url":      nullableString(urlEntry.Text),
			"all_day":  boolToIntVal(allDayCheck.Checked),
		}

		if endEntry.Text != "" {
			endTime, err := time.ParseInLocation("2006-01-02 15:04", endEntry.Text, time.Local)
			if err != nil {
				errorLabel.Text = "Invalid end time (YYYY-MM-DD HH:MM)"
				errorLabel.Refresh()
				return
			}
			fields["end_ts"] = endTime.Unix()
		}

		if calSelect.SelectedIndex() >= 0 && calSelect.SelectedIndex() < len(calIDs) {
			fields["calendar_id"] = calIDs[calSelect.SelectedIndex()]
		}

		if reminderEntry.Text != "" {
			mins, err := strconv.Atoi(reminderEntry.Text)
			if err != nil || mins < 0 {
				errorLabel.Text = "Reminder must be a non-negative integer"
				errorLabel.Refresh()
				return
			}
			fields["reminder_ts"] = startTime.Unix() - int64(mins*60)
		}

		if err := api.UpdateEvent(e.ID, fields); err != nil {
			errorLabel.Text = "Save failed: " + err.Error()
			errorLabel.Refresh()
			slog.Error("update event failed", "id", e.ID, "err", err)
			return
		}

		var selectedCats []int64
		for i, ch := range catChecks {
			if ch.Checked {
				selectedCats = append(selectedCats, catIDs[i])
			}
		}
		if err := api.SetEventCategories(e.ID, selectedCats); err != nil {
			slog.Error("set event categories failed", "id", e.ID, "err", err)
		}

		slog.Info("event updated via UI", "id", e.ID)
		w.Close()
		if onSave != nil {
			onSave()
		}
	})

	deleteBtn := widget.NewButton("Delete", func() {
		confirmDeleteEvent(e, w, func() {
			w.Close()
			if onSave != nil {
				onSave()
			}
		})
	})
	deleteBtn.Importance = widget.DangerImportance

	cancelBtn := widget.NewButton("Cancel", func() { w.Close() })

	formItems := []fyne.CanvasObject{
		formRow("Title:", titleEntry),
		formRow("Start (YYYY-MM-DD HH:MM):", startEntry),
		formRow("End (optional):", endEntry),
		allDayCheck,
		formRow("Notes:", notesEntry),
		formRow("Location:", locationEntry),
		formRow("URL:", urlEntry),
		formRow("Reminder (min before):", reminderEntry),
		formRow("Calendar:", calSelect),
		formRow("Categories:", catWidget),
	}
	if e.RecurrenceRule.Valid && e.RecurrenceRule.String != "" {
		recurrLbl := widget.NewLabel("Repeats: " + friendlyRRule(e.RecurrenceRule.String))
		recurrLbl.TextStyle = fyne.TextStyle{Italic: true}
		note := widget.NewLabel("Edits apply to all occurrences. Per-occurrence editing coming in a future update.")
		note.Wrapping = fyne.TextWrapWord
		note.TextStyle = fyne.TextStyle{Italic: true}
		formItems = append(formItems, recurrLbl, note)
	}
	formItems = append(formItems, errorLabel, container.NewHBox(saveBtn, deleteBtn, cancelBtn))
	form := container.NewVBox(formItems...)

	w.SetContent(container.NewVScroll(form))
	w.Show()
}

func confirmDeleteEvent(e api.Event, _ fyne.Window, onDone func()) {
	// Inline confirm: open a small dialog window since we may not have a parent dialog.
	a := getFyneApp()
	dw := a.NewWindow(fmt.Sprintf("Delete \"%s\"?", e.Title))
	dw.Resize(fyne.NewSize(300, 100))

	msg := widget.NewLabel(fmt.Sprintf("Delete \"%s\"? This cannot be undone.", e.Title))
	msg.Wrapping = fyne.TextWrapWord

	yesBtn := widget.NewButton("Delete", func() {
		if err := api.DeleteEvent(e.ID); err != nil {
			slog.Error("delete event failed", "id", e.ID, "err", err)
		}
		dw.Close()
		if onDone != nil {
			onDone()
		}
	})
	yesBtn.Importance = widget.DangerImportance
	noBtn := widget.NewButton("Cancel", func() { dw.Close() })

	dw.SetContent(container.NewVBox(msg, container.NewHBox(yesBtn, noBtn)))
	dw.Show()
}

func formRow(label string, w fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(widget.NewLabel(label), w)
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToIntVal(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ShowPopOutWindow opens a secondary window listing all events in a crowded BSP slot.
// Closes on Escape.
func ShowPopOutWindow(events []api.Event, cals map[int64]api.Calendar) {
	a := getFyneApp()
	w := a.NewWindow("Events in this slot")
	w.Resize(fyne.NewSize(380, 300))

	box := container.NewVBox()
	for _, e := range events {
		startStr := time.Unix(e.StartTS, 0).Format("15:04")
		timeStr := startStr
		if e.EndTS.Valid {
			timeStr += " – " + time.Unix(e.EndTS.Int64, 0).Format("15:04")
		}

		dotColor := parseHexColor("#3B82F6")
		if e.CalendarID.Valid {
			if cal, ok := cals[e.CalendarID.Int64]; ok {
				dotColor = parseHexColor(cal.Color)
			}
		}
		dot := canvas.NewRectangle(dotColor)
		dot.SetMinSize(fyne.NewSize(12, 12))

		lbl := widget.NewLabel(timeStr + "  " + e.Title)
		box.Add(container.NewHBox(dot, lbl))
	}

	w.SetContent(container.NewVScroll(box))
	w.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		if ev.Name == fyne.KeyEscape {
			w.Close()
		}
	})
	w.Show()
}

