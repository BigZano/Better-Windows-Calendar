package msgraph

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"pycalendar/internal/api"
	"pycalendar/internal/credstore"
	"pycalendar/internal/syncer"
	"pycalendar/internal/testutil"
)

const testCalID = int64(10)
const testGraphCalID = "AAMkTestCal"
const testClientID = "test-client-id"

// setupMsgraphCreds initialises in-memory DB + mock keyring + stores a
// refresh token for testCalID.
func setupMsgraphCreds(t *testing.T) {
	t.Helper()
	testutil.NewTestDB(t)
	keyring.MockInit()
	tok := credstore.OAuthToken{
		RefreshToken: []byte("test-refresh-token"),
		Scope:        "Calendars.ReadWrite offline_access",
		ObtainedAt:   time.Now(),
	}
	if err := credstore.StoreOAuthToken(testCalID, tok); err != nil {
		t.Fatalf("StoreOAuthToken: %v", err)
	}
}

// newTestAdapter creates an Adapter wired to the test server.
func newTestAdapter(srv *httptest.Server) *Adapter {
	return &Adapter{
		calendarID:      testCalID,
		graphCalendarID: testGraphCalID,
		clientID:        testClientID,
		client:          srv.Client(),
		graphBaseURL:    srv.URL,
		tokenURL:        srv.URL + "/token",
	}
}

// tokenHandler is an http.HandlerFunc that returns a fixed access token.
func tokenHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"access_token": "test-access-token",
	})
}

func TestMSGraphAdapter_FullFetch_ReturnsUpsertAndDelete(t *testing.T) {
	setupMsgraphCreds(t)

	var srvURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/me/calendars/"+testGraphCalID+"/events/delta", func(w http.ResponseWriter, r *http.Request) {
		deltaLink := srvURL + "/me/calendars/" + testGraphCalID + "/events/delta?$deltaToken=tok1"
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"value": []map[string]any{
				{
					"id":      "event-id-1",
					"subject": "Graph Meeting",
					"start":   map[string]string{"dateTime": "2026-06-01T14:00:00", "timeZone": "UTC"},
					"end":     map[string]string{"dateTime": "2026-06-01T15:00:00", "timeZone": "UTC"},
				},
				{
					"id":       "event-id-2",
					"@removed": map[string]string{"reason": "deleted"},
				},
			},
			"@odata.deltaLink": deltaLink,
		})
	})

	srv := httptest.NewServer(mux)
	srvURL = srv.URL
	defer srv.Close()

	a := newTestAdapter(srv)
	state := &syncer.SyncState{}

	changes, err := a.FetchChanges(t.Context(), state)
	if err != nil {
		t.Fatalf("FetchChanges: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("got %d changes, want 2", len(changes))
	}

	upsert := changes[0]
	if upsert.Type != syncer.ChangeUpsert {
		t.Errorf("first change type: got %v, want ChangeUpsert", upsert.Type)
	}
	if upsert.Event == nil || upsert.Event.Title != "Graph Meeting" {
		t.Errorf("event title: got %v", upsert.Event)
	}

	del := changes[1]
	if del.Type != syncer.ChangeDelete {
		t.Errorf("second change type: got %v, want ChangeDelete", del.Type)
	}

	// DeltaLink stored as SyncToken for next incremental fetch.
	if state.SyncToken == "" {
		t.Error("SyncToken not updated after full fetch")
	}
}

func TestMSGraphAdapter_DeltaFetch_UsesSyncToken(t *testing.T) {
	setupMsgraphCreds(t)

	deltaTokenPath := "/delta-token-endpoint"
	deltaFetchCalled := false
	var srvURL string

	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc(deltaTokenPath, func(w http.ResponseWriter, r *http.Request) {
		deltaFetchCalled = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"value":            []map[string]any{},
			"@odata.deltaLink": srvURL + deltaTokenPath,
		})
	})

	srv := httptest.NewServer(mux)
	srvURL = srv.URL
	defer srv.Close()

	a := newTestAdapter(srv)
	state := &syncer.SyncState{}
	state.SyncToken = srvURL + deltaTokenPath // existing delta URL

	_, err := a.FetchChanges(t.Context(), state)
	if err != nil {
		t.Fatalf("FetchChanges: %v", err)
	}
	if !deltaFetchCalled {
		t.Error("expected incremental fetch using SyncToken URL, but delta endpoint not called")
	}
}

func TestMSGraphAdapter_DeltaFetch_410Gone_RetriesFullFetch(t *testing.T) {
	setupMsgraphCreds(t)

	var srvURL string
	fullFetchCalled := false

	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/expired-delta", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	})
	mux.HandleFunc("/me/calendars/"+testGraphCalID+"/events/delta", func(w http.ResponseWriter, r *http.Request) {
		fullFetchCalled = true
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"value":            []map[string]any{},
			"@odata.deltaLink": srvURL + "/me/calendars/" + testGraphCalID + "/events/delta?$deltaToken=fresh",
		})
	})

	srv := httptest.NewServer(mux)
	srvURL = srv.URL
	defer srv.Close()

	a := newTestAdapter(srv)
	state := &syncer.SyncState{}
	state.SyncToken = srvURL + "/expired-delta"

	_, err := a.FetchChanges(t.Context(), state)
	if err != nil {
		t.Fatalf("FetchChanges: %v", err)
	}
	if !fullFetchCalled {
		t.Error("expected fallback to full fetch after 410 Gone")
	}
}

func TestMSGraphAdapter_PushChange_NewEvent_SendsPOST(t *testing.T) {
	setupMsgraphCreds(t)

	var gotMethod string
	var gotBody map[string]any

	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/me/calendars/"+testGraphCalID+"/events", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := newTestAdapter(srv)
	ev := api.Event{
		Title:   "New Graph Event",
		StartTS: time.Date(2026, 6, 1, 14, 0, 0, 0, time.UTC).Unix(),
	}
	if err := a.PushChange(t.Context(), &syncer.SyncState{}, ev); err != nil {
		t.Fatalf("PushChange: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method: got %q, want POST", gotMethod)
	}
	if subj, _ := gotBody["subject"].(string); subj != "New Graph Event" {
		t.Errorf("subject: got %q, want %q", subj, "New Graph Event")
	}
}

func TestMSGraphAdapter_PushChange_ExistingEvent_SendsPATCH(t *testing.T) {
	setupMsgraphCreds(t)

	var gotMethod string

	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/me/events/existing-event-id", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := newTestAdapter(srv)
	ev := api.Event{
		Title:   "Updated Event",
		StartTS: time.Date(2026, 6, 1, 14, 0, 0, 0, time.UTC).Unix(),
	}
	ev.ResourceURL.Valid = true
	ev.ResourceURL.String = srv.URL + "/me/events/existing-event-id"

	if err := a.PushChange(t.Context(), &syncer.SyncState{}, ev); err != nil {
		t.Fatalf("PushChange: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method: got %q, want PATCH", gotMethod)
	}
}

func TestMSGraphAdapter_DeleteRemote_SendsDELETE(t *testing.T) {
	setupMsgraphCreds(t)

	var gotMethod string

	mux := http.NewServeMux()
	mux.HandleFunc("/token", tokenHandler)
	mux.HandleFunc("/me/events/del-event-id", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := newTestAdapter(srv)
	resourceURL := srv.URL + "/me/events/del-event-id"

	if err := a.DeleteRemote(t.Context(), &syncer.SyncState{}, resourceURL); err != nil {
		t.Fatalf("DeleteRemote: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method: got %q, want DELETE", gotMethod)
	}
}

func TestMSGraphAdapter_TokenRotation_StoresNewRefreshToken(t *testing.T) {
	setupMsgraphCreds(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"access_token":  "new-access-token",
			"refresh_token": "rotated-refresh-token",
		})
	})
	// No /me/... handler needed — test only exercises the token exchange.
	mux.HandleFunc("/me/calendars/"+testGraphCalID+"/events", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := newTestAdapter(srv)
	ev := api.Event{
		Title:   "Rotation Test",
		StartTS: time.Now().Unix(),
	}
	if err := a.PushChange(t.Context(), &syncer.SyncState{}, ev); err != nil {
		t.Fatalf("PushChange: %v", err)
	}

	// Rotated token should be retrievable from credstore.
	stored, err := credstore.GetOAuthToken(testCalID)
	if err != nil {
		t.Fatalf("GetOAuthToken after rotation: %v", err)
	}
	defer stored.Zero()
	if !strings.Contains(string(stored.RefreshToken), "rotated") {
		t.Errorf("rotated token not stored: got %q", stored.RefreshToken)
	}
}
