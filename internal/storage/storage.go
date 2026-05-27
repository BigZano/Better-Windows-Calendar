package storage

import (
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var (
	poolMu sync.Mutex
	poolDB *sql.DB
)

// GetDBPath returns the platform-appropriate path for the SQLite database file.
// Windows: %LOCALAPPDATA%\PyCalendar\PyCalendar\pycalendar.db
// Linux:   $XDG_DATA_HOME/PyCalendar/pycalendar.db  (or ~/.local/share/...)
func GetDBPath() (string, error) {
	var base string
	if dir, ok := os.LookupEnv("LOCALAPPDATA"); ok && dir != "" {
		base = filepath.Join(dir, "PyCalendar", "PyCalendar")
	} else if dir, ok := os.LookupEnv("XDG_DATA_HOME"); ok && dir != "" {
		base = filepath.Join(dir, "PyCalendar")
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		base = filepath.Join(home, ".local", "share", "PyCalendar")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", fmt.Errorf("cannot create data directory: %w", err)
	}
	return filepath.Join(base, "pycalendar.db"), nil
}

// Open returns a connected *sql.DB with WAL mode and busy timeout configured.
// It retries up to maxRetries times with exponential backoff (0.1 s × 2^attempt).
func Open(maxRetries int) (*sql.DB, error) {
	dbPath, err := GetDBPath()
	if err != nil {
		return nil, err
	}

	var db *sql.DB
	for attempt := range maxRetries {
		db, err = sql.Open("sqlite", dbPath)
		if err == nil {
			if pingErr := db.Ping(); pingErr == nil {
				break
			} else {
				err = pingErr
				db.Close()
				db = nil
			}
		}
		if attempt < maxRetries-1 {
			wait := time.Duration(math.Pow(2, float64(attempt))*100) * time.Millisecond
			slog.Warn("database locked, retrying", "attempt", attempt+1, "wait", wait)
			time.Sleep(wait)
		}
	}
	if db == nil {
		return nil, fmt.Errorf("failed to open database after %d attempts: %w", maxRetries, err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	return db, nil
}

// Pool returns the shared long-lived *sql.DB, opening it on first call.
// InitDB must have been called first to ensure the schema is up to date.
func Pool() (*sql.DB, error) {
	poolMu.Lock()
	defer poolMu.Unlock()
	if poolDB != nil {
		return poolDB, nil
	}
	db, err := Open(5)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	poolDB = db
	return poolDB, nil
}

// InitDB runs all pending schema migrations in version order and seeds Pool.
func InitDB() error {
	db, err := Open(5)
	if err != nil {
		return err
	}

	if err := ensureSchemaVersionTable(db); err != nil {
		db.Close()
		return err
	}

	current, err := currentVersion(db)
	if err != nil {
		db.Close()
		return err
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		slog.Info("applying migration", "version", m.version)
		if err := m.run(db); err != nil {
			db.Close()
			return fmt.Errorf("migration v%d: %w", m.version, err)
		}
		if _, err := db.Exec(
			`INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().Unix(),
		); err != nil {
			db.Close()
			return fmt.Errorf("record migration v%d: %w", m.version, err)
		}
		slog.Info("migration applied", "version", m.version)
	}

	// Seed the shared pool with the already-open, migrated connection.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	poolMu.Lock()
	poolDB = db
	poolMu.Unlock()
	return nil
}

// ---- migrations ----

type migration struct {
	version int
	run     func(db *sql.DB) error
}

var migrations = []migration{
	{version: 1, run: migrateV1},
	{version: 2, run: migrateV2},
	{version: 3, run: migrateV3},
	{version: 4, run: migrateV4},
}

func migrateV1(db *sql.DB) error {
	for _, s := range []string{
		`CREATE TABLE IF NOT EXISTS events (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			title           TEXT    NOT NULL,
			start_ts        INTEGER NOT NULL,
			end_ts          INTEGER,
			timezone        TEXT    DEFAULT 'UTC',
			notes           TEXT,
			reminder_ts     INTEGER,
			created_ts      INTEGER NOT NULL,
			updated_ts      INTEGER NOT NULL,
			recurrence_rule TEXT,
			all_day         INTEGER DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_start_ts    ON events(start_ts)`,
		`CREATE INDEX IF NOT EXISTS idx_reminder_ts ON events(reminder_ts)`,
	} {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}

func migrateV2(db *sql.DB) error {
	stmts := []string{
		// calendars — one row per calendar source (local, CalDAV, Google)
		`CREATE TABLE IF NOT EXISTS calendars (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			name           TEXT    NOT NULL,
			color          TEXT    NOT NULL DEFAULT '#3B82F6',
			type           TEXT    NOT NULL DEFAULT 'local',
			sync_url       TEXT,
			username       TEXT,
			credential_key TEXT,
			sync_enabled   INTEGER NOT NULL DEFAULT 1,
			last_synced_at INTEGER,
			created_ts     INTEGER NOT NULL
		)`,
		// seed the default local calendar with a fixed id so existing events can reference it
		`INSERT OR IGNORE INTO calendars (id, name, color, type, created_ts)
		 VALUES (1, 'Local', '#3B82F6', 'local', CAST(strftime('%s','now') AS INTEGER))`,

		// extend events with cross-cutting fields
		`ALTER TABLE events ADD COLUMN calendar_id INTEGER`,
		`ALTER TABLE events ADD COLUMN location TEXT`,
		`ALTER TABLE events ADD COLUMN url TEXT`,
		// assign all pre-existing events to the default local calendar
		`UPDATE events SET calendar_id = 1 WHERE calendar_id IS NULL`,

		// categories — user-defined tags
		`CREATE TABLE IF NOT EXISTS categories (
			id    INTEGER PRIMARY KEY AUTOINCREMENT,
			name  TEXT    NOT NULL UNIQUE,
			color TEXT    NOT NULL DEFAULT '#6B7280'
		)`,
		`CREATE TABLE IF NOT EXISTS event_categories (
			event_id    INTEGER NOT NULL,
			category_id INTEGER NOT NULL,
			PRIMARY KEY (event_id, category_id)
		)`,

		// attendees
		`CREATE TABLE IF NOT EXISTS attendees (
			id       INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id INTEGER NOT NULL,
			name     TEXT,
			email    TEXT,
			status   TEXT NOT NULL DEFAULT 'unknown'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_attendees_event ON attendees(event_id)`,

		// attachments — inline blob or external URL
		`CREATE TABLE IF NOT EXISTS attachments (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id  INTEGER NOT NULL,
			filename  TEXT    NOT NULL,
			mime_type TEXT,
			data      BLOB,
			url       TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_attachments_event ON attachments(event_id)`,

		// sync state per calendar — CalDAV sync-token / Google nextSyncToken + per-resource ETags
		`CREATE TABLE IF NOT EXISTS sync_state (
			calendar_id  INTEGER NOT NULL PRIMARY KEY,
			sync_token   TEXT,
			etag_map     TEXT,
			last_sync_at INTEGER
		)`,

		// credential_index — tracks every OS keychain entry so uninstall can clean up
		`CREATE TABLE IF NOT EXISTS credential_index (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			service    TEXT    NOT NULL,
			account    TEXT    NOT NULL,
			created_ts INTEGER NOT NULL,
			UNIQUE(service, account)
		)`,
	}

	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("stmt %q: %w", s[:min(40, len(s))], err)
		}
	}
	return nil
}

func migrateV3(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE events ADD COLUMN parent_event_id INTEGER`,
		`CREATE INDEX IF NOT EXISTS idx_event_categories_cat ON event_categories(category_id)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("stmt %q: %w", s[:min(40, len(s))], err)
		}
	}
	return nil
}

func migrateV4(db *sql.DB) error {
	stmts := []string{
		`ALTER TABLE events ADD COLUMN resource_url TEXT`,
		`CREATE INDEX IF NOT EXISTS idx_events_resource_url ON events(resource_url)`,
		`CREATE TABLE IF NOT EXISTS conflicts (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			calendar_id INTEGER NOT NULL,
			event_id    INTEGER NOT NULL,
			local_json  TEXT    NOT NULL,
			remote_json TEXT    NOT NULL,
			detected_at INTEGER NOT NULL,
			resolved_at INTEGER,
			resolution  TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_conflicts_calendar ON conflicts(calendar_id, resolved_at)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil && !isAlreadyExists(err) {
			return fmt.Errorf("stmt %q: %w", s[:min(40, len(s))], err)
		}
	}
	return nil
}

// ---- helpers ----

func ensureSchemaVersionTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`)
	return err
}

func currentVersion(db *sql.DB) (int, error) {
	var v int
	err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&v)
	return v, err
}

// isAlreadyExists returns true for SQLite errors that mean the DDL object or
// column already exists — these are expected when re-running migrations.
func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "duplicate column") ||
		strings.Contains(s, "already exists") ||
		strings.Contains(s, "unique constraint failed")
}

