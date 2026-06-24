package msgraph

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListCalendars_HappyPath(t *testing.T) {
	var gotAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/me/calendars", func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"value":[
			{"id":"AAMkCal1","name":"Calendar"},
			{"id":"AAMkCal2","name":"Birthdays"}
		]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cals, err := ListCalendars(context.Background(), []byte("access-token-xyz"), srv.URL)
	if err != nil {
		t.Fatalf("ListCalendars: %v", err)
	}
	if gotAuth != "Bearer access-token-xyz" {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, "Bearer access-token-xyz")
	}
	if len(cals) != 2 {
		t.Fatalf("got %d calendars, want 2", len(cals))
	}
	if cals[0].ID != "AAMkCal1" || cals[0].Name != "Calendar" {
		t.Errorf("calendar[0]: got %+v", cals[0])
	}
	if cals[1].ID != "AAMkCal2" || cals[1].Name != "Birthdays" {
		t.Errorf("calendar[1]: got %+v", cals[1])
	}
}

func TestListCalendars_Non200_Errors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/me/calendars", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"code":"InvalidAuthenticationToken"}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := ListCalendars(context.Background(), []byte("bad"), srv.URL)
	if err == nil {
		t.Fatal("expected error on non-200, got nil")
	}
}

func TestListCalendars_EmptyList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/me/calendars", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"value":[]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cals, err := ListCalendars(context.Background(), []byte("tok"), srv.URL)
	if err != nil {
		t.Fatalf("ListCalendars: %v", err)
	}
	if len(cals) != 0 {
		t.Errorf("got %d calendars, want 0", len(cals))
	}
}
