package daemon

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"pycalendar/internal/api"
	"pycalendar/internal/config"
	"pycalendar/internal/notifier"
	"pycalendar/internal/push"
)

// Daemon polls for due reminders and dispatches notifications.
type Daemon struct {
	interval time.Duration
	notified map[int64]struct{} // suppresses duplicate notifications within a run
	mu       sync.Mutex
	stop     chan struct{}
}

// New creates a Daemon that checks every interval.
func New(interval time.Duration) *Daemon {
	return &Daemon{
		interval: interval,
		notified: make(map[int64]struct{}),
		stop:     make(chan struct{}),
	}
}

// Run starts the daemon loop. It blocks until Stop is called.
func (d *Daemon) Run() {
	slog.Info("daemon starting", "interval", d.interval)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	// Check immediately on start before waiting for first tick.
	d.checkReminders()

	for {
		select {
		case <-ticker.C:
			d.checkReminders()
		case <-d.stop:
			slog.Info("daemon stopped")
			return
		}
	}
}

// Stop signals the daemon to exit.
func (d *Daemon) Stop() {
	close(d.stop)
}

func (d *Daemon) checkReminders() {
	cfg, err := config.Load()
	if err != nil {
		slog.Warn("daemon: could not load config", "err", err)
	}

	events, err := api.GetDueReminders(120)
	if err != nil {
		slog.Error("daemon: get due reminders failed", "err", err)
		return
	}

	for _, e := range events {
		d.mu.Lock()
		_, alreadySent := d.notified[e.ID]
		d.mu.Unlock()

		if alreadySent {
			continue
		}

		if !cfg.Notifications.DesktopEnabled {
			slog.Info("skipping notification (desktop disabled)", "event_id", e.ID)
			d.mu.Lock()
			d.notified[e.ID] = struct{}{}
			d.mu.Unlock()
			continue
		}

		title := "Reminder: " + e.Title
		msg := fmt.Sprintf("Starting %s", e.StartTime().Format("15:04 on 2006-01-02"))
		if e.Notes.Valid && e.Notes.String != "" {
			msg += "\n" + e.Notes.String
		}

		if err := notifier.Notify(title, msg, cfg.Notifications.SoundEnabled); err != nil {
			slog.Warn("notification failed", "event_id", e.ID, "err", err)
			continue
		}

		slog.Info("sent reminder", "event_id", e.ID, "title", e.Title)
		d.mu.Lock()
		d.notified[e.ID] = struct{}{}
		d.mu.Unlock()

		if cfg.MobilePush.Enabled && cfg.MobilePush.WebhookURL != "" {
			go func(ev api.Event, t, b string) {
				if err := push.Send(cfg.MobilePush.WebhookURL, t, b, ev); err != nil {
					slog.Error("mobile push failed", "event_id", ev.ID, "err", err)
				} else {
					slog.Info("sent mobile push", "event_id", ev.ID)
				}
			}(e, title, msg)
		}
	}
}
