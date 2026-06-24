package msgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Calendar is a single Outlook calendar discovered via the Graph API.
// ID is the opaque Graph calendar ID (stored as the Calendar's sync_url for
// Outlook); Name is its display name.
type Calendar struct {
	ID   string
	Name string
}

// ListCalendars returns the signed-in user's Outlook calendars by GETting
// {graphBaseURL}/me/calendars with the supplied bearer access token. graphBaseURL
// is injectable so tests can point it at an httptest server; pass
// defaultGraphBaseURL in production.
func ListCalendars(ctx context.Context, accessToken []byte, graphBaseURL string) ([]Calendar, error) {
	if graphBaseURL == "" {
		graphBaseURL = defaultGraphBaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, graphBaseURL+"/me/calendars", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+string(accessToken))

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("msgraph: list calendars: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("msgraph: read calendars body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("msgraph: list calendars HTTP %d: %s", resp.StatusCode, truncate(string(b), 200))
	}

	var payload struct {
		Value []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"value"`
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil, fmt.Errorf("msgraph: parse calendars response: %w", err)
	}

	cals := make([]Calendar, 0, len(payload.Value))
	for _, v := range payload.Value {
		cals = append(cals, Calendar{ID: v.ID, Name: v.Name})
	}
	return cals, nil
}
