package testutil

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"pycalendar/internal/storage"
)

// NewTestDB opens an in-memory SQLite, runs all migrations, and registers it
// as the shared storage pool for the duration of the test.
func NewTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("testutil: open in-memory db: %v", err)
	}
	db.SetMaxOpenConns(1)
	if err := storage.MigrateDB(db); err != nil {
		db.Close()
		t.Fatalf("testutil: migrate: %v", err)
	}
	storage.SetPool(db)
	t.Cleanup(func() {
		storage.SetPool(nil)
		db.Close()
	})
	return db
}
