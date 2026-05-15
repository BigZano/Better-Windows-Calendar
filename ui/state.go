package ui

import (
	"log/slog"
	"sync"

	"pycalendar/internal/config"
)

var (
	visibleMu       sync.RWMutex
	hiddenCalendars = map[int64]bool{}
)

// InitVisibilityFromConfig seeds the in-memory hidden-calendar set from persisted config.
func InitVisibilityFromConfig(hidden []int64) {
	visibleMu.Lock()
	defer visibleMu.Unlock()
	hiddenCalendars = make(map[int64]bool, len(hidden))
	for _, id := range hidden {
		hiddenCalendars[id] = true
	}
}

func isCalendarVisible(id int64) bool {
	visibleMu.RLock()
	defer visibleMu.RUnlock()
	return !hiddenCalendars[id]
}

func setCalendarVisible(id int64, visible bool) {
	visibleMu.Lock()
	if visible {
		delete(hiddenCalendars, id)
	} else {
		hiddenCalendars[id] = true
	}
	ids := make([]int64, 0, len(hiddenCalendars))
	for hid := range hiddenCalendars {
		ids = append(ids, hid)
	}
	visibleMu.Unlock()

	go func() {
		cfg, err := config.Load()
		if err != nil {
			slog.Warn("visibility persist: load config", "err", err)
			return
		}
		cfg.UI.HiddenCalendars = ids
		if err := config.Save(cfg); err != nil {
			slog.Warn("visibility persist: save config", "err", err)
		}
	}()
}
