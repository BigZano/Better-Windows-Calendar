package syncwire

import (
	"database/sql"
	"testing"

	"pycalendar/internal/api"
	"pycalendar/internal/caldav"
	"pycalendar/internal/config"
	"pycalendar/internal/msgraph"
	"pycalendar/internal/testutil"
)

func nullStr(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

func TestBuildAdapter(t *testing.T) {
	cfgWithClient := config.Config{}
	cfgWithClient.OAuth.MicrosoftClientID = "client-123"

	tests := []struct {
		name   string
		cal    api.Calendar
		cfg    config.Config
		wantOK bool
		assert func(t *testing.T, a any)
	}{
		{
			name:   "caldav with url",
			cal:    api.Calendar{ID: 2, Type: api.CalendarTypeCalDAV, SyncURL: nullStr("https://dav.example.com/cal/")},
			wantOK: true,
			assert: func(t *testing.T, a any) {
				if _, ok := a.(*caldav.Adapter); !ok {
					t.Fatalf("want *caldav.Adapter, got %T", a)
				}
			},
		},
		{
			name:   "caldav missing url",
			cal:    api.Calendar{ID: 3, Type: api.CalendarTypeCalDAV},
			wantOK: false,
		},
		{
			name:   "caldav empty url",
			cal:    api.Calendar{ID: 3, Type: api.CalendarTypeCalDAV, SyncURL: nullStr("")},
			wantOK: false,
		},
		{
			name:   "outlook with client id and url",
			cal:    api.Calendar{ID: 4, Type: api.CalendarTypeOutlook, SyncURL: nullStr("AAMkAG...")},
			cfg:    cfgWithClient,
			wantOK: true,
			assert: func(t *testing.T, a any) {
				if _, ok := a.(*msgraph.Adapter); !ok {
					t.Fatalf("want *msgraph.Adapter, got %T", a)
				}
			},
		},
		{
			name:   "outlook missing client id",
			cal:    api.Calendar{ID: 5, Type: api.CalendarTypeOutlook, SyncURL: nullStr("AAMkAG...")},
			wantOK: false, // no built-in default client id yet
		},
		{
			name:   "outlook missing graph calendar id",
			cal:    api.Calendar{ID: 6, Type: api.CalendarTypeOutlook},
			cfg:    cfgWithClient,
			wantOK: false,
		},
		{
			name:   "google deferred",
			cal:    api.Calendar{ID: 7, Type: api.CalendarTypeGoogle, SyncURL: nullStr("x")},
			wantOK: false,
		},
		{
			name:   "local",
			cal:    api.Calendar{ID: 1, Type: api.CalendarTypeLocal},
			wantOK: false,
		},
		{
			name:   "unknown type",
			cal:    api.Calendar{ID: 8, Type: "exchange"},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, ok := buildAdapter(tt.cal, tt.cfg)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				if a != nil {
					t.Fatalf("adapter should be nil when ok is false, got %T", a)
				}
				return
			}
			if tt.assert != nil {
				tt.assert(t, a)
			}
		})
	}
}

func TestBuildEngine_CountsOnlySyncableCalendars(t *testing.T) {
	testutil.NewTestDB(t) // seeds default Local calendar (id=1)

	// Enabled CalDAV calendar — should be registered.
	davID, err := api.CreateCalendar("Work", "#3B82F6", api.CalendarTypeCalDAV)
	if err != nil {
		t.Fatalf("create caldav calendar: %v", err)
	}
	if err := api.UpdateCalendar(davID, map[string]any{"sync_url": "https://dav.example.com/cal/"}); err != nil {
		t.Fatalf("set sync_url: %v", err)
	}

	// Disabled CalDAV calendar — should be skipped.
	offID, err := api.CreateCalendar("Archive", "#888888", api.CalendarTypeCalDAV)
	if err != nil {
		t.Fatalf("create disabled calendar: %v", err)
	}
	if err := api.UpdateCalendar(offID, map[string]any{
		"sync_url":     "https://dav.example.com/old/",
		"sync_enabled": 0,
	}); err != nil {
		t.Fatalf("disable calendar: %v", err)
	}

	eng, n, err := BuildEngine(config.Config{})
	if err != nil {
		t.Fatalf("BuildEngine: %v", err)
	}
	if eng == nil {
		t.Fatal("BuildEngine returned nil engine")
	}
	if n != 1 {
		t.Fatalf("registered adapters = %d, want 1 (only the enabled CalDAV calendar)", n)
	}
}
