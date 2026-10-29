package capture

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/codehakase/reki/internal/storage"
)

// initTestDatabase creates and migrates the database for the current
// XDG_DATA_HOME, as reki init would.
func initTestDatabase(t *testing.T, ctx context.Context) {
	t.Helper()
	path, err := storage.Path()
	if err != nil {
		t.Fatalf("storage.Path() error = %v", err)
	}
	store, err := storage.OpenOrCreate(ctx, path)
	if err != nil {
		t.Fatalf("storage.OpenOrCreate() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("store.Migrate() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}
}

// readCapturedRow loads the single row captured under a session.
func readCapturedRow(t *testing.T, ctx context.Context, session string, dest ...any) string {
	t.Helper()
	db := openTestDB(t, ctx)

	var id string
	args := append([]any{&id}, dest...)
	err := db.QueryRowContext(ctx, `
		SELECT id, command, actor, session_id, shell, username, exit_code, duration_ms, started_at
		FROM history WHERE session_id = ?`, session,
	).Scan(args...)
	if err != nil {
		t.Fatalf("read captured row: %v", err)
	}
	return id
}

// openTestDB opens a read-only connection to the database under the current
// XDG_DATA_HOME. Tests only; the capture path uses storage.Open.
func openTestDB(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()
	path, err := storage.Path()
	if err != nil {
		t.Fatalf("storage.Path() error = %v", err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immediate=false")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
