package ui

import (
	"fmt"
	"image/color"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"pycalendar/internal/api"
	"pycalendar/internal/autostart"
	"pycalendar/internal/barsetup"
	"pycalendar/internal/config"
)

var fyneApp fyne.App

func getFyneApp() fyne.App {
	if fyneApp == nil {
		fyneApp = app.New()
	}
	return fyneApp
}

// onCalendarsChanged is called whenever a calendar is created, renamed, or deleted.
// Set by ShowCalendarWindow so the sidebar refreshes automatically.
var onCalendarsChanged func()

// ShowCalendarWindow opens (or brings to front) the main calendar window.
func ShowCalendarWindow() {
	a := getFyneApp()
	w := a.NewWindow("PyCalendar")
	w.Resize(fyne.NewSize(1050, 650))

	cfg, _ := config.Load()
	now := time.Now()

	dayView, dayReload := buildDayView(now, w)
	weekView, weekReload := buildWeekView(now, w)
	monthView, monthReload := buildMonthView(now.Year(), int(now.Month()), loadMonthEvents(now.Year(), int(now.Month())))

	allReload := func() {
		dayReload()
		weekReload()
		monthReload()
	}

	sidebar, sidebarRebuild := buildCalendarSidebar(allReload)
	onCalendarsChanged = func() {
		sidebarRebuild()
		allReload()
	}

	filterBar, filterRebuild := buildCategoryFilterBar(allReload)
	onCategoriesChanged = func() {
		filterRebuild()
		allReload()
	}

	tabs := container.NewAppTabs(
		container.NewTabItem("Day", dayView),
		container.NewTabItem("Week", weekView),
		container.NewTabItem("Month", monthView),
	)

	// Restore last-used view
	switch cfg.UI.DefaultView {
	case "week":
		tabs.SelectIndex(1)
	case "month":
		tabs.SelectIndex(2)
	default:
		tabs.SelectIndex(0)
	}

	// Persist tab selection
	tabs.OnSelected = func(_ *container.TabItem) {
		view := "day"
		switch tabs.SelectedIndex() {
		case 1:
			view = "week"
		case 2:
			view = "month"
		}
		go func() {
			c, err := config.Load()
			if err != nil {
				return
			}
			c.UI.DefaultView = view
			_ = config.Save(c)
		}()
	}

	addBtn := widget.NewButton("Add Event", func() {
		ShowAddEventDialog(allReload)
	})
	settingsBtn := widget.NewButton("Settings", func() {
		ShowSettingsWindow()
	})

	toolbar := container.NewHBox(addBtn, settingsBtn)
	topBar := container.NewVBox(toolbar, filterBar)
	main := container.NewBorder(topBar, nil, nil, nil, tabs)
	split := container.NewHSplit(sidebar, main)
	split.SetOffset(0.18)

	w.SetContent(split)
	w.SetCloseIntercept(func() { w.Hide() })
	w.Show()
}

// buildCalendarSidebar returns a sidebar widget listing all calendars with visibility
// toggles and a rebuild function (called when the calendar list changes).
func buildCalendarSidebar(onVisibilityChange func()) (fyne.CanvasObject, func()) {
	box := container.NewVBox()

	var rebuild func()
	rebuild = func() {
		box.Objects = nil
		cals, err := api.GetCalendars()
		if err != nil {
			slog.Error("sidebar: load calendars", "err", err)
		}
		for _, c := range cals {
			cal := c
			dot := canvas.NewRectangle(parseHexColor(cal.Color))
			dot.SetMinSize(fyne.NewSize(12, 12))

			check := widget.NewCheck("", func(visible bool) {
				setCalendarVisible(cal.ID, visible)
				if onVisibilityChange != nil {
					onVisibilityChange()
				}
			})
			check.SetChecked(isCalendarVisible(cal.ID))

			name := cal.Name
			if cal.ID == 1 {
				name += " ★"
			}
			lbl := widget.NewLabel(name)

			box.Add(container.NewHBox(check, dot, lbl))
		}
		box.Refresh()
	}
	rebuild()

	title := widget.NewLabelWithStyle("Calendars", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewBorder(title, nil, nil, nil, container.NewVScroll(box)), rebuild
}

// ---- Day view ----

func buildDayView(day time.Time, _ fyne.Window) (fyne.CanvasObject, func()) {
	cals, _ := api.GetCalendars()
	events := loadDayEvents(day)

	allDayBox := container.NewVBox()
	refreshAllDay := func(evts []api.Event) {
		allDayBox.Objects = nil
		for _, e := range allDayEventsForDay(evts, day) {
			allDayBox.Add(widget.NewLabel("▪ " + e.Title))
		}
		allDayBox.Refresh()
	}
	refreshAllDay(events)

	calMap := calendarMap(cals)
	var grid *TimeGridWidget
	grid = NewTimeGridWidget([]time.Time{day}, events, cals,
		func(e api.Event) {
			ShowEventDetailWindow(e, func() {
				newEvts := loadDayEvents(day)
				newCals, _ := api.GetCalendars()
				grid.Reload(newEvts, newCals)
				refreshAllDay(newEvts)
			})
		},
		func(group []api.Event) {
			ShowPopOutWindow(group, calMap)
		},
	)

	dateLabel := widget.NewLabel(day.Format("Monday, January 2, 2006"))

	prevBtn := widget.NewButton("◀", func() {
		day = day.AddDate(0, 0, -1)
		dateLabel.SetText(day.Format("Monday, January 2, 2006"))
		newEvts := loadDayEvents(day)
		newCals, _ := api.GetCalendars()
		grid.dates = []time.Time{day}
		grid.Reload(newEvts, newCals)
		refreshAllDay(newEvts)
	})
	nextBtn := widget.NewButton("▶", func() {
		day = day.AddDate(0, 0, 1)
		dateLabel.SetText(day.Format("Monday, January 2, 2006"))
		newEvts := loadDayEvents(day)
		newCals, _ := api.GetCalendars()
		grid.dates = []time.Time{day}
		grid.Reload(newEvts, newCals)
		refreshAllDay(newEvts)
	})
	todayBtn := widget.NewButton("Today", func() {
		day = time.Now()
		dateLabel.SetText(day.Format("Monday, January 2, 2006"))
		newEvts := loadDayEvents(day)
		newCals, _ := api.GetCalendars()
		grid.dates = []time.Time{day}
		grid.Reload(newEvts, newCals)
		refreshAllDay(newEvts)
	})

	nav := container.NewHBox(prevBtn, todayBtn, nextBtn, dateLabel)
	scroll := container.NewVScroll(grid)
	scroll.Offset = fyne.NewPos(0, hourHeight*float32(time.Now().Hour()))

	reload := func() {
		newEvts := loadDayEvents(day)
		newCals, _ := api.GetCalendars()
		grid.Reload(newEvts, newCals)
		refreshAllDay(newEvts)
	}

	top := container.NewVBox(nav, allDayBox)
	return container.NewBorder(top, nil, nil, nil, scroll), reload
}

// ---- Week view ----

func buildWeekView(anyDay time.Time, _ fyne.Window) (fyne.CanvasObject, func()) {
	weekStart := startOfWeek(anyDay)
	dates := make([]time.Time, 7)
	for i := range dates {
		dates[i] = weekStart.AddDate(0, 0, i)
	}

	cals, _ := api.GetCalendars()
	events := loadWeekEvents(weekStart)

	allDayBox := container.NewVBox()
	refreshAllDay := func(evts []api.Event) {
		allDayBox.Objects = nil
		for _, d := range dates {
			for _, e := range allDayEventsForDay(evts, d) {
				allDayBox.Add(widget.NewLabel(fmt.Sprintf("▪ %s: %s", d.Format("Mon"), e.Title)))
			}
		}
		allDayBox.Refresh()
	}
	refreshAllDay(events)

	calMap := calendarMap(cals)
	var grid *TimeGridWidget
	grid = NewTimeGridWidget(dates, events, cals,
		func(e api.Event) {
			ShowEventDetailWindow(e, func() {
				newEvts := loadWeekEvents(weekStart)
				newCals, _ := api.GetCalendars()
				grid.Reload(newEvts, newCals)
				refreshAllDay(newEvts)
			})
		},
		func(group []api.Event) {
			ShowPopOutWindow(group, calMap)
		},
	)

	weekRangeLabel := widget.NewLabel(weekRangeText(weekStart))

	prevBtn := widget.NewButton("◀", func() {
		weekStart = weekStart.AddDate(0, 0, -7)
		for i := range dates {
			dates[i] = weekStart.AddDate(0, 0, i)
		}
		weekRangeLabel.SetText(weekRangeText(weekStart))
		newEvts := loadWeekEvents(weekStart)
		newCals, _ := api.GetCalendars()
		grid.dates = append([]time.Time{}, dates...)
		grid.Reload(newEvts, newCals)
		refreshAllDay(newEvts)
	})
	nextBtn := widget.NewButton("▶", func() {
		weekStart = weekStart.AddDate(0, 0, 7)
		for i := range dates {
			dates[i] = weekStart.AddDate(0, 0, i)
		}
		weekRangeLabel.SetText(weekRangeText(weekStart))
		newEvts := loadWeekEvents(weekStart)
		newCals, _ := api.GetCalendars()
		grid.dates = append([]time.Time{}, dates...)
		grid.Reload(newEvts, newCals)
		refreshAllDay(newEvts)
	})
	todayBtn := widget.NewButton("Today", func() {
		weekStart = startOfWeek(time.Now())
		for i := range dates {
			dates[i] = weekStart.AddDate(0, 0, i)
		}
		weekRangeLabel.SetText(weekRangeText(weekStart))
		newEvts := loadWeekEvents(weekStart)
		newCals, _ := api.GetCalendars()
		grid.dates = append([]time.Time{}, dates...)
		grid.Reload(newEvts, newCals)
		refreshAllDay(newEvts)
	})

	nav := container.NewHBox(prevBtn, todayBtn, nextBtn, weekRangeLabel)
	scroll := container.NewVScroll(grid)
	scroll.Offset = fyne.NewPos(0, hourHeight*float32(time.Now().Hour()))

	reload := func() {
		newEvts := loadWeekEvents(weekStart)
		newCals, _ := api.GetCalendars()
		grid.Reload(newEvts, newCals)
		refreshAllDay(newEvts)
	}

	top := container.NewVBox(nav, allDayBox)
	return container.NewBorder(top, nil, nil, nil, scroll), reload
}

func weekRangeText(weekStart time.Time) string {
	weekEnd := weekStart.AddDate(0, 0, 6)
	return fmt.Sprintf("%s – %s", weekStart.Format("Jan 2"), weekEnd.Format("Jan 2, 2006"))
}

// ---- event loaders ----

func loadDayEvents(day time.Time) []api.Event {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, day.Location()).Unix()
	events, err := api.GetEvents(start, start+86400)
	if err != nil {
		slog.Error("load day events", "err", err)
	}
	if err := api.EnrichEventsWithCategories(events); err != nil {
		slog.Error("enrich day events categories", "err", err)
	}
	return events
}

func loadWeekEvents(weekStart time.Time) []api.Event {
	start := time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, weekStart.Location()).Unix()
	events, err := api.GetEvents(start, start+7*86400)
	if err != nil {
		slog.Error("load week events", "err", err)
	}
	if err := api.EnrichEventsWithCategories(events); err != nil {
		slog.Error("enrich week events categories", "err", err)
	}
	return events
}

func loadMonthEvents(year, month int) []api.Event {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.Local).Unix()
	end := time.Date(year, time.Month(month+1), 1, 0, 0, 0, 0, time.Local).Unix()
	events, err := api.GetEvents(start, end)
	if err != nil {
		slog.Error("load month events", "err", err)
	}
	if err := api.EnrichEventsWithCategories(events); err != nil {
		slog.Error("enrich month events categories", "err", err)
	}
	return events
}

func startOfWeek(t time.Time) time.Time {
	offset := int(t.Weekday())
	return time.Date(t.Year(), t.Month(), t.Day()-offset, 0, 0, 0, 0, t.Location())
}

func calendarMap(cals []api.Calendar) map[int64]api.Calendar {
	m := make(map[int64]api.Calendar, len(cals))
	for _, c := range cals {
		m[c.ID] = c
	}
	return m
}

// ---- Add Event dialog ----

// buildRRULE converts simple UI recurrence choices into an RFC 5545 RRULE string.
// Returns "" when freq is "None" or unrecognized.
func buildRRULE(freq string, startTime time.Time, untilDate string) string {
	if freq == "None" || freq == "" {
		return ""
	}
	var parts []string
	switch freq {
	case "Daily":
		parts = append(parts, "FREQ=DAILY")
	case "Weekly":
		days := []string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}
		parts = append(parts, "FREQ=WEEKLY", "BYDAY="+days[startTime.Weekday()])
	case "Monthly":
		parts = append(parts, "FREQ=MONTHLY")
	case "Yearly":
		parts = append(parts, "FREQ=YEARLY")
	default:
		return ""
	}
	if untilDate != "" {
		t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(untilDate), time.UTC)
		if err == nil {
			parts = append(parts, "UNTIL="+t.Format("20060102T150405Z"))
		}
	}
	return strings.Join(parts, ";")
}

// friendlyRRule converts an RFC 5545 RRULE string to a short human label.
func friendlyRRule(rrule string) string {
	upper := strings.ToUpper(rrule)
	switch {
	case strings.Contains(upper, "FREQ=DAILY"):
		return "Daily"
	case strings.Contains(upper, "FREQ=WEEKLY"):
		return "Weekly"
	case strings.Contains(upper, "FREQ=MONTHLY"):
		return "Monthly"
	case strings.Contains(upper, "FREQ=YEARLY"):
		return "Yearly"
	}
	return rrule
}

// ShowAddEventDialog opens a form for creating a new event.
func ShowAddEventDialog(onSuccess func()) {
	a := getFyneApp()
	w := a.NewWindow("Add Event")
	w.Resize(fyne.NewSize(460, 620))

	cals, _ := api.GetCalendars()
	calNames := make([]string, len(cals))
	calIDs := make([]int64, len(cals))
	for i, c := range cals {
		calNames[i] = c.Name
		calIDs[i] = c.ID
	}
	if len(calNames) == 0 {
		calNames = []string{"Local"}
		calIDs = []int64{1}
	}

	titleEntry := widget.NewEntry()
	titleEntry.SetPlaceHolder("Event title")

	defaultStart := time.Now().Add(time.Hour).Format("2006-01-02 15:04")
	startEntry := widget.NewEntry()
	startEntry.SetText(defaultStart)

	endEntry := widget.NewEntry()
	endEntry.SetPlaceHolder("YYYY-MM-DD HH:MM (optional)")

	allDayCheck := widget.NewCheck("All-day event", nil)

	notesEntry := widget.NewMultiLineEntry()
	notesEntry.SetPlaceHolder("Notes (optional)")
	notesEntry.SetMinRowsVisible(3)

	locationEntry := widget.NewEntry()
	locationEntry.SetPlaceHolder("Location (optional)")

	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("URL (optional)")

	// Single attachment link row on the Add form; more can be added via Edit.
	attachURLEntry := widget.NewEntry()
	attachURLEntry.SetPlaceHolder("https://zoom.us/j/… (optional)")
	attachLabelEntry := widget.NewEntry()
	attachLabelEntry.SetPlaceHolder("Label (optional)")

	reminderEntry := widget.NewEntry()
	reminderEntry.SetText("15")

	repeatFreqs := []string{"None", "Daily", "Weekly", "Monthly", "Yearly"}
	repeatSelect := widget.NewSelect(repeatFreqs, nil)
	repeatSelect.SetSelectedIndex(0)

	untilEntry := widget.NewEntry()
	untilEntry.SetPlaceHolder("YYYY-MM-DD (optional end date)")

	calSelect := widget.NewSelect(calNames, nil)
	calSelect.SetSelectedIndex(0)

	catChecks, catIDs, catWidget := buildCategoryChecklist(nil)

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

		var endTime *time.Time
		if endEntry.Text != "" {
			et, err := time.ParseInLocation("2006-01-02 15:04", endEntry.Text, time.Local)
			if err != nil {
				errorLabel.Text = "Invalid end time (YYYY-MM-DD HH:MM)"
				errorLabel.Refresh()
				return
			}
			endTime = &et
		}

		reminderMin := 15
		if reminderEntry.Text != "" {
			reminderMin, err = strconv.Atoi(reminderEntry.Text)
			if err != nil || reminderMin < 0 {
				errorLabel.Text = "Reminder must be a non-negative integer"
				errorLabel.Refresh()
				return
			}
		}

		calID := int64(1)
		if calSelect.SelectedIndex() >= 0 && calSelect.SelectedIndex() < len(calIDs) {
			calID = calIDs[calSelect.SelectedIndex()]
		}

		rruleStr := buildRRULE(repeatSelect.Selected, startTime, untilEntry.Text)

		// Validate attachment URL if provided before persisting the event.
		if attachURLEntry.Text != "" {
			if err := api.ValidateAttachmentURL(attachURLEntry.Text); err != nil {
				errorLabel.Text = "Invalid link URL: " + err.Error()
				errorLabel.Refresh()
				return
			}
		}

		id, err := api.CreateEvent(
			title, startTime, endTime,
			notesEntry.Text, &reminderMin,
			rruleStr, allDayCheck.Checked,
			"Local", calID,
			locationEntry.Text, urlEntry.Text,
		)
		if err != nil {
			errorLabel.Text = "Failed to save: " + err.Error()
			errorLabel.Refresh()
			slog.Error("create event failed", "err", err)
			return
		}

		var selectedCats []int64
		for i, ch := range catChecks {
			if ch.Checked {
				selectedCats = append(selectedCats, catIDs[i])
			}
		}
		if len(selectedCats) > 0 {
			if err := api.SetEventCategories(id, selectedCats); err != nil {
				slog.Error("set event categories failed", "id", id, "err", err)
			}
		}

		// Persist the optional attachment link.
		if attachURLEntry.Text != "" {
			if _, err := api.AddAttachment(id, attachLabelEntry.Text, attachURLEntry.Text); err != nil {
				slog.Error("add attachment on create", "event_id", id, "err", err)
			}
		}

		slog.Info("event created via UI", "id", id)
		w.Close()
		if onSuccess != nil {
			onSuccess()
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() { w.Close() })

	form := container.NewVBox(
		formRow("Title:", titleEntry),
		formRow("Start (YYYY-MM-DD HH:MM):", startEntry),
		formRow("End (optional):", endEntry),
		allDayCheck,
		formRow("Notes:", notesEntry),
		formRow("Location:", locationEntry),
		formRow("URL:", urlEntry),
		widget.NewLabelWithStyle("Meeting link (attachment)", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		formRow("Link URL:", attachURLEntry),
		formRow("Link label:", attachLabelEntry),
		formRow("Reminder (min before):", reminderEntry),
		formRow("Repeat:", repeatSelect),
		formRow("Repeat until (optional):", untilEntry),
		formRow("Calendar:", calSelect),
		formRow("Categories:", catWidget),
		errorLabel,
		container.NewHBox(saveBtn, cancelBtn),
	)

	w.SetContent(container.NewVScroll(form))
	w.Show()
}

// ---- Settings window ----

// ShowSettingsWindow opens the settings window with General, Notifications, Appearance,
// Calendars, and Categories tabs.
func ShowSettingsWindow() {
	a := getFyneApp()
	w := a.NewWindow("Settings")
	w.Resize(fyne.NewSize(520, 520))

	tabs := container.NewAppTabs(
		container.NewTabItem("General", buildGeneralTab()),
		container.NewTabItem("Notifications", buildNotificationsTab()),
		container.NewTabItem("Appearance", buildAppearanceTab()),
		container.NewTabItem("Calendars", buildCalendarsTab()),
		container.NewTabItem("Categories", buildCategoriesSettingsTab()),
	)
	w.SetContent(tabs)
	w.Show()
}

// ShowSettingsDialog is kept for backward compatibility (tray still calls it).
func ShowSettingsDialog(_ fyne.Window) {
	ShowSettingsWindow()
}

// ---- General tab ----

func buildGeneralTab() fyne.CanvasObject {
	cfg, err := config.Load()
	if err != nil {
		slog.Warn("settings: could not load config", "err", err)
		cfg = config.Default()
	}

	viewOptions := []string{"Day", "Week", "Month"}
	viewSelect := widget.NewSelect(viewOptions, nil)
	switch cfg.UI.DefaultView {
	case "week":
		viewSelect.SetSelected("Week")
	case "month":
		viewSelect.SetSelected("Month")
	default:
		viewSelect.SetSelected("Day")
	}

	autostartCheck := widget.NewCheck("Start with system", func(_ bool) {})
	autostartCheck.SetChecked(autostart.IsEnabled())

	errLabel := canvas.NewText("", color.RGBA{R: 200, A: 255})

	saveBtn := widget.NewButton("Save", func() {
		switch viewSelect.Selected {
		case "Week":
			cfg.UI.DefaultView = "week"
		case "Month":
			cfg.UI.DefaultView = "month"
		default:
			cfg.UI.DefaultView = "day"
		}
		if err := config.Save(cfg); err != nil {
			slog.Error("settings save failed", "err", err)
			errLabel.Text = "Save failed: " + err.Error()
			errLabel.Refresh()
			return
		}
		execPath, _ := os.Executable()
		if autostartCheck.Checked && !autostart.IsEnabled() {
			if err := autostart.Enable(execPath); err != nil {
				slog.Error("autostart enable failed", "err", err)
				errLabel.Text = "Autostart enable failed: " + err.Error()
				errLabel.Refresh()
			}
		} else if !autostartCheck.Checked && autostart.IsEnabled() {
			if err := autostart.Disable(); err != nil {
				slog.Error("autostart disable failed", "err", err)
			}
		}
		errLabel.Text = ""
		errLabel.Refresh()
	})

	barStatusLabel := canvas.NewText("", color.RGBA{R: 100, G: 200, B: 100, A: 255})

	barSetupBtn := widget.NewButton("Set up bar integration", func() {
		execPath, _ := os.Executable()
		r := barsetup.RunSetup(execPath)
		var msgs []string
		if r.KomorebiBar == barsetup.StatusInstalled {
			msgs = append(msgs, "komorebi-bar: configured")
		}
		if r.Waybar == barsetup.StatusInstalled {
			msgs = append(msgs, "Waybar: configured")
		}
		if r.Polybar == barsetup.StatusInstalled {
			msgs = append(msgs, "Polybar: configured")
		}
		if r.KomorebiBar == barsetup.StatusAlreadySetUp || r.Waybar == barsetup.StatusAlreadySetUp || r.Polybar == barsetup.StatusAlreadySetUp {
			msgs = append(msgs, "already configured")
		}
		if len(msgs) == 0 {
			barStatusLabel.Color = color.RGBA{R: 180, G: 180, B: 180, A: 255}
			barStatusLabel.Text = "No supported bar found."
		} else {
			barStatusLabel.Color = color.RGBA{R: 100, G: 200, B: 100, A: 255}
			barStatusLabel.Text = strings.Join(msgs, "; ")
		}
		barStatusLabel.Refresh()
	})

	return container.NewVBox(
		formRow("Default calendar view:", viewSelect),
		autostartCheck,
		errLabel,
		saveBtn,
		widget.NewSeparator(),
		barSetupBtn,
		barStatusLabel,
	)
}

// ---- Notifications tab ----

func buildNotificationsTab() fyne.CanvasObject {
	cfg, err := config.Load()
	if err != nil {
		slog.Warn("settings: could not load config", "err", err)
		cfg = config.Default()
	}

	desktopCheck := widget.NewCheck("Desktop notifications", func(v bool) {
		cfg.Notifications.DesktopEnabled = v
	})
	desktopCheck.SetChecked(cfg.Notifications.DesktopEnabled)

	soundCheck := widget.NewCheck("Sound effects", func(v bool) {
		cfg.Notifications.SoundEnabled = v
	})
	soundCheck.SetChecked(cfg.Notifications.SoundEnabled)

	reminderEntry := widget.NewEntry()
	reminderEntry.SetText(strconv.Itoa(cfg.Notifications.DefaultReminderMinutes))

	saveBtn := widget.NewButton("Save", func() {
		if mins, err := strconv.Atoi(reminderEntry.Text); err == nil && mins >= 0 {
			cfg.Notifications.DefaultReminderMinutes = mins
		}
		if err := config.Save(cfg); err != nil {
			slog.Error("settings save failed", "err", err)
		}
	})

	return container.NewVBox(
		desktopCheck,
		soundCheck,
		formRow("Default reminder (min):", reminderEntry),
		saveBtn,
	)
}

// ---- Appearance tab ----

func buildAppearanceTab() fyne.CanvasObject {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}

	themeOptions := []string{"System", "Light (coming soon)", "Dark (coming soon)", "Retro (coming soon)"}
	themeSelect := widget.NewSelect(themeOptions, nil)
	switch cfg.UI.Theme {
	case "light":
		themeSelect.SetSelected("Light (coming soon)")
	case "dark":
		themeSelect.SetSelected("Dark (coming soon)")
	case "retro":
		themeSelect.SetSelected("Retro (coming soon)")
	default:
		themeSelect.SetSelected("System")
	}

	note := widget.NewLabel("Light, Dark, and Retro themes are coming in a future update.")
	note.TextStyle = fyne.TextStyle{Italic: true}
	note.Wrapping = fyne.TextWrapWord

	saveBtn := widget.NewButton("Save", func() {
		switch themeSelect.Selected {
		case "Light (coming soon)":
			cfg.UI.Theme = "light"
		case "Dark (coming soon)":
			cfg.UI.Theme = "dark"
		case "Retro (coming soon)":
			cfg.UI.Theme = "retro"
		default:
			cfg.UI.Theme = "system"
		}
		if err := config.Save(cfg); err != nil {
			slog.Error("settings save failed", "err", err)
		}
	})

	return container.NewVBox(
		formRow("Theme:", themeSelect),
		note,
		saveBtn,
	)
}

// ---- Calendars tab ----

func buildCalendarsTab() fyne.CanvasObject {
	var listBox *fyne.Container
	listBox = container.NewVBox()

	var rebuild func()
	rebuild = func() {
		listBox.Objects = nil

		cfg, err := config.Load()
		if err != nil {
			cfg = config.Default()
		}
		muteSet := make(map[int64]bool, len(cfg.UI.MuteInviteCalendars))
		for _, id := range cfg.UI.MuteInviteCalendars {
			muteSet[id] = true
		}

		setMuted := func(calID int64, muted bool) {
			c2, err := config.Load()
			if err != nil {
				c2 = config.Default()
			}
			result := make([]int64, 0, len(c2.UI.MuteInviteCalendars))
			for _, id := range c2.UI.MuteInviteCalendars {
				if id != calID {
					result = append(result, id)
				}
			}
			if muted {
				result = append(result, calID)
			}
			c2.UI.MuteInviteCalendars = result
			if err := config.Save(c2); err != nil {
				slog.Error("save mute calendar pref", "err", err)
			}
		}

		cals, err := api.GetCalendars()
		if err != nil {
			slog.Error("load calendars", "err", err)
		}
		for _, cal := range cals {
			c := cal // capture
			colorDot := canvas.NewRectangle(parseHexColor(c.Color))
			colorDot.SetMinSize(fyne.NewSize(16, 16))

			visCheck := widget.NewCheck("", func(visible bool) {
				setCalendarVisible(c.ID, visible)
			})
			visCheck.SetChecked(isCalendarVisible(c.ID))

			muteCheck := widget.NewCheck("Mute invites", func(muted bool) {
				setMuted(c.ID, muted)
			})
			muteCheck.SetChecked(muteSet[c.ID])

			nameLabel := widget.NewLabel(c.Name)

			editBtn := widget.NewButton("Edit", func() {
				ShowEditCalendarWindow(c, rebuild)
			})
			deleteBtn := widget.NewButton("Delete", func() {
				showDeleteCalendarConfirm(c, rebuild)
			})
			if c.ID == 1 {
				deleteBtn.Disable() // protect the default Local calendar
			}

			row := container.NewHBox(visCheck, colorDot, nameLabel, muteCheck, editBtn, deleteBtn)
			listBox.Add(row)
		}
		listBox.Refresh()
	}
	rebuild()

	addBtn := widget.NewButton("Add Calendar", func() {
		ShowEditCalendarWindow(api.Calendar{}, rebuild)
	})

	return container.NewBorder(addBtn, nil, nil, nil, container.NewVScroll(listBox))
}

func showDeleteCalendarConfirm(c api.Calendar, onDone func()) {
	count, err := api.CountEventsForCalendar(c.ID)
	if err != nil || count == 0 {
		showSimpleDeleteCalendar(c, onDone)
		return
	}
	showReassignDeleteCalendar(c, count, onDone)
}

func showSimpleDeleteCalendar(c api.Calendar, onDone func()) {
	a := getFyneApp()
	dw := a.NewWindow(fmt.Sprintf("Delete calendar \"%s\"?", c.Name))
	dw.Resize(fyne.NewSize(320, 110))

	msg := widget.NewLabel(fmt.Sprintf("Delete calendar \"%s\"? This cannot be undone.", c.Name))
	msg.Wrapping = fyne.TextWrapWord

	yesBtn := widget.NewButton("Delete", func() {
		if err := api.DeleteCalendar(c.ID); err != nil {
			slog.Error("delete calendar failed", "id", c.ID, "err", err)
		}
		dw.Close()
		if onCalendarsChanged != nil {
			onCalendarsChanged()
		}
		onDone()
	})
	yesBtn.Importance = widget.DangerImportance
	noBtn := widget.NewButton("Cancel", func() { dw.Close() })

	dw.SetContent(container.NewVBox(msg, container.NewHBox(yesBtn, noBtn)))
	dw.Show()
}

func showReassignDeleteCalendar(c api.Calendar, eventCount int, onDone func()) {
	a := getFyneApp()
	dw := a.NewWindow(fmt.Sprintf("Delete calendar \"%s\"?", c.Name))
	dw.Resize(fyne.NewSize(420, 210))

	msg := widget.NewLabel(fmt.Sprintf(
		"Calendar \"%s\" has %d event(s). What should happen to them?", c.Name, eventCount))
	msg.Wrapping = fyne.TextWrapWord

	cals, _ := api.GetCalendars()
	var otherCals []api.Calendar
	for _, cal := range cals {
		if cal.ID != c.ID {
			otherCals = append(otherCals, cal)
		}
	}
	calNames := make([]string, len(otherCals))
	for i, cal := range otherCals {
		calNames[i] = cal.Name
	}

	calSelect := widget.NewSelect(calNames, nil)
	if len(calNames) > 0 {
		calSelect.SetSelectedIndex(0)
	}

	errorLbl := canvas.NewText("", color.RGBA{R: 200, A: 255})

	doDelete := func(reassign bool) {
		if reassign {
			if len(otherCals) == 0 || calSelect.SelectedIndex() < 0 {
				errorLbl.Text = "Select a calendar to reassign to"
				errorLbl.Refresh()
				return
			}
			toID := otherCals[calSelect.SelectedIndex()].ID
			if err := api.ReassignCalendarEvents(c.ID, toID); err != nil {
				slog.Error("reassign events failed", "err", err)
			}
		} else {
			if err := api.DeleteEventsByCalendar(c.ID); err != nil {
				slog.Error("delete events failed", "err", err)
			}
		}
		if err := api.DeleteCalendar(c.ID); err != nil {
			slog.Error("delete calendar failed", "id", c.ID, "err", err)
		}
		dw.Close()
		if onCalendarsChanged != nil {
			onCalendarsChanged()
		}
		onDone()
	}

	reassignBtn := widget.NewButton("Reassign events", func() { doDelete(true) })
	deleteAllBtn := widget.NewButton("Delete events too", func() { doDelete(false) })
	deleteAllBtn.Importance = widget.DangerImportance
	cancelBtn := widget.NewButton("Cancel", func() { dw.Close() })

	var reassignRow fyne.CanvasObject
	if len(calNames) > 0 {
		reassignRow = container.NewVBox(widget.NewLabel("Reassign to:"), calSelect)
	} else {
		reassignRow = widget.NewLabel("(no other calendars to reassign to)")
		reassignBtn.Disable()
	}

	dw.SetContent(container.NewVBox(
		msg,
		reassignRow,
		errorLbl,
		container.NewHBox(reassignBtn, deleteAllBtn, cancelBtn),
	))
	dw.Show()
}

// ShowEditCalendarWindow opens the create-or-edit calendar form.
// cal.ID == 0 means create; otherwise edit.
func ShowEditCalendarWindow(cal api.Calendar, onSave func()) {
	a := getFyneApp()
	title := "Add Calendar"
	if cal.ID != 0 {
		title = "Edit Calendar — " + cal.Name
	}
	w := a.NewWindow(title)
	w.Resize(fyne.NewSize(400, 340))

	nameEntry := widget.NewEntry()
	nameEntry.SetText(cal.Name)
	nameEntry.SetPlaceHolder("Calendar name")

	hexVal := cal.Color
	if hexVal == "" {
		hexVal = "#3B82F6"
	}

	swatch := canvas.NewRectangle(parseHexColor(hexVal))
	swatch.SetMinSize(fyne.NewSize(32, 32))

	hexEntry := widget.NewEntry()
	hexEntry.SetText(hexVal)
	hexEntry.OnChanged = func(s string) {
		if len(s) == 7 && s[0] == '#' {
			swatch.FillColor = parseHexColor(s)
			swatch.Refresh()
		}
	}

	presets := []string{
		"#3B82F6", "#EF4444", "#22C55E", "#F59E0B",
		"#8B5CF6", "#EC4899", "#14B8A6", "#F97316",
		"#6B7280", "#FBBF24", "#10B981", "#6366F1",
	}

	presetRow := container.NewHBox()
	for _, p := range presets {
		pColor := p
		dot := canvas.NewRectangle(parseHexColor(pColor))
		dot.SetMinSize(fyne.NewSize(22, 22))
		btn := widget.NewButton("", func() {
			hexEntry.SetText(pColor)
			swatch.FillColor = parseHexColor(pColor)
			swatch.Refresh()
		})
		btn.Importance = widget.LowImportance
		presetRow.Add(container.NewStack(dot, btn))
	}

	errorLabel := canvas.NewText("", color.RGBA{R: 200, A: 255})

	saveBtn := widget.NewButton("Save", func() {
		name := nameEntry.Text
		if name == "" {
			errorLabel.Text = "Name is required"
			errorLabel.Refresh()
			return
		}
		hexColor := hexEntry.Text
		if len(hexColor) != 7 || hexColor[0] != '#' {
			hexColor = "#3B82F6"
		}

		if cal.ID == 0 {
			_, err := api.CreateCalendar(name, hexColor, "local")
			if err != nil {
				errorLabel.Text = "Failed: " + err.Error()
				errorLabel.Refresh()
				return
			}
		} else {
			err := api.UpdateCalendar(cal.ID, map[string]any{
				"name":  name,
				"color": hexColor,
			})
			if err != nil {
				errorLabel.Text = "Failed: " + err.Error()
				errorLabel.Refresh()
				return
			}
		}

		w.Close()
		if onCalendarsChanged != nil {
			onCalendarsChanged()
		}
		if onSave != nil {
			onSave()
		}
	})
	cancelBtn := widget.NewButton("Cancel", func() { w.Close() })

	form := container.NewVBox(
		formRow("Name:", nameEntry),
		widget.NewLabel("Color:"),
		presetRow,
		container.NewHBox(swatch, formRow("Hex (#RRGGBB):", hexEntry)),
		errorLabel,
		container.NewHBox(saveBtn, cancelBtn),
	)

	w.SetContent(form)
	w.Show()
}

