package ui

import (
	"context"
	"log/slog"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"

	"pycalendar/internal/config"
	"pycalendar/internal/daemon"
	"pycalendar/internal/storage"
	"pycalendar/internal/syncwire"
)

var embeddedDaemon *daemon.Daemon

var trayIconPNG []byte

// SetTrayIconData passes the embedded icon bytes from the main package.
func SetTrayIconData(data []byte) { trayIconPNG = data }

// RunTray initialises the database, sets up the system tray via Fyne's
// desktop.App interface, and runs the Fyne event loop. Blocks until Quit.
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

	a := getFyneApp()

	// Show one-time setup wizard on first launch.
	if cfg, err := config.Load(); err == nil && !cfg.UI.FirstRunComplete {
		ShowFirstRunWizard()
	}

	if len(trayIconPNG) > 0 {
		iconRes := fyne.NewStaticResource("icon.png", trayIconPNG)
		a.SetIcon(iconRes)
		if desk, ok := a.(desktop.App); ok {
			desk.SetSystemTrayIcon(iconRes)
		}
	}

	if desk, ok := a.(desktop.App); ok {
		desk.SetSystemTrayMenu(fyne.NewMenu("PyCalendar",
			fyne.NewMenuItem("Open Calendar", func() {
				ShowCalendarWindow()
			}),
			fyne.NewMenuItem("Add Event", func() {
				ShowAddEventDialog(nil)
			}),
			fyne.NewMenuItem("Settings", func() {
				ShowSettingsWindow()
			}),
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem("Quit", func() {
				if embeddedDaemon != nil {
					embeddedDaemon.Stop()
				}
				a.Quit()
			}),
		))
	}

	embeddedDaemon = daemon.New(30 * time.Second)
	go embeddedDaemon.Run()

	syncCtx, cancelSync := context.WithCancel(context.Background())
	stopSync, _, err := syncwire.Start(syncCtx)
	if err != nil {
		slog.Warn("sync engine failed to start", "err", err)
		stopSync = func() {}
	}

	a.Run()

	stopSync()
	cancelSync()
	if embeddedDaemon != nil {
		embeddedDaemon.Stop()
	}
	slog.Info("tray exiting")
}
