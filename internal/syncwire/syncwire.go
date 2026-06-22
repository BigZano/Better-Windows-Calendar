// Package syncwire builds and launches the Milestone 2 Sync Engine from the
// persisted Calendars and config. It sits above the protocol adapters (caldav,
// msgraph) so it can import them without the import cycle the syncer package
// itself would hit (caldav and msgraph both import syncer).
package syncwire

import (
	"context"
	"log/slog"
	"time"

	"pycalendar/internal/api"
	"pycalendar/internal/caldav"
	"pycalendar/internal/config"
	"pycalendar/internal/msgraph"
	"pycalendar/internal/syncer"
)

const defaultIntervalMinutes = 5

// BuildEngine reads the sync-enabled Calendars, constructs a protocol Adapter
// for each, and registers them on a new (stopped) Engine. The returned int is
// the number of Adapters registered.
func BuildEngine(cfg config.Config) (*syncer.Engine, int, error) {
	minutes := cfg.Sync.IntervalMinutes
	if minutes <= 0 {
		minutes = defaultIntervalMinutes
	}
	eng := syncer.New(time.Duration(minutes) * time.Minute)

	cals, err := api.GetCalendars()
	if err != nil {
		return nil, 0, err
	}

	n := 0
	for _, c := range cals {
		if !c.SyncEnabled || c.Type == api.CalendarTypeLocal {
			continue
		}
		adapter, ok := buildAdapter(c, cfg)
		if !ok {
			slog.Warn("syncwire: calendar has no usable adapter; skipping",
				"id", c.ID, "type", c.Type)
			continue
		}
		eng.RegisterAdapter(c.ID, adapter)
		n++
	}
	return eng, n, nil
}

// buildAdapter maps a Calendar to its protocol Adapter. ok is false when the
// Calendar's type has no implemented or fully-configured Adapter. It performs
// no I/O — credentials are loaded lazily during sync — so it is pure and
// directly unit-testable.
func buildAdapter(c api.Calendar, cfg config.Config) (adapter syncer.Adapter, ok bool) {
	switch c.Type {
	case api.CalendarTypeCalDAV:
		if !c.SyncURL.Valid || c.SyncURL.String == "" {
			return nil, false
		}
		return caldav.New(c.ID, c.SyncURL.String), true
	case api.CalendarTypeOutlook:
		// SyncURL carries the opaque Graph calendar ID for Outlook calendars.
		clientID := cfg.OAuth.MicrosoftClientID
		if clientID == "" || !c.SyncURL.Valid || c.SyncURL.String == "" {
			return nil, false
		}
		return msgraph.New(c.ID, c.SyncURL.String, clientID), true
	default:
		// google is deferred (ADR-0003); unknown types fall through.
		return nil, false
	}
}

// Start builds the Sync Engine from config and the persisted Calendars and
// launches it on its own goroutine (ADR-0002: independent of the reminder
// Daemon). The returned stop function blocks until the engine has stopped; it
// is a no-op when there are no sync-enabled Calendars. n is the number of
// Calendars being synced.
func Start(ctx context.Context) (stop func(), n int, err error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, 0, err
	}
	eng, n, err := BuildEngine(cfg)
	if err != nil {
		return nil, 0, err
	}
	if n == 0 {
		slog.Info("syncwire: no sync-enabled calendars; sync engine idle")
		return func() {}, 0, nil
	}
	eng.Start(ctx)
	slog.Info("syncwire: sync engine started", "calendars", n, "interval_min", cfg.Sync.IntervalMinutes)
	return eng.Stop, n, nil
}
