package credstore

import (
	"encoding/json"
	"fmt"
	"time"

	"pycalendar/internal/keychain"
)

const (
	svcOAuth  = "PyCalendar:oauth"
	svcCalDAV = "PyCalendar:caldav"
)

// OAuthToken holds the durable credential for an OAuth-connected calendar.
// RefreshToken is []byte so callers can zero it after use (ADR-0004).
// The access token is never stored — callers exchange the refresh token on
// every sync operation and zero the resulting access token after use.
type OAuthToken struct {
	RefreshToken []byte
	Scope        string
	ObtainedAt   time.Time
}

// Zero overwrites the refresh token bytes in place and resets the struct.
func (t *OAuthToken) Zero() {
	for i := range t.RefreshToken {
		t.RefreshToken[i] = 0
	}
	*t = OAuthToken{}
}

// StoreOAuthToken serialises tok and stores it in the OS keyring under calendarID.
// The intermediate JSON bytes are zeroed before returning.
func StoreOAuthToken(calendarID int64, tok OAuthToken) error {
	b, err := marshalToken(tok)
	if err != nil {
		return fmt.Errorf("credstore: marshal oauth token: %w", err)
	}
	defer zeroBytes(b)
	return keychain.Set(svcOAuth, accountKey(calendarID), string(b))
}

// GetOAuthToken retrieves the stored OAuthToken for calendarID.
// The intermediate string from the keyring is zeroed via the defer.
func GetOAuthToken(calendarID int64) (OAuthToken, error) {
	raw, err := keychain.Get(svcOAuth, accountKey(calendarID))
	if err != nil {
		return OAuthToken{}, fmt.Errorf("credstore: get oauth token %d: %w", calendarID, err)
	}
	b := []byte(raw)
	defer zeroBytes(b)
	return unmarshalToken(b, calendarID)
}

// DeleteOAuthToken removes the stored OAuth token for calendarID.
func DeleteOAuthToken(calendarID int64) error {
	return keychain.Delete(svcOAuth, accountKey(calendarID))
}

// StoreCalDAV stores Basic Auth credentials for a CalDAV calendar.
// The intermediate JSON bytes are zeroed before returning.
func StoreCalDAV(calendarID int64, username, password string) error {
	b, err := json.Marshal(struct {
		U string `json:"u"`
		P string `json:"p"`
	}{username, password})
	if err != nil {
		return fmt.Errorf("credstore: marshal caldav creds: %w", err)
	}
	defer zeroBytes(b)
	return keychain.Set(svcCalDAV, accountKey(calendarID), string(b))
}

// GetCalDAV retrieves the stored CalDAV credentials for calendarID.
func GetCalDAV(calendarID int64) (username, password string, err error) {
	raw, err := keychain.Get(svcCalDAV, accountKey(calendarID))
	if err != nil {
		return "", "", fmt.Errorf("credstore: get caldav creds %d: %w", calendarID, err)
	}
	b := []byte(raw)
	defer zeroBytes(b)

	var wire struct {
		U string `json:"u"`
		P string `json:"p"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		return "", "", fmt.Errorf("credstore: unmarshal caldav creds %d: %w", calendarID, err)
	}
	return wire.U, wire.P, nil
}

// DeleteCalDAV removes the stored CalDAV credentials for calendarID.
func DeleteCalDAV(calendarID int64) error {
	return keychain.Delete(svcCalDAV, accountKey(calendarID))
}

// DeleteCalendar removes all stored credentials for calendarID.
// Both deletions are always attempted; errors are silently discarded.
func DeleteCalendar(calendarID int64) {
	_ = DeleteOAuthToken(calendarID)
	_ = DeleteCalDAV(calendarID)
}

// ---- helpers ----

func accountKey(calendarID int64) string {
	return fmt.Sprintf("%d", calendarID)
}

type tokenWire struct {
	R []byte    `json:"r"`
	S string    `json:"s"`
	T time.Time `json:"t"`
}

func marshalToken(tok OAuthToken) ([]byte, error) {
	return json.Marshal(tokenWire{R: tok.RefreshToken, S: tok.Scope, T: tok.ObtainedAt})
}

func unmarshalToken(b []byte, calendarID int64) (OAuthToken, error) {
	var w tokenWire
	if err := json.Unmarshal(b, &w); err != nil {
		return OAuthToken{}, fmt.Errorf("credstore: unmarshal oauth token %d: %w", calendarID, err)
	}
	return OAuthToken{RefreshToken: w.R, Scope: w.S, ObtainedAt: w.T}, nil
}

func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
