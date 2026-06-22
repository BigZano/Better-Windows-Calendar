// Package msgraph implements syncer.Adapter for Microsoft Graph calendars.
// Authentication uses the OAuth refresh-token flow (ADR-0003, ADR-0004):
// the refresh token is loaded from CredentialStore on every operation; the
// access token is zeroed as soon as the HTTP call completes.
//
// Incremental sync uses Graph delta queries ($select + /delta endpoint).
// A 410 Gone response means the deltaLink expired; the adapter falls back to a
// full fetch and resets the SyncToken automatically.
package msgraph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"pycalendar/internal/api"
	"pycalendar/internal/syncer"
)

const defaultGraphBaseURL = "https://graph.microsoft.com/v1.0"

// Adapter is the Microsoft Graph implementation of syncer.Adapter.
// graphCalendarID is the opaque Graph calendar ID string (e.g. "AAMkAG...").
// clientID is the Azure app registration client ID; pass the value from
// config.OAuthConfig.MicrosoftClientID (no built-in default ships in this
// open-source repo).
type Adapter struct {
	calendarID      int64
	graphCalendarID string
	clientID        string
	client          *http.Client
	graphBaseURL    string
	tokenURL        string
}

// New returns a Graph Adapter. clientID must be set; an empty string will
// cause all token refreshes to fail with an OAuth error.
func New(calendarID int64, graphCalendarID, clientID string) *Adapter {
	return &Adapter{
		calendarID:      calendarID,
		graphCalendarID: graphCalendarID,
		clientID:        clientID,
		client:          &http.Client{Timeout: 30 * time.Second},
		graphBaseURL:    defaultGraphBaseURL,
		tokenURL:        tokenEndpoint,
	}
}

// ---- syncer.Adapter ----

// FetchChanges returns events that have changed or been deleted on the remote
// calendar since the last sync. An empty SyncToken triggers a full fetch via
// the delta endpoint; a stored deltaLink URL drives incremental fetches.
// A 410 Gone from Graph (expired deltaLink) resets the token and retries as a
// full fetch.
func (a *Adapter) FetchChanges(ctx context.Context, state *syncer.SyncState) ([]syncer.RemoteChange, error) {
	accessToken, err := a.getAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	defer zeroBytes(accessToken)

	if state.SyncToken == "" {
		return a.fullFetch(ctx, accessToken, state)
	}
	changes, err := a.deltaFetch(ctx, accessToken, state)
	if errors.Is(err, errDeltaExpired) {
		state.SyncToken = ""
		return a.fullFetch(ctx, accessToken, state)
	}
	return changes, err
}

// PushChange serialises e as a Graph JSON body and POSTs (new) or PATCHes (existing).
func (a *Adapter) PushChange(ctx context.Context, _ *syncer.SyncState, e api.Event) (syncer.PushResult, error) {
	accessToken, err := a.getAccessToken(ctx)
	if err != nil {
		return syncer.PushResult{}, err
	}
	defer zeroBytes(accessToken)

	body, err := eventToGraphBody(e)
	if err != nil {
		return syncer.PushResult{}, fmt.Errorf("msgraph: serialise event: %w", err)
	}

	existingURL := ""
	if e.ResourceURL.Valid {
		existingURL = e.ResourceURL.String
	}

	var method, reqURL string
	if existingURL != "" {
		method = http.MethodPatch
		reqURL = existingURL
	} else {
		method = http.MethodPost
		reqURL = a.graphBaseURL + "/me/calendars/" + a.graphCalendarID + "/events"
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(body))
	if err != nil {
		return syncer.PushResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+string(accessToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return syncer.PushResult{}, fmt.Errorf("msgraph: %s event: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return syncer.PushResult{}, fmt.Errorf("msgraph: %s event HTTP %d: %s", method, resp.StatusCode, truncate(string(b), 200))
	}

	// Parse the returned event so a create can be linked to its new resource;
	// a missing/!unparseable body is tolerated for an update we already have a
	// URL for.
	var created struct {
		ID   string `json:"id"`
		ETag string `json:"@odata.etag"`
	}
	respBody, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(respBody, &created)

	resourceURL := existingURL
	if resourceURL == "" && created.ID != "" {
		resourceURL = a.graphBaseURL + "/me/events/" + created.ID
	}
	return syncer.PushResult{ResourceURL: resourceURL, ETag: created.ETag}, nil
}

// DeleteRemote sends DELETE for the Graph event at resourceURL.
func (a *Adapter) DeleteRemote(ctx context.Context, _ *syncer.SyncState, resourceURL string) error {
	accessToken, err := a.getAccessToken(ctx)
	if err != nil {
		return err
	}
	defer zeroBytes(accessToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, resourceURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+string(accessToken))

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("msgraph: DELETE event: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("msgraph: DELETE event HTTP %d", resp.StatusCode)
	}
	return nil
}

// ---- fetch helpers ----

func (a *Adapter) fullFetch(ctx context.Context, accessToken []byte, state *syncer.SyncState) ([]syncer.RemoteChange, error) {
	startURL := a.graphBaseURL + "/me/calendars/" + a.graphCalendarID +
		"/events/delta?$select=id,subject,start,end,isAllDay,body,location,webLink,lastModifiedDateTime"
	return a.fetchPages(ctx, accessToken, startURL, state)
}

func (a *Adapter) deltaFetch(ctx context.Context, accessToken []byte, state *syncer.SyncState) ([]syncer.RemoteChange, error) {
	return a.fetchPages(ctx, accessToken, state.SyncToken, state)
}

// fetchPages follows @odata.nextLink pages until @odata.deltaLink is returned,
// saving the deltaLink as the new SyncToken.
func (a *Adapter) fetchPages(ctx context.Context, accessToken []byte, startURL string, state *syncer.SyncState) ([]syncer.RemoteChange, error) {
	var all []syncer.RemoteChange
	nextURL := startURL

	for nextURL != "" {
		page, deltaLink, next, err := a.fetchPage(ctx, accessToken, nextURL)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if deltaLink != "" {
			state.SyncToken = deltaLink
			break
		}
		nextURL = next
	}
	return all, nil
}

type deltaResponse struct {
	Value     []graphEvent `json:"value"`
	NextLink  string       `json:"@odata.nextLink"`
	DeltaLink string       `json:"@odata.deltaLink"`
}

// errDeltaExpired is returned when Graph responds 410 Gone (deltaLink expired).
var errDeltaExpired = errors.New("msgraph: deltaLink expired")

func (a *Adapter) fetchPage(ctx context.Context, accessToken []byte, pageURL string) ([]syncer.RemoteChange, string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+string(accessToken))

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("msgraph: GET delta: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", fmt.Errorf("msgraph: read delta body: %w", err)
	}

	if resp.StatusCode == http.StatusGone {
		return nil, "", "", errDeltaExpired
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("msgraph: delta HTTP %d: %s",
			resp.StatusCode, truncate(string(b), 200))
	}

	var dr deltaResponse
	if err := json.Unmarshal(b, &dr); err != nil {
		return nil, "", "", fmt.Errorf("msgraph: parse delta response: %w", err)
	}

	var changes []syncer.RemoteChange
	for _, ge := range dr.Value {
		resourceURL := a.graphBaseURL + "/me/events/" + ge.ID
		if ge.Removed != nil {
			changes = append(changes, syncer.RemoteChange{
				ResourceURL: resourceURL,
				Type:        syncer.ChangeDelete,
			})
			continue
		}
		ev, err := graphEventToEvent(ge)
		if err != nil {
			continue // skip unparseable events without failing the whole sync
		}
		changes = append(changes, syncer.RemoteChange{
			ResourceURL: resourceURL,
			Type:        syncer.ChangeUpsert,
			Event:       &ev,
		})
	}
	return changes, dr.DeltaLink, dr.NextLink, nil
}

// ---- helpers ----

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
