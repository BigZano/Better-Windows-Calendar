package ui

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"

	"pycalendar/internal/config"
	"pycalendar/internal/daemon"
	"pycalendar/internal/storage"
	"pycalendar/internal/syncer"
	"pycalendar/internal/syncwire"
)

var embeddedDaemon *daemon.Daemon

var trayIconPNG []byte

// Package-level handles so the tray menu can be rebuilt after startup (e.g. to
// refresh the conflict badge).
var (
	trayApp  fyne.App
	trayDesk desktop.App
)

// SetTrayIconData passes the embedded icon bytes from the main package.
func SetTrayIconData(data []byte) { trayIconPNG = data }

// buildTrayMenu constructs the system-tray menu, inserting a conflict-count
// item when there are unresolved sync conflicts (ADR-0007 tray badge).
func buildTrayMenu() *fyne.Menu {
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Open Calendar", func() { ShowCalendarWindow() }),
		fyne.NewMenuItem("Add Event", func() { ShowAddEventDialog(nil) }),
		fyne.NewMenuItem("Settings", func() { ShowSettingsWindow() }),
	}

	if n, err := syncer.CountPendingConflicts(); err == nil && n > 0 {
		label := fmt.Sprintf("⚠ %d sync conflict", n)
		if n > 1 {
			label += "s"
		}
		items = append(items,
			fyne.NewMenuItemSeparator(),
			fyne.NewMenuItem(label, func() { ShowAlertsWindow() }),
		)
	}

	items = append(items,
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() {
			if embeddedDaemon != nil {
				embeddedDaemon.Stop()
			}
			if trayApp != nil {
				trayApp.Quit()
			}
		}),
	)
	return fyne.NewMenu("PyCalendar", items...)
}

// RefreshTrayBadge rebuilds the tray menu so the conflict count is current.
// Safe to call after a sync or conflict resolution.
func RefreshTrayBadge() {
	if trayDesk != nil {
		trayDesk.SetSystemTrayMenu(buildTrayMenu())
	}
}

// RunTray initialises the database, sets up the system tray via Fyne's
// desktop.App interface, and runs the Fyne event loop. Blocks until Quit.
func RunTray() {
	if err := storage.InitDB(); err != nil {
		slog.Error("failed to init database", "err", err)
		return
	}
	if err := syncer.PruneStaleConflicts(); err != nil {
		slog.Warn("prune stale conflicts failed", "err", err)
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
		trayApp = a
		trayDesk = desk
		desk.SetSystemTrayMenu(buildTrayMenu())
	}

	embeddedDaemon = daemon.New(30 * time.Second)
	go embeddedDaemon.Run()

	syncCtx, cancelSync := context.WithCancel(context.Background())
	stopSync, _, err := syncwire.Start(syncCtx)
	if err != nil {
		slog.Warn("sync engine failed to start", "err", err)
		stopSync = func() {}
	}

	// Refresh the conflict badge periodically so conflicts detected by the
	// background sync surface without user interaction.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-syncCtx.Done():
				return
			case <-ticker.C:
				RefreshTrayBadge()
			}
		}
	}()

	a.Run()

	stopSync()
	cancelSync()
	if embeddedDaemon != nil {
		embeddedDaemon.Stop()
	}
	slog.Info("tray exiting")
}
