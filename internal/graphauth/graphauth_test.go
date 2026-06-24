package graphauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGeneratePKCE_ChallengeMatchesVerifier(t *testing.T) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}

	// verifier length is within RFC 7636 bounds (43–128).
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Errorf("verifier length %d outside [43,128]", len(verifier))
	}
	// verifier uses only unreserved base64url chars (no padding).
	for _, r := range verifier {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			t.Errorf("verifier contains disallowed char %q", r)
		}
	}

	// challenge == base64url-nopad(sha256(verifier))
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if challenge != want {
		t.Errorf("challenge: got %q, want %q", challenge, want)
	}
}

func TestGeneratePKCE_Unique(t *testing.T) {
	v1, _, _ := GeneratePKCE()
	v2, _, _ := GeneratePKCE()
	if v1 == v2 {
		t.Error("two GeneratePKCE calls returned the same verifier")
	}
}

func TestBuildAuthorizeURL_HasRequiredParams(t *testing.T) {
	cfg := Config{
		ClientID:     "client-123",
		AuthorizeURL: "https://idp.example/authorize",
		Scopes:       []string{"Calendars.ReadWrite", "offline_access"},
	}
	raw := buildAuthorizeURL(cfg, "http://127.0.0.1:5555/callback", "the-challenge", "the-state")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse authorize URL: %v", err)
	}
	q := u.Query()

	checks := map[string]string{
		"response_type":         "code",
		"client_id":             "client-123",
		"redirect_uri":          "http://127.0.0.1:5555/callback",
		"scope":                 "Calendars.ReadWrite offline_access",
		"code_challenge":        "the-challenge",
		"code_challenge_method": "S256",
		"state":                 "the-state",
		"prompt":                "select_account",
	}
	for k, want := range checks {
		if got := q.Get(k); got != want {
			t.Errorf("param %q: got %q, want %q", k, got, want)
		}
	}
}

// mockIdP serves an /authorize that 302-redirects back to the redirect_uri with
// a code, and a /token that validates the PKCE verifier and returns tokens. It
// records the verifier/challenge for assertions.
type mockIdP struct {
	t            *testing.T
	gotGrantType string
	gotVerifier  string
	gotChallenge string
	// overrides for negative-path tests
	stateOverride string
	tokenError    string
}

func (m *mockIdP) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		m.gotChallenge = q.Get("code_challenge")
		redirectURI := q.Get("redirect_uri")
		state := q.Get("state")
		if m.stateOverride != "" {
			state = m.stateOverride
		}
		dest := redirectURI + "?code=auth-code-xyz&state=" + url.QueryEscape(state)
		http.Redirect(w, r, dest, http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		m.gotGrantType = r.Form.Get("grant_type")
		m.gotVerifier = r.Form.Get("code_verifier")
		w.Header().Set("Content-Type", "application/json")
		if m.tokenError != "" {
			w.Write([]byte(`{"error":"` + m.tokenError + `","error_description":"bad"}`))
			return
		}
		w.Write([]byte(`{"access_token":"access-abc","refresh_token":"refresh-def"}`))
	})
	return mux
}

// browserOpener returns an OpenBrowser that fires the request asynchronously so
// it follows the /authorize redirect to the loopback callback. The default HTTP
// client follows 3xx automatically.
func browserOpener() func(string) error {
	return func(u string) error {
		go func() {
			resp, err := http.Get(u)
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
}

func TestLogin_HappyPath_ReturnsTokensAndValidatesPKCE(t *testing.T) {
	m := &mockIdP{t: t}
	srv := httptest.NewServer(m.handler())
	defer srv.Close()

	cfg := Config{
		ClientID:     "client-123",
		AuthorizeURL: srv.URL + "/authorize",
		TokenURL:     srv.URL + "/token",
		Scopes:       []string{"Calendars.ReadWrite", "offline_access"},
		OpenBrowser:  browserOpener(),
		Timeout:      10 * time.Second,
	}

	refresh, access, err := Login(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if string(refresh) != "refresh-def" {
		t.Errorf("refresh token: got %q, want %q", refresh, "refresh-def")
	}
	if string(access) != "access-abc" {
		t.Errorf("access token: got %q, want %q", access, "access-abc")
	}
	if m.gotGrantType != "authorization_code" {
		t.Errorf("grant_type: got %q, want authorization_code", m.gotGrantType)
	}

	// The verifier the token endpoint received must S256-hash to the challenge
	// the authorize endpoint saw.
	sum := sha256.Sum256([]byte(m.gotVerifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if wantChallenge != m.gotChallenge {
		t.Errorf("PKCE mismatch: S256(verifier)=%q, challenge=%q", wantChallenge, m.gotChallenge)
	}
}

func TestLogin_StateMismatch_Errors(t *testing.T) {
	m := &mockIdP{t: t, stateOverride: "tampered-state"}
	srv := httptest.NewServer(m.handler())
	defer srv.Close()

	cfg := Config{
		ClientID:     "client-123",
		AuthorizeURL: srv.URL + "/authorize",
		TokenURL:     srv.URL + "/token",
		Scopes:       []string{"Calendars.ReadWrite", "offline_access"},
		OpenBrowser:  browserOpener(),
		Timeout:      10 * time.Second,
	}

	_, _, err := Login(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error on state mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "state mismatch") {
		t.Errorf("error: got %v, want state mismatch", err)
	}
}

func TestLogin_TokenEndpointError_Errors(t *testing.T) {
	m := &mockIdP{t: t, tokenError: "invalid_grant"}
	srv := httptest.NewServer(m.handler())
	defer srv.Close()

	cfg := Config{
		ClientID:     "client-123",
		AuthorizeURL: srv.URL + "/authorize",
		TokenURL:     srv.URL + "/token",
		Scopes:       []string{"Calendars.ReadWrite", "offline_access"},
		OpenBrowser:  browserOpener(),
		Timeout:      10 * time.Second,
	}

	_, _, err := Login(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error on token endpoint error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Errorf("error: got %v, want invalid_grant", err)
	}
}

func TestLogin_AuthorizeError_Errors(t *testing.T) {
	// /authorize redirects back with an error param instead of a code.
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		dest := q.Get("redirect_uri") + "?error=access_denied&error_description=denied&state=" +
			url.QueryEscape(q.Get("state"))
		http.Redirect(w, r, dest, http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cfg := Config{
		ClientID:     "client-123",
		AuthorizeURL: srv.URL + "/authorize",
		TokenURL:     srv.URL + "/token",
		Scopes:       []string{"Calendars.ReadWrite", "offline_access"},
		OpenBrowser:  browserOpener(),
		Timeout:      10 * time.Second,
	}

	_, _, err := Login(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error on authorize error redirect, got nil")
	}
	if !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("error: got %v, want access_denied", err)
	}
}

func TestLogin_Timeout_Errors(t *testing.T) {
	// OpenBrowser never triggers the callback → the flow must time out.
	cfg := Config{
		ClientID:     "client-123",
		AuthorizeURL: "https://idp.invalid/authorize",
		TokenURL:     "https://idp.invalid/token",
		Scopes:       []string{"Calendars.ReadWrite", "offline_access"},
		OpenBrowser:  func(string) error { return nil }, // no-op: never calls back
		Timeout:      150 * time.Millisecond,
	}

	start := time.Now()
	_, _, err := Login(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error: got %v, want timed out", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("timeout took far longer than configured")
	}
}

func TestLogin_MissingClientID_Errors(t *testing.T) {
	_, _, err := Login(context.Background(), Config{
		AuthorizeURL: "https://x/authorize",
		TokenURL:     "https://x/token",
	})
	if err == nil {
		t.Fatal("expected error for empty ClientID")
	}
}
