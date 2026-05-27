// Package caldav implements syncer.Adapter for CalDAV servers (Nextcloud,
// Fastmail, iCloud via app-passwords). Uses Basic Auth; credentials are
// loaded from CredentialStore on every operation so rotations take effect
// without restarting the app.
package caldav

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"pycalendar/internal/api"
	"pycalendar/internal/credstore"
	"pycalendar/internal/syncer"
)

// Adapter is the CalDAV implementation of syncer.Adapter.
// First sync (empty SyncToken): PROPFIND + calendar-multiget.
// Subsequent syncs: sync-collection REPORT with automatic fallback to full
// fetch if the server doesn't support it or the token has expired.
type Adapter struct {
	calendarID  int64
	calendarURL string // always ends with "/"
	client      *http.Client
}

// New returns a CalDAV Adapter. calendarURL is the full URL of the calendar
// collection (trailing slash added automatically if missing).
func New(calendarID int64, calendarURL string) *Adapter {
	if !strings.HasSuffix(calendarURL, "/") {
		calendarURL += "/"
	}
	return &Adapter{
		calendarID:  calendarID,
		calendarURL: calendarURL,
		client:      &http.Client{Timeout: 30 * time.Second},
	}
}

// ---- syncer.Adapter ----

// FetchChanges returns events that have changed or been deleted on the remote
// calendar since the last sync.
func (a *Adapter) FetchChanges(ctx context.Context, state *syncer.SyncState) ([]syncer.RemoteChange, error) {
	user, pass, err := credstore.GetCalDAV(a.calendarID)
	if err != nil {
		return nil, fmt.Errorf("caldav: credentials: %w", err)
	}
	if state.SyncToken == "" {
		return a.fullFetch(ctx, state, user, pass)
	}
	return a.incrementalFetch(ctx, state, user, pass)
}

// PushChange serialises e as iCalendar and PUTs it to the remote calendar.
// Uses If-Match on update to catch concurrent edits.
func (a *Adapter) PushChange(ctx context.Context, state *syncer.SyncState, e api.Event) error {
	user, pass, err := credstore.GetCalDAV(a.calendarID)
	if err != nil {
		return fmt.Errorf("caldav: credentials: %w", err)
	}

	icsData, err := eventToICS(e)
	if err != nil {
		return fmt.Errorf("caldav: serialise event: %w", err)
	}

	resourceURL := e.ResourceURL.String
	if !e.ResourceURL.Valid || resourceURL == "" {
		resourceURL = a.calendarURL + eventUID(e) + ".ics"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, resourceURL, bytes.NewReader(icsData))
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Content-Type", "text/calendar; charset=utf-8")
	if etag := state.GetETag(resourceURL); etag != "" {
		req.Header.Set("If-Match", etag)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("caldav: PUT %s: %w", resourceURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("caldav: PUT %s: HTTP %d", resourceURL, resp.StatusCode)
	}
	if newETag := resp.Header.Get("ETag"); newETag != "" {
		state.SetETag(resourceURL, stripQuotes(newETag))
	}
	return nil
}

// DeleteRemote removes the resource at resourceURL, sending If-Match if we
// have a stored ETag.
func (a *Adapter) DeleteRemote(ctx context.Context, state *syncer.SyncState, resourceURL string) error {
	user, pass, err := credstore.GetCalDAV(a.calendarID)
	if err != nil {
		return fmt.Errorf("caldav: credentials: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, resourceURL, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(user, pass)
	if etag := state.GetETag(resourceURL); etag != "" {
		req.Header.Set("If-Match", etag)
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("caldav: DELETE %s: %w", resourceURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("caldav: DELETE %s: HTTP %d", resourceURL, resp.StatusCode)
	}
	return nil
}

// ---- full fetch: PROPFIND + calendar-multiget ----

func (a *Adapter) fullFetch(ctx context.Context, state *syncer.SyncState, user, pass string) ([]syncer.RemoteChange, error) {
	props, err := a.propfind(ctx, user, pass)
	if err != nil {
		return nil, err
	}

	var toFetch []string
	for _, r := range props {
		if !strings.HasSuffix(strings.ToLower(r.Href), ".ics") {
			continue
		}
		if state.GetETag(r.Href) != r.ETag {
			toFetch = append(toFetch, r.Href)
		}
	}
	if len(toFetch) == 0 {
		return nil, nil
	}
	return a.multiget(ctx, state, toFetch, user, pass)
}

// ---- incremental fetch: sync-collection REPORT ----

func (a *Adapter) incrementalFetch(ctx context.Context, state *syncer.SyncState, user, pass string) ([]syncer.RemoteChange, error) {
	body := `<?xml version="1.0" encoding="utf-8"?>` +
		`<D:sync-collection xmlns:D="DAV:">` +
		`<D:sync-token>` + xmlEscape(state.SyncToken) + `</D:sync-token>` +
		`<D:sync-level>1</D:sync-level>` +
		`<D:prop><D:getetag/></D:prop>` +
		`</D:sync-collection>`

	req, err := http.NewRequestWithContext(ctx, "REPORT", a.calendarURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", "1")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("caldav: sync-collection: %w", err)
	}
	defer resp.Body.Close()

	// Fall back to full fetch if sync-collection is unsupported or token expired.
	if resp.StatusCode == http.StatusBadRequest ||
		resp.StatusCode == http.StatusForbidden ||
		resp.StatusCode == http.StatusUnprocessableEntity {
		state.SyncToken = ""
		return a.fullFetch(ctx, state, user, pass)
	}
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("caldav: sync-collection: HTTP %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var ms multistatus
	if err := xml.Unmarshal(b, &ms); err != nil {
		return nil, fmt.Errorf("caldav: parse sync-collection: %w", err)
	}
	if ms.SyncToken != "" {
		state.SyncToken = ms.SyncToken
	}

	var toFetch []string
	var changes []syncer.RemoteChange

	for _, r := range ms.Responses {
		href := a.absURL(r.Href)
		if r.isDelete() {
			changes = append(changes, syncer.RemoteChange{
				ResourceURL: href,
				Type:        syncer.ChangeDelete,
			})
			continue
		}
		etag := r.firstETag()
		if state.GetETag(href) != etag {
			toFetch = append(toFetch, href)
		}
	}

	if len(toFetch) > 0 {
		more, err := a.multiget(ctx, state, toFetch, user, pass)
		if err != nil {
			return nil, err
		}
		changes = append(changes, more...)
	}
	return changes, nil
}

// ---- HTTP helpers ----

func (a *Adapter) propfind(ctx context.Context, user, pass string) ([]davResource, error) {
	body := `<?xml version="1.0" encoding="utf-8"?>` +
		`<D:propfind xmlns:D="DAV:">` +
		`<D:prop><D:getetag/><D:getcontenttype/></D:prop>` +
		`</D:propfind>`

	req, err := http.NewRequestWithContext(ctx, "PROPFIND", a.calendarURL, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", "1")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("caldav: PROPFIND: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("caldav: PROPFIND: HTTP %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var ms multistatus
	if err := xml.Unmarshal(b, &ms); err != nil {
		return nil, fmt.Errorf("caldav: parse PROPFIND: %w", err)
	}

	var resources []davResource
	for _, r := range ms.Responses {
		if r.isDelete() {
			continue
		}
		resources = append(resources, davResource{
			Href: a.absURL(r.Href),
			ETag: r.firstETag(),
		})
	}
	return resources, nil
}

// multiget issues a calendar-multiget REPORT to fetch ICS content for hrefs.
func (a *Adapter) multiget(ctx context.Context, state *syncer.SyncState, hrefs []string, user, pass string) ([]syncer.RemoteChange, error) {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="utf-8"?>`)
	sb.WriteString(`<C:calendar-multiget xmlns:D="DAV:" xmlns:C="urn:ietf:params:xml:ns:caldav">`)
	sb.WriteString(`<D:prop><D:getetag/><C:calendar-data/></D:prop>`)
	for _, h := range hrefs {
		sb.WriteString(`<D:href>`)
		sb.WriteString(xmlEscape(h))
		sb.WriteString(`</D:href>`)
	}
	sb.WriteString(`</C:calendar-multiget>`)

	req, err := http.NewRequestWithContext(ctx, "REPORT", a.calendarURL, strings.NewReader(sb.String()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, pass)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("Depth", "1")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("caldav: calendar-multiget: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("caldav: calendar-multiget: HTTP %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var ms multistatus
	if err := xml.Unmarshal(b, &ms); err != nil {
		return nil, fmt.Errorf("caldav: parse calendar-multiget: %w", err)
	}

	var changes []syncer.RemoteChange
	for _, r := range ms.Responses {
		if r.isDelete() {
			continue
		}
		href := a.absURL(r.Href)
		calData := r.firstCalendarData()
		etag := r.firstETag()
		if calData == "" {
			continue
		}
		event, err := icsToEvent(calData)
		if err != nil {
			continue // skip malformed ICS without failing the whole sync
		}
		state.SetETag(href, etag)
		changes = append(changes, syncer.RemoteChange{
			ResourceURL: href,
			ETag:        etag,
			Type:        syncer.ChangeUpsert,
			Event:       &event,
		})
	}
	return changes, nil
}

// ---- XML types ----

type multistatus struct {
	XMLName   xml.Name   `xml:"multistatus"`
	SyncToken string     `xml:"sync-token"`
	Responses []response `xml:"response"`
}

type response struct {
	Href      string      `xml:"href"`
	Status    string      `xml:"status"`
	Propstats []propstatX `xml:"propstat"`
}

type propstatX struct {
	Props  propsetX `xml:"prop"`
	Status string   `xml:"status"`
}

type propsetX struct {
	ETag         string `xml:"getetag"`
	ContentType  string `xml:"getcontenttype"`
	CalendarData string `xml:"calendar-data"`
}

type davResource struct {
	Href string
	ETag string
}

// isDelete returns true for a response representing a deleted/missing resource.
// sync-collection uses a top-level 404 status; multiget uses propstat status.
func (r *response) isDelete() bool {
	if strings.Contains(r.Status, "404") {
		return true
	}
	for _, ps := range r.Propstats {
		if strings.Contains(ps.Status, "200") {
			return false
		}
	}
	// No successful propstat and no top-level status → treat as missing.
	return len(r.Propstats) > 0 && r.Status == ""
}

func (r *response) firstETag() string {
	for _, ps := range r.Propstats {
		if ps.Props.ETag != "" {
			return stripQuotes(ps.Props.ETag)
		}
	}
	return ""
}

func (r *response) firstCalendarData() string {
	for _, ps := range r.Propstats {
		if ps.Props.CalendarData != "" {
			return ps.Props.CalendarData
		}
	}
	return ""
}

// ---- URL helpers ----

// absURL makes href absolute. Relative hrefs (starting with "/") are resolved
// against the scheme+host of calendarURL.
func (a *Adapter) absURL(href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	u := a.calendarURL
	schemeEnd := strings.Index(u, "://")
	if schemeEnd < 0 {
		return href
	}
	hostStart := schemeEnd + 3
	pathStart := strings.Index(u[hostStart:], "/")
	if pathStart < 0 {
		return u + href
	}
	base := u[:hostStart+pathStart]
	if !strings.HasPrefix(href, "/") {
		href = "/" + href
	}
	return base + href
}

func stripQuotes(s string) string {
	return strings.Trim(s, `"`)
}

func xmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
