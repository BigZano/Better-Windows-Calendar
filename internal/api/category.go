package api

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Category mirrors the categories table schema.
type Category struct {
	ID    int64
	Name  string
	Color string
}

// CreateCategory inserts a new category and returns its ID.
func CreateCategory(name, color string) (int64, error) {
	if color == "" {
		color = "#6B7280"
	}
	db, err := openDB()
	if err != nil {
		return 0, err
	}


	res, err := db.Exec(`INSERT INTO categories (name, color) VALUES (?, ?)`, name, color)
	if err != nil {
		return 0, fmt.Errorf("create category: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	slog.Info("created category", "id", id, "name", name)
	return id, nil
}

// GetCategories returns all categories ordered by name.
func GetCategories() ([]Category, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}


	rows, err := db.Query(`SELECT id, name, color FROM categories ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("get categories: %w", err)
	}
	defer rows.Close()

	var cats []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Color); err != nil {
			return nil, err
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

// DeleteCategory removes the category and all its event associations.
func DeleteCategory(id int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}


	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM event_categories WHERE category_id = ?`, id); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete event_categories for category %d: %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM categories WHERE id = ?`, id); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete category %d: %w", id, err)
	}
	slog.Info("deleted category", "id", id)
	return tx.Commit()
}

// AddEventCategory tags an event with a category. Idempotent.
func AddEventCategory(eventID, categoryID int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}


	_, err = db.Exec(
		`INSERT OR IGNORE INTO event_categories (event_id, category_id) VALUES (?, ?)`,
		eventID, categoryID,
	)
	if err != nil {
		return fmt.Errorf("add event category: %w", err)
	}
	return nil
}

// RemoveEventCategory removes a category tag from an event.
func RemoveEventCategory(eventID, categoryID int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}


	_, err = db.Exec(
		`DELETE FROM event_categories WHERE event_id = ? AND category_id = ?`,
		eventID, categoryID,
	)
	if err != nil {
		return fmt.Errorf("remove event category: %w", err)
	}
	return nil
}

// GetEventCategories returns all categories attached to the given event.
func GetEventCategories(eventID int64) ([]Category, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}


	rows, err := db.Query(`
		SELECT c.id, c.name, c.color
		FROM categories c
		JOIN event_categories ec ON ec.category_id = c.id
		WHERE ec.event_id = ?
		ORDER BY c.name ASC`, eventID)
	if err != nil {
		return nil, fmt.Errorf("get event categories: %w", err)
	}
	defer rows.Close()

	var cats []Category
	for rows.Next() {
		var c Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Color); err != nil {
			return nil, err
		}
		cats = append(cats, c)
	}
	return cats, rows.Err()
}

// SetEventCategories replaces all category tags on an event in a single transaction.
func SetEventCategories(eventID int64, categoryIDs []int64) error {
	db, err := openDB()
	if err != nil {
		return err
	}


	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM event_categories WHERE event_id = ?`, eventID); err != nil {
		tx.Rollback()
		return fmt.Errorf("clear event categories: %w", err)
	}
	for _, catID := range categoryIDs {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO event_categories (event_id, category_id) VALUES (?, ?)`, eventID, catID); err != nil {
			tx.Rollback()
			return fmt.Errorf("set event category: %w", err)
		}
	}
	if _, err := tx.Exec(`UPDATE events SET updated_ts = ? WHERE id = ?`, time.Now().Unix(), eventID); err != nil {
		tx.Rollback()
		return fmt.Errorf("update event ts: %w", err)
	}
	return tx.Commit()
}

// EnrichEventsWithCategories populates the Categories field on each event in-place.
// Virtual recurring occurrences (ID=0) are looked up via ParentEventID.
func EnrichEventsWithCategories(events []Event) error {
	idSet := make(map[int64]bool)
	for _, e := range events {
		if e.ID != 0 {
			idSet[e.ID] = true
		} else if e.ParentEventID.Valid {
			idSet[e.ParentEventID.Int64] = true
		}
	}
	if len(idSet) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}

	db, err := openDB()
	if err != nil {
		return err
	}


	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}

	rows, err := db.Query(`
		SELECT ec.event_id, c.id, c.name, c.color
		FROM event_categories ec
		JOIN categories c ON c.id = ec.category_id
		WHERE ec.event_id IN (`+placeholders+`)
		ORDER BY ec.event_id, c.name`, args...)
	if err != nil {
		return fmt.Errorf("enrich categories: %w", err)
	}
	defer rows.Close()

	catMap := make(map[int64][]Category)
	for rows.Next() {
		var eventID int64
		var c Category
		if err := rows.Scan(&eventID, &c.ID, &c.Name, &c.Color); err != nil {
			continue
		}
		catMap[eventID] = append(catMap[eventID], c)
	}

	for i := range events {
		lookupID := events[i].ID
		if lookupID == 0 && events[i].ParentEventID.Valid {
			lookupID = events[i].ParentEventID.Int64
		}
		events[i].Categories = catMap[lookupID]
	}
	return rows.Err()
}

// GetEventsByCategory returns all events tagged with the given category.
func GetEventsByCategory(categoryID int64) ([]Event, error) {
	db, err := openDB()
	if err != nil {
		return nil, err
	}


	rows, err := db.Query(`
		SELECT e.id, e.title, e.start_ts, e.end_ts, e.timezone, e.notes, e.reminder_ts,
		       e.created_ts, e.updated_ts, e.recurrence_rule, e.all_day,
		       e.calendar_id, e.location, e.url, e.parent_event_id
		FROM events e
		JOIN event_categories ec ON ec.event_id = e.id
		WHERE ec.category_id = ?
		ORDER BY e.start_ts ASC`, categoryID)
	if err != nil {
		return nil, fmt.Errorf("get events by category: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
