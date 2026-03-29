package storage

import (
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const schemaVersion = 1

// GetDBPath returns the platform-appropriate path for the SQLite database file.
// On Windows: %LOCALAPPDATA%\PyCalendar\PyCalendar\pycalendar.db
// On Linux:   $HOME/.local/share/PyCalendar/pycalendar.db
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
// It retries up to maxRetries times with exponential backoff (0.1s × 2^attempt).
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
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	return db, nil
}

// InitDB creates the schema if it does not exist and records the schema version.
func InitDB() error {
	db, err := Open(5)
	if err != nil {
		return err
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS events (
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
		)`)
	if err != nil {
		return fmt.Errorf("create events table: %w", err)
	}

	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_start_ts    ON events(start_ts)`)
	if err != nil {
		return fmt.Errorf("create start_ts index: %w", err)
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS idx_reminder_ts ON events(reminder_ts)`)
	if err != nil {
		return fmt.Errorf("create reminder_ts index: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version    INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`)
	if err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	var current int
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	if current < schemaVersion {
		_, err = db.Exec(`INSERT INTO schema_version (version, applied_at) VALUES (?, ?)`,
			schemaVersion, time.Now().Unix())
		if err != nil {
			return fmt.Errorf("record schema version: %w", err)
		}
	}

	return nil
}
