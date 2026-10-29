package storage

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestOpenMissingDatabaseDoesNotCreate(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing", "history.db")
	_, err := Open(context.Background(), path)
	if !errors.Is(err, ErrDatabaseNotFound) {
		t.Fatalf("Open() error = %v, want %v", err, ErrDatabaseNotFound)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database directory stat error = %v, want %v", err, os.ErrNotExist)
	}
}

func TestMigrateIsIdempotentAndConfiguresConnections(t *testing.T) {
	t.Parallel()

	store := createTestStore(t, filepath.Join(t.TempDir(), "history.db"))
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}

	var migrationCount int
	if err := store.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).
		Scan(&migrationCount); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("migration count = %d, want 1", migrationCount)
	}

	tests := []struct {
		pragma string
		want   string
	}{
		{pragma: "busy_timeout", want: "5000"},
		{pragma: "foreign_keys", want: "1"},
		{pragma: "journal_mode", want: "wal"},
		{pragma: "synchronous", want: "1"},
	}
	for _, tt := range tests {
		t.Run(tt.pragma, func(t *testing.T) {
			var got string
			if err := store.read.QueryRowContext(ctx, "PRAGMA "+tt.pragma).Scan(&got); err != nil {
				t.Fatalf("query PRAGMA %s: %v", tt.pragma, err)
			}
			if got != tt.want {
				t.Fatalf("PRAGMA %s = %q, want %q", tt.pragma, got, tt.want)
			}
		})
	}
}

func TestBusyTimeoutIsFiveSecondsOnBothPools(t *testing.T) {
	t.Parallel()

	store := createTestStore(t, filepath.Join(t.TempDir(), "history.db"))
	ctx := context.Background()
	for name, db := range map[string]*sql.DB{"read": store.read, "write": store.write} {
		t.Run(name, func(t *testing.T) {
			var got string
			if err := db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&got); err != nil {
				t.Fatalf("query PRAGMA busy_timeout: %v", err)
			}
			if got != "5000" {
				t.Fatalf("PRAGMA busy_timeout = %q, want 5000", got)
			}
		})
	}
}

func TestConcurrentMigrationsApplyOnce(t *testing.T) {
	const migratorCount = 10

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	stores := []*Store{createTestStore(t, path)}
	for range migratorCount - 1 {
		stores = append(stores, openTestStore(t, path))
	}

	start := make(chan struct{})
	errs := make(chan error, migratorCount)
	var migrators sync.WaitGroup
	migrators.Add(migratorCount)
	for _, store := range stores {
		go func() {
			defer migrators.Done()
			<-start
			errs <- store.Migrate(ctx)
		}()
	}
	close(start)
	migrators.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent Migrate() error = %v", err)
		}
	}
	if t.Failed() {
		return
	}

	var migrationCount int
	if err := stores[0].read.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations`).
		Scan(&migrationCount); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("migration count = %d, want 1", migrationCount)
	}
}

func TestConcurrentWritersLoseNoRows(t *testing.T) {
	const (
		writerCount   = 10
		rowsPerWriter = 30
	)

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	setup := createTestStore(t, path)
	if err := setup.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := setup.Close(); err != nil {
		t.Fatalf("setup Close() error = %v", err)
	}

	stores := make([]*Store, 0, writerCount)
	for range writerCount {
		stores = append(stores, openTestStore(t, path))
	}

	start := make(chan struct{})
	errs := make(chan error, writerCount)
	var writers sync.WaitGroup
	writers.Add(writerCount)
	for writer := range writerCount {
		store := stores[writer]
		go func() {
			defer writers.Done()
			<-start
			for row := range rowsPerWriter {
				err := store.InsertEntry(ctx, Entry{
					ID:        fmt.Sprintf("entry-%02d-%02d", writer, row),
					Command:   fmt.Sprintf("command %d %d", writer, row),
					StartedAt: time.Unix(int64(writer), int64(row)),
				})
				if err != nil {
					errs <- err
					return
				}
			}
		}()
	}
	close(start)
	writers.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent InsertEntry() error = %v", err)
	}
	if t.Failed() {
		return
	}

	var rowCount int
	err := stores[0].read.QueryRowContext(ctx, `SELECT COUNT(*) FROM history`).Scan(&rowCount)
	if err != nil {
		t.Fatalf("count history rows: %v", err)
	}
	wantRows := writerCount * rowsPerWriter
	if rowCount != wantRows {
		t.Fatalf("history row count = %d, want %d", rowCount, wantRows)
	}

	var integrity string
	if err := stores[0].read.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity check = %q, want ok", integrity)
	}
}

func TestMigrationExcludesDeferredSyncSchema(t *testing.T) {
	t.Parallel()

	store := createTestStore(t, filepath.Join(t.TempDir(), "history.db"))
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var deferredTableCount int
	err := store.read.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table' AND name IN ('origins', 'sync_state')`,
	).Scan(&deferredTableCount)
	if err != nil {
		t.Fatalf("count deferred tables: %v", err)
	}
	if deferredTableCount != 0 {
		t.Fatalf("deferred table count = %d, want 0", deferredTableCount)
	}

	rows, err := store.read.QueryContext(ctx, `PRAGMA table_info(history)`)
	if err != nil {
		t.Fatalf("query history columns: %v", err)
	}
	defer func() { _ = rows.Close() }()

	deferredColumns := map[string]bool{
		"origin_id":  false,
		"seq":        false,
		"updated_at": false,
		"is_deleted": false,
	}
	for rows.Next() {
		var (
			columnID   int
			name       string
			columnType string
			notNull    int
			defaultVal any
			primaryKey int
		)
		if err := rows.Scan(
			&columnID,
			&name,
			&columnType,
			&notNull,
			&defaultVal,
			&primaryKey,
		); err != nil {
			t.Fatalf("scan history column: %v", err)
		}
		if _, ok := deferredColumns[name]; ok {
			deferredColumns[name] = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate history columns: %v", err)
	}
	for name, found := range deferredColumns {
		if found {
			t.Errorf("history contains deferred column %q", name)
		}
	}
}

func TestInsertEntryUpdatesSearchIndex(t *testing.T) {
	t.Parallel()

	store := createTestStore(t, filepath.Join(t.TempDir(), "history.db"))
	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	err := store.InsertEntry(ctx, Entry{
		ID:        "01890f3b-4c5d-7abc-8def-0123456789ab",
		Command:   "kubectl get pods",
		StartedAt: time.Unix(1, 0),
	})
	if err != nil {
		t.Fatalf("InsertEntry() error = %v", err)
	}

	var matchCount int
	err = store.read.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM history_fts
		WHERE history_fts MATCH '"kubectl"'`,
	).Scan(&matchCount)
	if err != nil {
		t.Fatalf("search indexed command: %v", err)
	}
	if matchCount != 1 {
		t.Fatalf("search match count = %d, want 1", matchCount)
	}
}

func TestKilledMidWriteLeavesDatabaseConsistent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "history.db")
	store := createTestStore(t, path)
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("setup Close() error = %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestStorageCrashHelper")
	cmd.Env = append(os.Environ(), "REKI_CRASH_TEST=1", "REKI_CRASH_DB="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe() error = %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start crash helper: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "write-open" {
		t.Fatalf("crash helper did not open transaction: output %q, error %v",
			scanner.Text(), scanner.Err())
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill crash helper: %v", err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("crash helper exited successfully after kill")
	}

	store = openTestStore(t, path)
	var integrity string
	if err := store.read.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity check = %q, want ok", integrity)
	}
	var rowCount int
	if err := store.read.QueryRowContext(ctx, `SELECT COUNT(*) FROM history`).Scan(&rowCount); err != nil {
		t.Fatalf("count history rows: %v", err)
	}
	if rowCount != 0 {
		t.Fatalf("history row count = %d, want 0 after uncommitted write", rowCount)
	}
}

func TestStorageCrashHelper(t *testing.T) {
	if os.Getenv("REKI_CRASH_TEST") != "1" {
		t.Skip("crash test helper")
	}
	ctx := context.Background()
	store, err := Open(ctx, os.Getenv("REKI_CRASH_DB"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	tx, err := store.write.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO history (
			id, command, actor, started_at
		) VALUES ('uncommitted', 'secret', 'user', 1)`)
	if err != nil {
		t.Fatalf("insert uncommitted row: %v", err)
	}
	fmt.Println("write-open")
	time.Sleep(time.Hour)
}

func openTestStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func createTestStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := OpenOrCreate(context.Background(), path)
	if err != nil {
		t.Fatalf("OpenOrCreate() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
