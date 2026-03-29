package ui

import (
	"log/slog"

	"github.com/getlantern/systray"

	"pycalendar/internal/api"
	"pycalendar/internal/storage"
)

// RunTray initialises the database, then starts the system tray event loop.
// This function blocks until the user quits.
func RunTray() {
	if err := storage.InitDB(); err != nil {
		slog.Error("failed to init database", "err", err)
		return
	}
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTitle("PyCalendar")
	systray.SetTooltip("PyCalendar")
	setTrayIcon()

	mOpen := systray.AddMenuItem("Open Calendar", "Show the calendar window")
	mAdd := systray.AddMenuItem("Add Event", "Add a new event")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Exit PyCalendar")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				ShowCalendarWindow()
			case <-mAdd.ClickedCh:
				ShowAddEventDialog(nil)
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()

	// Refresh tray tooltip with upcoming event count on start.
	go updateTooltip()
}

func onExit() {
	slog.Info("tray exiting")
}

func updateTooltip() {
	events, err := api.GetUpcoming(5)
	if err != nil {
		return
	}
	if len(events) == 0 {
		systray.SetTooltip("PyCalendar — no upcoming events")
		return
	}
	next := events[0]
	systray.SetTooltip("PyCalendar — next: " + next.Title + " @ " + next.StartTime().Format("15:04 01/02"))
}
