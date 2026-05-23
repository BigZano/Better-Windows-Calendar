package push

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pycalendar/internal/api"
)

type payload struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	EventID   int64  `json:"event_id"`
	Timestamp int64  `json:"timestamp"`
}

// Send delivers a push notification to webhookURL for event e.
// Returns an error if the URL is not HTTPS, encoding fails, the request
// fails, or the server returns a non-2xx status.
func Send(webhookURL, title, body string, e api.Event) error {
	if !strings.HasPrefix(webhookURL, "https://") {
		return fmt.Errorf("push: webhook URL must use https")
	}

	data, err := json.Marshal(payload{
		Title:     title,
		Body:      body,
		EventID:   e.ID,
		Timestamp: e.StartTS,
	})
	if err != nil {
		return fmt.Errorf("push: marshal payload: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("push: send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("push: server returned %d", resp.StatusCode)
	}
	return nil
}
