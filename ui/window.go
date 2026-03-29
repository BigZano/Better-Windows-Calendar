package ui

import (
	"fmt"
	"image/color"
	"log/slog"
	"strconv"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"pycalendar/internal/api"
	"pycalendar/internal/autostart"
	"pycalendar/internal/config"
)

var fyneApp fyne.App

func getFyneApp() fyne.App {
	if fyneApp == nil {
		fyneApp = app.New()
	}
	return fyneApp
}

// ShowCalendarWindow opens (or brings to front) the main calendar window.
func ShowCalendarWindow() {
	a := getFyneApp()
	w := a.NewWindow("PyCalendar")
	w.Resize(fyne.NewSize(600, 400))

	events, err := api.GetUpcoming(50)
	if err != nil {
		slog.Error("failed to load events", "err", err)
	}

	list := widget.NewList(
		func() int { return len(events) },
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			e := events[i]
			o.(*widget.Label).SetText(
				fmt.Sprintf("[%3d] %s - %s", e.ID, e.StartTime().Format("2006-01-02 15:04"), e.Title),
			)
		},
	)

	refreshBtn := widget.NewButton("Refresh", func() {
		events, err = api.GetUpcoming(50)
		if err != nil {
			slog.Error("refresh failed", "err", err)
		}
		list.Refresh()
	})

	addBtn := widget.NewButton("Add Event", func() {
		ShowAddEventDialog(func() {
			events, err = api.GetUpcoming(50)
			if err != nil {
				slog.Error("refresh after add failed", "err", err)
			}
			list.Refresh()
		})
	})

	deleteBtn := widget.NewButton("Delete Selected", func() {
		sel := list.Length() // placeholder — deletion via dialog
		slog.Info("delete requested", "list_length", sel)
		dialog.ShowInformation("Delete", "Select an event ID to delete.", w)
	})

	settingsBtn := widget.NewButton("Settings", func() {
		ShowSettingsDialog(w)
	})

	toolbar := container.NewHBox(addBtn, refreshBtn, deleteBtn, settingsBtn)
	content := container.NewBorder(toolbar, nil, nil, nil, list)
	w.SetContent(content)
	w.Show()
}

// ShowAddEventDialog opens a form for creating a new event.
// onSuccess is called (if non-nil) after the event is saved.
func ShowAddEventDialog(onSuccess func()) {
	a := getFyneApp()
	w := a.NewWindow("Add Event")
	w.Resize(fyne.NewSize(420, 320))

	titleEntry := widget.NewEntry()
	titleEntry.SetPlaceHolder("Event title")

	defaultStart := time.Now().Add(time.Hour).Format("2006-01-02 15:04")
	startEntry := widget.NewEntry()
	startEntry.SetText(defaultStart)

	notesEntry := widget.NewMultiLineEntry()
	notesEntry.SetPlaceHolder("Notes (optional)")
	notesEntry.SetMinRowsVisible(3)

	reminderEntry := widget.NewEntry()
	reminderEntry.SetText("15")

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
			errorLabel.Text = "Invalid date format (use YYYY-MM-DD HH:MM)"
			errorLabel.Refresh()
			return
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

		id, err := api.CreateEvent(title, startTime, nil, notesEntry.Text, &reminderMin, "", false, "Local")
		if err != nil {
			errorLabel.Text = "Failed to save: " + err.Error()
			errorLabel.Refresh()
			slog.Error("create event failed", "err", err)
			return
		}

		slog.Info("event created via UI", "id", id)
		w.Close()
		if onSuccess != nil {
			onSuccess()
		}
	})

	cancelBtn := widget.NewButton("Cancel", func() { w.Close() })

	form := container.NewVBox(
		widget.NewLabel("Title:"), titleEntry,
		widget.NewLabel("Start (YYYY-MM-DD HH:MM):"), startEntry,
		widget.NewLabel("Notes:"), notesEntry,
		widget.NewLabel("Reminder (minutes before):"), reminderEntry,
		errorLabel,
		container.NewHBox(saveBtn, cancelBtn),
	)

	w.SetContent(form)
	w.Show()
}

// ShowSettingsDialog opens the settings window attached to parent.
func ShowSettingsDialog(parent fyne.Window) {
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

	autostartCheck := widget.NewCheck("Start daemon with system", func(_ bool) {})
	autostartCheck.SetChecked(autostart.IsEnabled())

	reminderEntry := widget.NewEntry()
	reminderEntry.SetText(strconv.Itoa(cfg.Notifications.DefaultReminderMinutes))

	form := dialog.NewForm("Settings", "Save", "Cancel",
		[]*widget.FormItem{
			{Text: "", Widget: desktopCheck},
			{Text: "", Widget: soundCheck},
			{Text: "", Widget: autostartCheck},
			{Text: "Default reminder (min)", Widget: reminderEntry},
		},
		func(save bool) {
			if !save {
				return
			}

			if mins, err := strconv.Atoi(reminderEntry.Text); err == nil && mins >= 0 {
				cfg.Notifications.DefaultReminderMinutes = mins
			}

			if err := config.Save(cfg); err != nil {
				slog.Error("settings save failed", "err", err)
			}

			// Handle autostart toggle.
			execPath, _ := fyne.CurrentApp().Metadata().Custom["ExecPath"]
			if autostartCheck.Checked && !autostart.IsEnabled() {
				if err := autostart.Enable(execPath); err != nil {
					slog.Error("autostart enable failed", "err", err)
				}
			} else if !autostartCheck.Checked && autostart.IsEnabled() {
				if err := autostart.Disable(); err != nil {
					slog.Error("autostart disable failed", "err", err)
				}
			}
		},
		parent,
	)
	form.Show()
}

// setTrayIcon generates a minimal programmatic tray icon using Fyne-compatible
// image primitives. Kept simple — replace assets/icon.png later for a real icon.
func setTrayIcon() {
	// systray requires a []byte PNG; we embed a minimal 1x1 black pixel PNG.
	// A proper icon should be embedded via //go:embed assets/icon.png in a
	// separate file once the asset is created.
	// For now we leave the default system icon rather than embed a bad icon.
}
