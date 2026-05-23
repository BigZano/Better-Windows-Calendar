package ui

import (
	"log/slog"
	"time"

	"github.com/getlantern/systray"

	"pycalendar/internal/api"
	"pycalendar/internal/config"
	"pycalendar/internal/daemon"
	"pycalendar/internal/storage"
)

var embeddedDaemon *daemon.Daemon

var trayIconPNG []byte

// SetTrayIconData passes the embedded icon bytes from the main package.
func SetTrayIconData(data []byte) { trayIconPNG = data }

func setTrayIcon() {
	if len(trayIconPNG) > 0 {
		systray.SetIcon(trayIconPNG)
	}
}

// RunTray initialises the database, then starts the system tray event loop.
// This function blocks until the user quits.
func RunTray() {
	if err := storage.InitDB(); err != nil {
		slog.Error("failed to init database", "err", err)
		return
	}
	if cfg, err := config.Load(); err != nil {
		slog.Warn("failed to load config", "err", err)
	} else {
		InitVisibilityFromConfig(cfg.UI.HiddenCalendars)
	}
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetTitle("PyCalendar")
	systray.SetTooltip("PyCalendar")
	setTrayIcon()

	embeddedDaemon = daemon.New(30 * time.Second)
	go embeddedDaemon.Run()

	mOpen := systray.AddMenuItem("Open Calendar", "Show the calendar window")
	mAdd := systray.AddMenuItem("Add Event", "Add a new event")
	mSettings := systray.AddMenuItem("Settings", "Open settings")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Exit PyCalendar")

	go func() {
		for {
			select {
			case <-mOpen.ClickedCh:
				ShowCalendarWindow()
			case <-mAdd.ClickedCh:
				ShowAddEventDialog(nil)
			case <-mSettings.ClickedCh:
				ShowSettingsWindow()
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()

	go updateTooltip()
}

func onExit() {
	if embeddedDaemon != nil {
		embeddedDaemon.Stop()
	}
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
