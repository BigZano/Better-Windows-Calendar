package api

import (
	"fmt"
	"log/slog"
	"net/url"
)

// Attachment mirrors a row in the attachments table.
// Only the URL-link use-case is exposed here; the blob/filename
// columns are kept for future use.
type Attachment struct {
	ID       int64
	EventID  int64
	Label    string // stored in the filename column
	URL      string // stored in the url column
}

// ValidateAttachmentURL returns an error when u is blank or not an
// absolute http/https URL.
func ValidateAttachmentURL(u string) error {
	if u == "" {
		return fmt.Errorf("URL must not be blank")
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL must use http or https (got %q)", parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("URL must include a host")
	}
	return nil
}

// AddAttachment inserts a new URL attachment for the given event.
// label may be empty; url must be a valid absolute http/https URL.
func AddAttachment(eventID int64, label, rawURL string) (int64, error) {
	if err := ValidateAttachmentURL(rawURL); err != nil {
		return 0, err
	}
	db, err := openDB()
	if err != nil {
		return 0, err
	}
	defer db.Close()

	res, err := db.Exec(
		`INSERT INTO attachments (event_id, filename, url) VALUES (?, ?, ?)`,
		eventID, label, rawURL,
	)
	if err != nil {
		return 0, fmt.Errorf("add attachment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	slog.Info("added attachment", "id", id, "event_id", eventID)
	return id, nil
}

// GetAttachments returns all attachments for the given event.
func GetAttachments(eventID int64) ([]Attachment, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(
		`SELECT id, event_id, filename, url FROM attachments WHERE event_id = ? ORDER BY id ASC`,
		eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("get attachments: %w", err)
	}
	defer rows.Close()

	var list []Attachment
	for rows.Next() {
		var a Attachment
		var rawURL *string
		if err := rows.Scan(&a.ID, &a.EventID, &a.Label, &rawURL); err != nil {
			return nil, err
		}
		if rawURL != nil {
			a.URL = *rawURL
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// DeleteAttachment removes a single attachment by its ID.
func DeleteAttachment(id int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.Exec(`DELETE FROM attachments WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete attachment %d: %w", id, err)
	}
	slog.Info("deleted attachment", "id", id)
	return nil
}
