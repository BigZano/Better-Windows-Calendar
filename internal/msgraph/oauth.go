package msgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"pycalendar/internal/credstore"
)

const tokenEndpoint = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// getAccessToken exchanges the stored refresh token for a short-lived access token.
// If the server returns a new refresh token (rotating tokens), it is stored back.
// Returns the access token as []byte so the caller can zero it after use (ADR-0004).
func (a *Adapter) getAccessToken(ctx context.Context) ([]byte, error) {
	tok, err := credstore.GetOAuthToken(a.calendarID)
	if err != nil {
		return nil, fmt.Errorf("msgraph: load token: %w", err)
	}
	defer tok.Zero()

	form := url.Values{
		"client_id":     {a.clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {string(tok.RefreshToken)},
		"scope":         {"Calendars.ReadWrite offline_access"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("msgraph: token request: %w", err)
	}
	defer resp.Body.Close()

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, fmt.Errorf("msgraph: decode token response: %w", err)
	}
	if tr.Error != "" {
		return nil, fmt.Errorf("msgraph: token error %q: %s", tr.Error, tr.ErrorDesc)
	}

	// Rotating refresh tokens: store the new one if it changed.
	if tr.RefreshToken != "" && tr.RefreshToken != string(tok.RefreshToken) {
		newTok := credstore.OAuthToken{
			RefreshToken: []byte(tr.RefreshToken),
			Scope:        tok.Scope,
			ObtainedAt:   time.Now(),
		}
		if storeErr := credstore.StoreOAuthToken(a.calendarID, newTok); storeErr != nil {
			slog.Warn("msgraph: failed to rotate refresh token",
				"calendar", a.calendarID, "err", storeErr)
		}
		newTok.Zero()
	}

	return []byte(tr.AccessToken), nil
}
