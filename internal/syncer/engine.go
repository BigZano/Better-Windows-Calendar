package syncer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"pycalendar/internal/api"
)

// ChangeType describes how a remote resource has changed.
type ChangeType int

const (
	ChangeUpsert ChangeType = iota
	ChangeDelete
)

// RemoteChange represents a single change fetched from a remote calendar.
type RemoteChange struct {
	ResourceURL string
	ETag        string
	Type        ChangeType
	Event       *api.Event // nil for ChangeDelete
}

// Adapter is the protocol-specific backend for syncing one calendar.
// CalDAV and Microsoft Graph each provide a distinct Adapter implementation.
type Adapter interface {
	// FetchChanges returns changes since the last sync. state.SyncToken drives
	// incremental fetches; an empty token triggers a full fetch.
	FetchChanges(ctx context.Context, state *SyncState) ([]RemoteChange, error)
	// PushChange sends a local event change to the remote calendar.
	PushChange(ctx context.Context, state *SyncState, e api.Event) error
	// DeleteRemote removes the remote resource at resourceURL.
	DeleteRemote(ctx context.Context, state *SyncState, resourceURL string) error
}

// SyncEventSource is an optional interface for adapters that support server-push
// notifications (e.g. webhook channels). Callers type-assert to check availability.
type SyncEventSource interface {
	Notify(calendarID int64)
}

// SyncStatus holds the last-known sync result for a calendar.
type SyncStatus struct {
	CalendarID int64
	LastSyncAt time.Time
	InProgress bool
	LastError  error
}

// Engine orchestrates periodic sync for all registered calendars.
// It runs on its own goroutine, independent of the reminder Daemon (ADR-0002).
type Engine struct {
	interval time.Duration
	adapters map[int64]Adapter

	mu       sync.RWMutex
	statuses map[int64]SyncStatus

	stop chan struct{}
	done chan struct{}
}

// New returns a stopped Engine with the given sync interval.
// Call RegisterAdapter for each sync-enabled calendar, then Start.
func New(interval time.Duration) *Engine {
	return &Engine{
		interval: interval,
		adapters: make(map[int64]Adapter),
		statuses: make(map[int64]SyncStatus),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// RegisterAdapter binds a protocol Adapter to calendarID.
// Must be called before Start.
func (e *Engine) RegisterAdapter(calendarID int64, a Adapter) {
	e.adapters[calendarID] = a
}

// Start launches the sync goroutine. Safe to call exactly once.
func (e *Engine) Start(ctx context.Context) {
	go e.run(ctx)
}

// Stop signals the sync goroutine to exit and blocks until it has.
func (e *Engine) Stop() {
	close(e.stop)
	<-e.done
}

// Sync syncs a single calendar immediately in the calling goroutine.
func (e *Engine) Sync(ctx context.Context, calendarID int64) error {
	a, ok := e.adapters[calendarID]
	if !ok {
		return fmt.Errorf("syncer: no adapter for calendar %d", calendarID)
	}
	return e.syncOne(ctx, calendarID, a)
}

// SyncAll syncs every registered calendar sequentially.
// Returns the first error; sync continues for remaining calendars regardless.
func (e *Engine) SyncAll(ctx context.Context) error {
	var firstErr error
	for calID, a := range e.adapters {
		if err := e.syncOne(ctx, calID, a); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Status returns the last-known SyncStatus for calendarID.
func (e *Engine) Status(calendarID int64) SyncStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.statuses[calendarID]
}

func (e *Engine) run(ctx context.Context) {
	defer close(e.done)
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stop:
			return
		case <-ticker.C:
			_ = e.SyncAll(ctx)
		}
	}
}

func (e *Engine) syncOne(ctx context.Context, calendarID int64, a Adapter) error {
	e.setStatus(calendarID, SyncStatus{CalendarID: calendarID, InProgress: true})

	state, err := LoadSyncState(calendarID)
	if err != nil {
		e.setStatus(calendarID, SyncStatus{CalendarID: calendarID, LastError: err})
		return fmt.Errorf("syncer %d: load state: %w", calendarID, err)
	}

	changes, err := a.FetchChanges(ctx, state)
	if err != nil {
		e.setStatus(calendarID, SyncStatus{CalendarID: calendarID, LastError: err})
		return fmt.Errorf("syncer %d: fetch: %w", calendarID, err)
	}

	for _, ch := range changes {
		if err := applyChange(ctx, calendarID, ch, state); err != nil {
			slog.Warn("syncer: apply change failed",
				"calendar", calendarID, "url", ch.ResourceURL, "err", err)
		}
	}

	state.LastSyncAt = time.Now()
	if err := state.Save(); err != nil {
		slog.Warn("syncer: save state failed", "calendar", calendarID, "err", err)
	}

	e.setStatus(calendarID, SyncStatus{
		CalendarID: calendarID,
		LastSyncAt: state.LastSyncAt,
	})
	slog.Info("syncer: complete", "calendar", calendarID, "changes", len(changes))
	return nil
}

func (e *Engine) setStatus(calendarID int64, s SyncStatus) {
	e.mu.Lock()
	e.statuses[calendarID] = s
	e.mu.Unlock()
}

// applyChange merges one RemoteChange into the local database.
// On conflict (both local and remote changed since last sync), the conflict is
// recorded in the Conflict Queue and remote-wins is applied by default (ADR-0007).
func applyChange(_ context.Context, calendarID int64, ch RemoteChange, state *SyncState) error {
	if ch.Type == ChangeDelete {
		existing, err := api.GetEventByResourceURL(ch.ResourceURL)
		if err != nil {
			if errors.Is(err, api.ErrNotFound) {
				return nil // already deleted locally
			}
			return err
		}
		return api.DeleteEvent(existing.ID)
	}

	if ch.Event == nil {
		return nil
	}

	existing, err := api.GetEventByResourceURL(ch.ResourceURL)
	if err != nil {
		if !errors.Is(err, api.ErrNotFound) {
			return err
		}
		// New remote event — insert.
		_, err = api.CreateEventFromRemote(*ch.Event, calendarID, ch.ResourceURL)
		if err != nil {
			return err
		}
		state.SetETag(ch.ResourceURL, ch.ETag)
		return nil
	}

	// Both exist: detect conflict (local changed after last sync).
	if state.LastSyncAt.Unix() > 0 && existing.UpdatedTS > state.LastSyncAt.Unix() {
		if cerr := RecordConflict(calendarID, existing, *ch.Event); cerr != nil {
			slog.Warn("syncer: record conflict failed", "err", cerr)
		}
		// Remote-wins: fall through to update.
	}

	if err := api.UpdateEventFromRemote(existing.ID, *ch.Event); err != nil {
		return err
	}
	state.SetETag(ch.ResourceURL, ch.ETag)
	return nil
}
