package syncer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"maps"
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

// PushResult reports where a pushed event lives on the remote afterwards, so
// the engine can link the local event to its remote resource and avoid
// re-importing it as a new event on the next fetch.
type PushResult struct {
	ResourceURL string
	ETag        string
}

// Adapter is the protocol-specific backend for syncing one calendar.
// CalDAV and Microsoft Graph each provide a distinct Adapter implementation.
type Adapter interface {
	// FetchChanges returns changes since the last sync. state.SyncToken drives
	// incremental fetches; an empty token triggers a full fetch.
	FetchChanges(ctx context.Context, state *SyncState) ([]RemoteChange, error)
	// PushChange sends a local event change to the remote calendar and returns
	// the resulting remote resource URL and ETag (the existing one for an
	// update, a freshly assigned one for a create).
	PushChange(ctx context.Context, state *SyncState, e api.Event) (PushResult, error)
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

	mu        sync.RWMutex
	adapters  map[int64]Adapter
	statuses  map[int64]SyncStatus
	syncLocks map[int64]*sync.Mutex // serializes sync per calendar

	stop chan struct{}
	done chan struct{}
}

// New returns a stopped Engine with the given sync interval.
// Call RegisterAdapter for each sync-enabled calendar, then Start.
func New(interval time.Duration) *Engine {
	return &Engine{
		interval:  interval,
		adapters:  make(map[int64]Adapter),
		statuses:  make(map[int64]SyncStatus),
		syncLocks: make(map[int64]*sync.Mutex),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
}

// RegisterAdapter binds a protocol Adapter to calendarID. Safe to call after
// Start, allowing calendars added at runtime to begin syncing.
func (e *Engine) RegisterAdapter(calendarID int64, a Adapter) {
	e.mu.Lock()
	e.adapters[calendarID] = a
	e.mu.Unlock()
}

// HasAdapter reports whether an Adapter is registered for calendarID.
func (e *Engine) HasAdapter(calendarID int64) bool {
	e.mu.RLock()
	_, ok := e.adapters[calendarID]
	e.mu.RUnlock()
	return ok
}

// UnregisterAdapter removes the Adapter for calendarID, stopping future syncs.
// Used when a calendar is deleted or sync is disabled.
func (e *Engine) UnregisterAdapter(calendarID int64) {
	e.mu.Lock()
	delete(e.adapters, calendarID)
	delete(e.statuses, calendarID)
	e.mu.Unlock()
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
	e.mu.RLock()
	a, ok := e.adapters[calendarID]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("syncer: no adapter for calendar %d", calendarID)
	}
	return e.syncOne(ctx, calendarID, a)
}

// SyncAll syncs every registered calendar sequentially.
// Returns the first error; sync continues for remaining calendars regardless.
func (e *Engine) SyncAll(ctx context.Context) error {
	e.mu.RLock()
	snapshot := make(map[int64]Adapter, len(e.adapters))
	maps.Copy(snapshot, e.adapters)
	e.mu.RUnlock()

	var firstErr error
	for calID, a := range snapshot {
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

// calLock returns the per-calendar mutex, creating it on first use. Holding it
// across a full syncOne guarantees the same calendar is never synced
// concurrently (e.g. a user "Sync Now" overlapping the background ticker),
// which would otherwise race on insert in applyChange.
func (e *Engine) calLock(calendarID int64) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	m, ok := e.syncLocks[calendarID]
	if !ok {
		m = &sync.Mutex{}
		e.syncLocks[calendarID] = m
	}
	return m
}

func (e *Engine) syncOne(ctx context.Context, calendarID int64, a Adapter) error {
	e.calLock(calendarID).Lock()
	defer e.calLock(calendarID).Unlock()

	e.setStatus(calendarID, SyncStatus{CalendarID: calendarID, InProgress: true})

	state, err := LoadSyncState(calendarID)
	if err != nil {
		e.setStatus(calendarID, SyncStatus{CalendarID: calendarID, LastError: err})
		return fmt.Errorf("syncer %d: load state: %w", calendarID, err)
	}

	// Push local changes first so our own writes are reflected on the remote
	// before we pull (and so the pull's etag check skips the echo).
	e.drainOutbox(ctx, calendarID, a, state)

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
	if err := api.SetCalendarLastSynced(calendarID, state.LastSyncAt.Unix()); err != nil {
		slog.Warn("syncer: record last_synced_at failed", "calendar", calendarID, "err", err)
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

// drainOutbox pushes every queued local change for calendarID to the remote,
// removing each entry on success and leaving failed ones for the next sync.
func (e *Engine) drainOutbox(ctx context.Context, calendarID int64, a Adapter, state *SyncState) {
	entries, err := api.PendingOutbox(calendarID)
	if err != nil {
		slog.Warn("syncer: load outbox failed", "calendar", calendarID, "err", err)
		return
	}
	for _, en := range entries {
		if err := pushOne(ctx, a, state, en); err != nil {
			slog.Warn("syncer: push failed", "op", en.Op, "url", en.ResourceURL, "err", err)
			continue // keep the entry for retry on the next sync
		}
		if err := api.DeleteOutboxEntry(en.ID); err != nil {
			slog.Warn("syncer: clear outbox entry failed", "id", en.ID, "err", err)
		}
	}
}

// pushOne sends a single queued change to the remote and, for a create, links
// the local event to the resource it now occupies.
func pushOne(ctx context.Context, a Adapter, state *SyncState, en api.OutboxEntry) error {
	switch en.Op {
	case api.OutboxDelete:
		if en.ResourceURL == "" {
			return nil // nothing to delete remotely
		}
		return a.DeleteRemote(ctx, state, en.ResourceURL)

	case api.OutboxUpsert:
		ev, err := api.GetEvent(en.EventID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil // event deleted locally since enqueue; drop
			}
			return err
		}
		res, err := a.PushChange(ctx, state, ev)
		if err != nil {
			return err
		}
		if res.ResourceURL != "" && (!ev.ResourceURL.Valid || ev.ResourceURL.String == "") {
			if err := api.SetEventResourceURL(ev.ID, res.ResourceURL); err != nil {
				return err
			}
		}
		if res.ResourceURL != "" && res.ETag != "" {
			state.SetETag(res.ResourceURL, res.ETag)
		}
		return nil
	}
	return nil
}

// applyChange merges one RemoteChange into the local database.
// On conflict (both local and remote changed since last sync), the conflict is
// recorded in the Conflict Queue and remote-wins is applied by default (ADR-0007).
func applyChange(_ context.Context, calendarID int64, ch RemoteChange, state *SyncState) error {
	// Skip a change we already hold at this ETag — e.g. our own push echoed
	// back by the next fetch — to avoid a spurious self-conflict.
	if ch.Type == ChangeUpsert && ch.ETag != "" && state.GetETag(ch.ResourceURL) == ch.ETag {
		return nil
	}

	if ch.Type == ChangeDelete {
		existing, err := api.GetEventByResourceURL(ch.ResourceURL)
		if err != nil {
			if errors.Is(err, api.ErrNotFound) {
				return nil // already deleted locally
			}
			return err
		}
		return api.DeleteEventFromRemote(existing.ID)
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
