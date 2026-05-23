package ui

import (
	"fmt"
	"image/color"
	"log/slog"
	"net/url"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"pycalendar/internal/api"
)

// ShowEventDetailWindow opens an edit window for an existing event.
// Virtual recurring occurrences (ID=0) are shown with a scope selector so
// the user can apply changes to just this occurrence, this and following, or all.
// onSave is called after a successful save.
func ShowEventDetailWindow(e api.Event, onSave func()) {
	// For virtual occurrences, fetch the master for display but track the
	// occurrence's original timestamp so the scope dialog can target it.
	var occurrenceTS int64
	isvirtual := e.ID == 0 && e.ParentEventID.Valid
	if isvirtual {
		occurrenceTS = e.StartTS
		master, err := api.GetEvent(e.ParentEventID.Int64)
		if err != nil {
			slog.Error("fetch master for virtual occurrence", "parent", e.ParentEventID.Int64, "err", err)
			// Fall back: treat as non-recurring single event for editing.
			isvirtual = false
		} else {
			e = master
		}
	}

	a := getFyneApp()
	w := a.NewWindow("Edit Event — " + e.Title)
	w.Resize(fyne.NewSize(460, 680))

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

	// ---- Attachments section ----
	existingAttachments, _ := api.GetAttachments(e.ID)

	attachErrorLabel := canvas.NewText("", color.RGBA{R: 200, A: 255})

	// The live list of attachment rows rendered in the form.
	attachList := container.NewVBox()

	var rebuildAttachList func([]api.Attachment)
	rebuildAttachList = func(atts []api.Attachment) {
		attachList.Objects = nil
		for _, a := range atts {
			att := a
			displayText := att.URL
			if att.Label != "" {
				displayText = att.Label + " — " + att.URL
			}
			lnk := widget.NewHyperlink(displayText, mustParseURL(att.URL))
			removeBtn := widget.NewButton("Remove", func() {
				if err := api.DeleteAttachment(att.ID); err != nil {
					slog.Error("delete attachment", "id", att.ID, "err", err)
					attachErrorLabel.Text = "Remove failed: " + err.Error()
					attachErrorLabel.Refresh()
					return
				}
				updated, _ := api.GetAttachments(e.ID)
				rebuildAttachList(updated)
			})
			removeBtn.Importance = widget.DangerImportance
			attachList.Add(container.NewHBox(lnk, removeBtn))
		}
		attachList.Refresh()
	}
	rebuildAttachList(existingAttachments)

	newLinkURLEntry := widget.NewEntry()
	newLinkURLEntry.SetPlaceHolder("https://example.com/meeting")
	newLinkLabelEntry := widget.NewEntry()
	newLinkLabelEntry.SetPlaceHolder("Label (optional)")

	addLinkBtn := widget.NewButton("Add Link", func() {
		rawURL := newLinkURLEntry.Text
		label := newLinkLabelEntry.Text
		if _, err := api.AddAttachment(e.ID, label, rawURL); err != nil {
			attachErrorLabel.Text = err.Error()
			attachErrorLabel.Refresh()
			return
		}
		attachErrorLabel.Text = ""
		attachErrorLabel.Refresh()
		newLinkURLEntry.SetText("")
		newLinkLabelEntry.SetText("")
		updated, _ := api.GetAttachments(e.ID)
		rebuildAttachList(updated)
	})

	attachSection := container.NewVBox(
		widget.NewLabelWithStyle("Attachments", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		attachList,
		attachErrorLabel,
		formRow("URL:", newLinkURLEntry),
		formRow("Label:", newLinkLabelEntry),
		addLinkBtn,
	)

	// ---- end Attachments section ----

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

	collectFields := func() (map[string]any, []int64, int, bool) {
		title := titleEntry.Text
		if title == "" {
			errorLabel.Text = "Title is required"
			errorLabel.Refresh()
			return nil, nil, 0, false
		}
		startTime, err := time.ParseInLocation("2006-01-02 15:04", startEntry.Text, time.Local)
		if err != nil {
			errorLabel.Text = "Invalid start (YYYY-MM-DD HH:MM)"
			errorLabel.Refresh()
			return nil, nil, 0, false
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
				return nil, nil, 0, false
			}
			fields["end_ts"] = endTime.Unix()
		}
		if calSelect.SelectedIndex() >= 0 && calSelect.SelectedIndex() < len(calIDs) {
			fields["calendar_id"] = calIDs[calSelect.SelectedIndex()]
		}
		reminderMins := 15
		if reminderEntry.Text != "" {
			mins, err := strconv.Atoi(reminderEntry.Text)
			if err != nil || mins < 0 {
				errorLabel.Text = "Reminder must be a non-negative integer"
				errorLabel.Refresh()
				return nil, nil, 0, false
			}
			fields["reminder_ts"] = startTime.Unix() - int64(mins*60)
			reminderMins = mins
		}
		var selectedCats []int64
		for i, ch := range catChecks {
			if ch.Checked {
				selectedCats = append(selectedCats, catIDs[i])
			}
		}
		return fields, selectedCats, reminderMins, true
	}

	saveBtn := widget.NewButton("Save", func() {
		fields, selectedCats, reminderMins, ok := collectFields()
		if !ok {
			return
		}

		if isvirtual && e.RecurrenceRule.Valid {
			// Show 3-option scope dialog for virtual recurring occurrences.
			showRecurrenceScopeDialog(e, fields, selectedCats, occurrenceTS, reminderMins, w, onSave)
			return
		}

		// Non-virtual or non-recurring: update master directly.
		if err := api.UpdateEvent(e.ID, fields); err != nil {
			errorLabel.Text = "Save failed: " + err.Error()
			errorLabel.Refresh()
			slog.Error("update event failed", "id", e.ID, "err", err)
			return
		}
		if err := api.SetEventCategories(e.ID, selectedCats); err != nil {
			slog.Error("set event categories failed", "id", e.ID, "err", err)
		}
		// Delete stale exceptions when editing master directly so they don't shadow the update.
		if e.RecurrenceRule.Valid {
			if err := api.DeleteExceptionsForMaster(e.ID); err != nil {
				slog.Warn("delete exceptions after master edit", "id", e.ID, "err", err)
			}
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
		attachSection,
		formRow("Reminder (min before):", reminderEntry),
		formRow("Calendar:", calSelect),
		formRow("Categories:", catWidget),
	}
	if e.RecurrenceRule.Valid && e.RecurrenceRule.String != "" {
		recurrLbl := widget.NewLabel("Repeats: " + friendlyRRule(e.RecurrenceRule.String))
		recurrLbl.TextStyle = fyne.TextStyle{Italic: true}
		var scopeNote string
		if isvirtual {
			scopeNote = "Saving will ask whether to edit just this occurrence, this and following, or all."
		} else {
			scopeNote = "Saving edits all occurrences and clears exceptions. Click a specific occurrence to edit just that one."
		}
		note := widget.NewLabel(scopeNote)
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

// mustParseURL parses a URL string for use with widget.NewHyperlink.
// If parsing fails it falls back to an empty URL so the UI still renders.
func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		return &url.URL{}
	}
	return u
}

// showRecurrenceScopeDialog presents a 3-choice dialog for editing a recurring
// occurrence: just this one, this and following, or all events.
func showRecurrenceScopeDialog(master api.Event, fields map[string]any,
	selectedCats []int64, occurrenceTS int64, reminderMins int,
	parentWindow fyne.Window, onSave func()) {

	a := getFyneApp()
	dw := a.NewWindow("Edit Recurring Event")
	dw.Resize(fyne.NewSize(380, 180))

	msg := widget.NewLabel("This is a recurring event. Which occurrences should be changed?")
	msg.Wrapping = fyne.TextWrapWord

	applyAndClose := func(applyFn func() error) {
		if err := applyFn(); err != nil {
			slog.Error("apply recurring edit", "err", err)
			return
		}
		dw.Close()
		parentWindow.Close()
		if onSave != nil {
			onSave()
		}
	}

	justThisBtn := widget.NewButton("Just this occurrence", func() {
		applyAndClose(func() error {
			return applyJustThis(master, fields, occurrenceTS, selectedCats, reminderMins)
		})
	})
	thisAndFollowingBtn := widget.NewButton("This and following", func() {
		applyAndClose(func() error {
			return applyThisAndFollowing(master, fields, occurrenceTS, selectedCats, reminderMins)
		})
	})
	allEventsBtn := widget.NewButton("All events", func() {
		applyAndClose(func() error {
			return applyAllEvents(master, fields, selectedCats)
		})
	})
	cancelBtn := widget.NewButton("Cancel", func() { dw.Close() })

	dw.SetContent(container.NewVBox(
		msg,
		container.NewHBox(justThisBtn, thisAndFollowingBtn, allEventsBtn, cancelBtn),
	))
	dw.Show()
}

// applyJustThis creates a new exception row for the single occurrence at occurrenceTS,
// leaving the master and other occurrences unchanged.
func applyJustThis(master api.Event, fields map[string]any, occurrenceTS int64, cats []int64, reminderMins int) error {
	title, _ := fields["title"].(string)
	startTS, _ := fields["start_ts"].(int64)
	notes := stringFromField(fields, "notes")
	location := stringFromField(fields, "location")
	urlStr := stringFromField(fields, "url")
	allDay := fields["all_day"] == 1
	calID := master.CalendarID.Int64
	if v, ok := fields["calendar_id"].(int64); ok {
		calID = v
	}
	startTime := time.Unix(startTS, 0)
	var endTime *time.Time
	if v, ok := fields["end_ts"].(int64); ok {
		t := time.Unix(v, 0)
		endTime = &t
	}

	id, err := api.CreateExceptionEvent(master.ID, title, startTime, endTime,
		notes, location, urlStr, allDay, calID, reminderMins)
	if err != nil {
		return err
	}
	if len(cats) > 0 {
		_ = api.SetEventCategories(id, cats)
	}
	_ = occurrenceTS // exception suppresses the virtual occurrence via day-based matching
	return nil
}

// applyThisAndFollowing caps the master's series before occurrenceTS and creates a
// new independent master for the edited occurrence onward.
func applyThisAndFollowing(master api.Event, fields map[string]any, occurrenceTS int64, cats []int64, reminderMins int) error {
	// Cap the master.
	cappedRule, err := api.AddUntilToRRule(master.RecurrenceRule.String, occurrenceTS)
	if err != nil {
		return err
	}
	if err := api.UpdateEvent(master.ID, map[string]any{"recurrence_rule": cappedRule}); err != nil {
		return err
	}
	// Remove exceptions on or after the split point.
	if err := api.DeleteExceptionsOnOrAfter(master.ID, occurrenceTS); err != nil {
		return err
	}

	// Build new master fields.
	title, _ := fields["title"].(string)
	startTS, _ := fields["start_ts"].(int64)
	notes := stringFromField(fields, "notes")
	location := stringFromField(fields, "location")
	urlStr := stringFromField(fields, "url")
	allDay := fields["all_day"] == 1
	calID := master.CalendarID.Int64
	if v, ok := fields["calendar_id"].(int64); ok {
		calID = v
	}

	startTime := time.Unix(startTS, 0)
	var endTime *time.Time
	if v, ok := fields["end_ts"].(int64); ok {
		t := time.Unix(v, 0)
		endTime = &t
	}

	newRule, err := api.CloneRRuleForNewSeries(master.RecurrenceRule.String, startTime)
	if err != nil {
		newRule = master.RecurrenceRule.String
	}

	newID, err := api.CreateEvent(title, startTime, endTime, notes, &reminderMins,
		newRule, allDay, master.Timezone, calID, location, urlStr)
	if err != nil {
		return err
	}
	if len(cats) > 0 {
		_ = api.SetEventCategories(newID, cats)
	}
	return nil
}

// applyAllEvents updates the master in place and deletes all exceptions.
func applyAllEvents(master api.Event, fields map[string]any, cats []int64) error {
	if err := api.UpdateEvent(master.ID, fields); err != nil {
		return err
	}
	if err := api.DeleteExceptionsForMaster(master.ID); err != nil {
		slog.Warn("delete exceptions for master", "id", master.ID, "err", err)
	}
	if err := api.SetEventCategories(master.ID, cats); err != nil {
		slog.Warn("set categories on master", "id", master.ID, "err", err)
	}
	return nil
}

func stringFromField(fields map[string]any, key string) string {
	if v, ok := fields[key].(string); ok {
		return v
	}
	return ""
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

