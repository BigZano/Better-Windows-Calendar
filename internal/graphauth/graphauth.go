// Package graphauth implements the initial Microsoft Graph OAuth login that
// obtains the first refresh token for an Outlook calendar. It uses the RFC 8252
// loopback redirect + PKCE (S256) flow: a 127.0.0.1-only listener on an
// ephemeral port receives the authorization code, which is then exchanged for
// tokens (ADR-0008).
//
// Everything is injectable so the whole flow is testable against an httptest
// mock identity provider: the authorize/token endpoints and the browser-opener
// are all fields on Config. No live Azure app is required.
//
// Token bytes returned by Login are []byte (not string) so callers can zero
// them after storing the refresh token (ADR-0004).
package graphauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// defaultTimeout bounds how long Login waits for the user to complete the
// browser sign-in before giving up.
const defaultTimeout = 3 * time.Minute

// Config configures a Login. AuthorizeURL and TokenURL are injectable so tests
// can point them at a mock IdP; OpenBrowser is injectable so tests can drive
// the callback without a real browser.
type Config struct {
	ClientID     string
	AuthorizeURL string
	TokenURL     string
	Scopes       []string
	// OpenBrowser launches the user's browser at url. Nil defaults to a real
	// platform opener (see openBrowser).
	OpenBrowser func(url string) error
	// Timeout bounds the whole flow. Zero defaults to defaultTimeout.
	Timeout time.Duration
}

// GeneratePKCE returns a PKCE verifier and its S256 challenge. The verifier is a
// 43-character URL-safe (unreserved) string drawn from crypto/rand; the
// challenge is base64url-no-padding(SHA256(verifier)) per RFC 7636.
func GeneratePKCE() (verifier, challenge string, err error) {
	// 32 random bytes → 43 base64url chars, within the 43–128 RFC 7636 range.
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("graphauth: read random for PKCE verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// randomState returns a crypto/rand URL-safe string for the OAuth state param.
func randomState() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("graphauth: read random for state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// callbackResult carries what the loopback handler captured from the redirect.
type callbackResult struct {
	code string
	err  error
}

// Login runs the loopback + PKCE authorization-code flow and returns the
// refresh and access tokens as []byte. The caller is responsible for zeroing
// them once stored/used (ADR-0004).
func Login(ctx context.Context, cfg Config) (refreshToken []byte, accessToken []byte, err error) {
	if cfg.ClientID == "" {
		return nil, nil, errors.New("graphauth: ClientID is required")
	}
	if cfg.AuthorizeURL == "" || cfg.TokenURL == "" {
		return nil, nil, errors.New("graphauth: AuthorizeURL and TokenURL are required")
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	open := cfg.OpenBrowser
	if open == nil {
		open = openBrowser
	}

	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, nil, err
	}
	defer zeroString(&verifier)

	state, err := randomState()
	if err != nil {
		return nil, nil, err
	}

	// Listen on an ephemeral loopback port. 127.0.0.1 (not 0.0.0.0) avoids the
	// Windows firewall prompt and exposes nothing off-machine (ADR-0008).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, nil, fmt.Errorf("graphauth: open loopback listener: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	resultCh := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if gotState := q.Get("state"); gotState != state {
			writeHTML(w, false, "State mismatch — the sign-in response could not be verified. Please try again.")
			select {
			case resultCh <- callbackResult{err: errors.New("graphauth: state mismatch in callback")}:
			default:
			}
			return
		}
		if e := q.Get("error"); e != "" {
			desc := q.Get("error_description")
			writeHTML(w, false, "Sign-in failed: "+e)
			select {
			case resultCh <- callbackResult{err: fmt.Errorf("graphauth: authorize error %q: %s", e, desc)}:
			default:
			}
			return
		}
		code := q.Get("code")
		if code == "" {
			writeHTML(w, false, "Sign-in failed: no authorization code was returned.")
			select {
			case resultCh <- callbackResult{err: errors.New("graphauth: no code in callback")}:
			default:
			}
			return
		}
		writeHTML(w, true, "Connected to Outlook. You can close this tab and return to PyCalendar.")
		select {
		case resultCh <- callbackResult{code: code}:
		default:
		}
	})

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }() // returns ErrServerClosed on Shutdown
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	authURL := buildAuthorizeURL(cfg, redirectURI, challenge, state)
	if err := open(authURL); err != nil {
		return nil, nil, fmt.Errorf("graphauth: open browser: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var code string
	select {
	case <-waitCtx.Done():
		return nil, nil, fmt.Errorf("graphauth: timed out waiting for sign-in: %w", waitCtx.Err())
	case res := <-resultCh:
		if res.err != nil {
			return nil, nil, res.err
		}
		code = res.code
	}

	refresh, access, err := exchangeCode(ctx, cfg, code, verifier, redirectURI)
	if err != nil {
		return nil, nil, err
	}
	return refresh, access, nil
}

// buildAuthorizeURL constructs the authorize URL with the PKCE challenge,
// state, redirect_uri and space-joined scopes.
func buildAuthorizeURL(cfg Config, redirectURI, challenge, state string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {cfg.ClientID},
		"redirect_uri":          {redirectURI},
		"scope":                 {strings.Join(cfg.Scopes, " ")},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"prompt":                {"select_account"},
	}
	sep := "?"
	if strings.Contains(cfg.AuthorizeURL, "?") {
		sep = "&"
	}
	return cfg.AuthorizeURL + sep + q.Encode()
}

// tokenResponse mirrors the OAuth token endpoint payload.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// exchangeCode POSTs the authorization_code grant and returns the refresh and
// access tokens as []byte.
func exchangeCode(ctx context.Context, cfg Config, code, verifier, redirectURI string) (refreshToken, accessToken []byte, err error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"client_id":     {cfg.ClientID},
		"code_verifier": {verifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("graphauth: token request: %w", err)
	}
	defer resp.Body.Close()

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, nil, fmt.Errorf("graphauth: decode token response: %w", err)
	}
	if tr.Error != "" {
		return nil, nil, fmt.Errorf("graphauth: token error %q: %s", tr.Error, tr.ErrorDesc)
	}
	if tr.RefreshToken == "" {
		return nil, nil, errors.New("graphauth: token response had no refresh_token (is offline_access in scope?)")
	}
	return []byte(tr.RefreshToken), []byte(tr.AccessToken), nil
}

// writeHTML renders a minimal success/error page for the browser tab.
func writeHTML(w http.ResponseWriter, ok bool, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title := "PyCalendar"
	color := "#16a34a"
	if !ok {
		color = "#dc2626"
	}
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>%s</title></head>`+
		`<body style="font-family:sans-serif;text-align:center;padding-top:4rem">`+
		`<h2 style="color:%s">%s</h2><p>%s</p></body></html>`,
		title, color, title, message)
}

// openBrowser is the default OpenBrowser implementation. Tests inject their own.
func openBrowser(rawURL string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	case "darwin":
		return exec.Command("open", rawURL).Start()
	default:
		return exec.Command("xdg-open", rawURL).Start()
	}
}

// zeroString overwrites the backing bytes of *s where possible and clears the
// reference. Go strings are immutable so this is best-effort; the primary
// defence remains never logging the verifier and dropping it quickly.
func zeroString(s *string) {
	*s = ""
}
