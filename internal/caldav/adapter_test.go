package caldav

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"pycalendar/internal/api"
	"pycalendar/internal/credstore"
	"pycalendar/internal/syncer"
	"pycalendar/internal/testutil"
)

const testCalendarID = int64(1)

// testICS is minimal valid VCALENDAR with one event.
const testICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:test-1@local\r\nSUMMARY:Test Meeting\r\n" +
	"DTSTART:20260601T140000Z\r\nDTEND:20260601T150000Z\r\n" +
	"END:VEVENT\r\nEND:VCALENDAR\r\n"

// multistatusXML returns a WebDAV multistatus body with one event entry.
func multistatusXML(href, etag, calData string) string {
	cal := ""
	if calData != "" {
		cal = fmt.Sprintf(`<C:calendar-data xmlns:C="urn:ietf:params:xml:ns:caldav">%s</C:calendar-data>`, calData)
	}
	return fmt.Sprintf(`<?xml version="1.0"?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop><D:getetag>%s</D:getetag>%s</D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`, href, etag, cal)
}

// setupCreds registers mock keyring + test DB and stores CalDAV credentials.
func setupCreds(t *testing.T) {
	t.Helper()
	testutil.NewTestDB(t)
	keyring.MockInit()
	if err := credstore.StoreCalDAV(testCalendarID, "testuser", "testpass"); err != nil {
		t.Fatalf("StoreCalDAV: %v", err)
	}
}

func TestCalDAVAdapter_FullFetch_ReturnsUpsertChanges(t *testing.T) {
	setupCreds(t)

	eventPath := "/calendars/test/event1.ics"
	etag := `"etag-abc"`

	mux := http.NewServeMux()
	mux.HandleFunc("/calendars/test/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, multistatusXML(eventPath, etag, ""))
		case "REPORT":
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "calendar-multiget") {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusMultiStatus)
				fmt.Fprint(w, multistatusXML(eventPath, etag, testICS))
			}
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(testCalendarID, srv.URL+"/calendars/test/")
	a.client = srv.Client()

	state := &syncer.SyncState{}
	changes, err := a.FetchChanges(t.Context(), state)
	if err != nil {
		t.Fatalf("FetchChanges: %v", err)
	}

	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(changes))
	}
	ch := changes[0]
	if ch.Type != syncer.ChangeUpsert {
		t.Errorf("change type: got %v, want ChangeUpsert", ch.Type)
	}
	if ch.Event == nil {
		t.Fatal("change event is nil")
	}
	if ch.Event.Title != "Test Meeting" {
		t.Errorf("title: got %q, want %q", ch.Event.Title, "Test Meeting")
	}
}

func TestCalDAVAdapter_FullFetch_SkipsUnchangedETags(t *testing.T) {
	setupCreds(t)

	etag := `"etag-abc"`
	mux := http.NewServeMux()
	propfindCalled := 0
	multigetCalled := 0
	mux.HandleFunc("/calendars/test/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			propfindCalled++
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, multistatusXML("/calendars/test/event1.ics", etag, ""))
		case "REPORT":
			multigetCalled++
			w.WriteHeader(http.StatusMethodNotAllowed)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(testCalendarID, srv.URL+"/calendars/test/")
	a.client = srv.Client()

	// State already has the current ETag — nothing new to fetch.
	state := &syncer.SyncState{}
	state.SetETag(srv.URL+"/calendars/test/event1.ics", stripQuotes(etag))

	changes, err := a.FetchChanges(t.Context(), state)
	if err != nil {
		t.Fatalf("FetchChanges: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("got %d changes, want 0 (nothing new)", len(changes))
	}
	if multigetCalled != 0 {
		t.Errorf("multiget called %d times, want 0", multigetCalled)
	}
}

func TestCalDAVAdapter_IncrementalFetch_SyncCollection(t *testing.T) {
	setupCreds(t)

	eventPath := "/calendars/test/event2.ics"
	etag := `"etag-xyz"`
	newToken := "https://example.com/sync-token-2"

	syncCollectionResponse := fmt.Sprintf(`<?xml version="1.0"?>
<D:multistatus xmlns:D="DAV:">
  <D:sync-token>%s</D:sync-token>
  <D:response>
    <D:href>%s</D:href>
    <D:propstat>
      <D:prop><D:getetag>%s</D:getetag></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`, newToken, eventPath, etag)

	mux := http.NewServeMux()
	mux.HandleFunc("/calendars/test/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "REPORT" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		bs := string(body)
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusMultiStatus)
		if strings.Contains(bs, "sync-collection") {
			fmt.Fprint(w, syncCollectionResponse)
		} else if strings.Contains(bs, "calendar-multiget") {
			fmt.Fprint(w, multistatusXML(eventPath, etag, testICS))
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(testCalendarID, srv.URL+"/calendars/test/")
	a.client = srv.Client()

	state := &syncer.SyncState{}
	state.SyncToken = "https://example.com/sync-token-1" // non-empty → incremental path

	changes, err := a.FetchChanges(t.Context(), state)
	if err != nil {
		t.Fatalf("FetchChanges: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d changes, want 1", len(changes))
	}
	if state.SyncToken != newToken {
		t.Errorf("SyncToken not updated: got %q, want %q", state.SyncToken, newToken)
	}
}

func TestCalDAVAdapter_IncrementalFetch_422FallsBackToFullFetch(t *testing.T) {
	setupCreds(t)

	eventPath := "/calendars/test/event3.ics"
	etag := `"etag-new"`
	reportCalls := 0

	mux := http.NewServeMux()
	mux.HandleFunc("/calendars/test/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "REPORT":
			reportCalls++
			body, _ := io.ReadAll(r.Body)
			bs := string(body)
			if strings.Contains(bs, "sync-collection") {
				// Simulate expired/unsupported sync token.
				w.WriteHeader(http.StatusUnprocessableEntity)
				return
			}
			// Second call is multiget fallback.
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, multistatusXML(eventPath, etag, testICS))
		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			fmt.Fprint(w, multistatusXML(eventPath, etag, ""))
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(testCalendarID, srv.URL+"/calendars/test/")
	a.client = srv.Client()

	state := &syncer.SyncState{}
	state.SyncToken = "expired-token"

	changes, err := a.FetchChanges(t.Context(), state)
	if err != nil {
		t.Fatalf("FetchChanges: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("got %d changes after fallback, want 1", len(changes))
	}
	// SyncToken should have been reset by fallback.
	if state.SyncToken != "" {
		t.Errorf("SyncToken should be empty after fallback reset, got %q", state.SyncToken)
	}
}

func TestCalDAVAdapter_PushChange_NewEvent_SendsPUT(t *testing.T) {
	setupCreds(t)

	var gotMethod, gotPath string
	var gotContentType string

	mux := http.NewServeMux()
	mux.HandleFunc("/calendars/test/", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusCreated)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(testCalendarID, srv.URL+"/calendars/test/")
	a.client = srv.Client()

	ev := makeEvent("New Event", 1)
	state := &syncer.SyncState{}

	res, err := a.PushChange(t.Context(), state, ev)
	if err != nil {
		t.Fatalf("PushChange: %v", err)
	}

	if gotMethod != http.MethodPut {
		t.Errorf("method: got %q, want PUT", gotMethod)
	}
	if !strings.HasSuffix(gotPath, ".ics") {
		t.Errorf("path should end with .ics, got %q", gotPath)
	}
	// A create must report the resource URL it PUT to, so the engine can link
	// the local event.
	if !strings.HasPrefix(res.ResourceURL, srv.URL+"/calendars/test/") || !strings.HasSuffix(res.ResourceURL, ".ics") {
		t.Errorf("ResourceURL: got %q, want the PUT target under the calendar URL", res.ResourceURL)
	}
	if !strings.Contains(gotContentType, "text/calendar") {
		t.Errorf("Content-Type: got %q, want text/calendar", gotContentType)
	}
}

func TestCalDAVAdapter_PushChange_ExistingEvent_SendsIfMatch(t *testing.T) {
	setupCreds(t)

	var gotIfMatch string

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gotIfMatch = r.Header.Get("If-Match")
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(testCalendarID, srv.URL+"/calendars/test/")
	a.client = srv.Client()

	ev := makeEvent("Existing Event", 5)
	ev.ResourceURL.Valid = true
	ev.ResourceURL.String = srv.URL + "/calendars/test/event5.ics"

	state := &syncer.SyncState{}
	state.SetETag(ev.ResourceURL.String, `"stored-etag"`)

	if _, err := a.PushChange(t.Context(), state, ev); err != nil {
		t.Fatalf("PushChange: %v", err)
	}
	if gotIfMatch != `"stored-etag"` {
		t.Errorf("If-Match: got %q, want %q", gotIfMatch, `"stored-etag"`)
	}
}

func TestCalDAVAdapter_PushChange_ResponseETag_StoredInState(t *testing.T) {
	setupCreds(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/calendars/test/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"server-etag-v2"`)
		w.WriteHeader(http.StatusCreated)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(testCalendarID, srv.URL+"/calendars/test/")
	a.client = srv.Client()

	ev := makeEvent("Event With ETag", 2)
	state := &syncer.SyncState{}

	if _, err := a.PushChange(t.Context(), state, ev); err != nil {
		t.Fatalf("PushChange: %v", err)
	}

	// The resource URL for a new event is calendarURL + UID + ".ics"
	uid := fmt.Sprintf("pycalendar-%d@local", ev.ID)
	resourceURL := srv.URL + "/calendars/test/" + uid + ".ics"
	if got := state.GetETag(resourceURL); got != "server-etag-v2" {
		t.Errorf("stored ETag: got %q, want %q", got, "server-etag-v2")
	}
}

func TestCalDAVAdapter_DeleteRemote_SendsIfMatch(t *testing.T) {
	setupCreds(t)

	var gotMethod, gotIfMatch string

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotIfMatch = r.Header.Get("If-Match")
		w.WriteHeader(http.StatusNoContent)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(testCalendarID, srv.URL+"/calendars/test/")
	a.client = srv.Client()

	resourceURL := srv.URL + "/calendars/test/delete-me.ics"
	state := &syncer.SyncState{}
	state.SetETag(resourceURL, `"del-etag"`)

	if err := a.DeleteRemote(t.Context(), state, resourceURL); err != nil {
		t.Fatalf("DeleteRemote: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method: got %q, want DELETE", gotMethod)
	}
	if gotIfMatch != `"del-etag"` {
		t.Errorf("If-Match: got %q, want %q", gotIfMatch, `"del-etag"`)
	}
}

// makeEvent builds a minimal api.Event for push/delete tests.
func makeEvent(title string, id int64) api.Event {
	return api.Event{
		ID:      id,
		Title:   title,
		StartTS: 1748736000, // 2026-06-01 00:00:00 UTC
	}
}

func TestCalDAVAdapter_DeleteRemote_HTTPError_ReturnsError(t *testing.T) {
	setupCreds(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	a := New(testCalendarID, srv.URL+"/cal/")
	a.client = srv.Client()

	err := a.DeleteRemote(t.Context(), &syncer.SyncState{}, srv.URL+"/cal/gone.ics")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}
