// Package syncwire builds and launches the Milestone 2 Sync Engine from the
// persisted Calendars and config. It sits above the protocol adapters (caldav,
// msgraph) so it can import them without the import cycle the syncer package
// itself would hit (caldav and msgraph both import syncer).
package syncwire

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"pycalendar/internal/api"
	"pycalendar/internal/caldav"
	"pycalendar/internal/config"
	"pycalendar/internal/msgraph"
	"pycalendar/internal/syncer"
)

const defaultIntervalMinutes = 5

// running holds the live Engine launched by Start, so the UI ("Sync Now",
// adding a calendar) can reach it. Guarded by mu.
var (
	mu      sync.Mutex
	running *syncer.Engine
)

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
// Daemon). The engine runs even with zero sync-enabled Calendars so that one
// added at runtime (via RegisterCalendar) is picked up without a restart. The
// returned stop function blocks until the engine has stopped. n is the number
// of Calendars registered at boot.
func Start(ctx context.Context) (stop func(), n int, err error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, 0, err
	}
	eng, n, err := BuildEngine(cfg)
	if err != nil {
		return nil, 0, err
	}

	mu.Lock()
	running = eng
	mu.Unlock()

	eng.Start(ctx)
	slog.Info("syncwire: sync engine started", "calendars", n, "interval_min", cfg.Sync.IntervalMinutes)
	return func() {
		eng.Stop()
		mu.Lock()
		if running == eng {
			running = nil
		}
		mu.Unlock()
	}, n, nil
}

// RegisterCalendar (re)builds the Adapter for calID and registers it on the
// running engine, so a Calendar added or reconfigured at runtime starts syncing
// without a restart. No-op error if the engine is not running.
func RegisterCalendar(calID int64) error {
	mu.Lock()
	eng := running
	mu.Unlock()
	if eng == nil {
		return errors.New("syncwire: sync engine not running")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cal, err := api.GetCalendar(calID)
	if err != nil {
		return err
	}
	adapter, ok := buildAdapter(cal, cfg)
	if !ok {
		return fmt.Errorf("syncwire: calendar %d (type %q) has no usable adapter", calID, cal.Type)
	}
	eng.RegisterAdapter(calID, adapter)
	slog.Info("syncwire: registered calendar", "id", calID, "type", cal.Type)
	return nil
}

// UnregisterCalendar removes calID from the running engine (on delete or when
// sync is disabled). No-op if the engine is not running.
func UnregisterCalendar(calID int64) {
	mu.Lock()
	eng := running
	mu.Unlock()
	if eng != nil {
		eng.UnregisterAdapter(calID)
	}
}

// SyncNow syncs calID immediately in the calling goroutine. It routes through
// the running engine when that engine owns the calendar (so the sync is
// serialized against the background ticker); otherwise it performs a one-shot
// sync with a throwaway engine. Returns an error if the Calendar has no usable
// Adapter.
func SyncNow(ctx context.Context, calID int64) error {
	mu.Lock()
	eng := running
	mu.Unlock()
	if eng != nil && eng.HasAdapter(calID) {
		return eng.Sync(ctx, calID)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cal, err := api.GetCalendar(calID)
	if err != nil {
		return err
	}
	adapter, ok := buildAdapter(cal, cfg)
	if !ok {
		return fmt.Errorf("syncwire: calendar %d (type %q) is not sync-enabled", calID, cal.Type)
	}
	tmp := syncer.New(time.Hour) // interval unused; never Started
	tmp.RegisterAdapter(calID, adapter)
	return tmp.Sync(ctx, calID)
}
