// Package storage owns Reki's local SQLite database.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	_ "modernc.org/sqlite"
)

const _directoryMode = 0o700

// ErrDatabaseNotFound indicates that the local database has not been created.
var ErrDatabaseNotFound = errors.New("storage: database not found")

// Store contains separate SQLite pools for serialized writes and concurrent reads.
type Store struct {
	write *sql.DB
	read  *sql.DB
}

// Entry is a captured command and its execution context.
type Entry struct {
	ID        string
	Command   string
	CWD       string
	Username  string
	Hostname  string
	Shell     string
	SessionID string
	ExitCode  int
	Duration  time.Duration
	Actor     string
	StartedAt time.Time
	DedupeKey sql.NullString
}

// Open opens an existing local database. It does not create files or run migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, ErrDatabaseNotFound
	} else if err != nil {
		return nil, fmt.Errorf("storage: inspect database: %w", err)
	}

	return openStore(ctx, path)
}

// OpenOrCreate creates the local database when needed. It does not run migrations.
func OpenOrCreate(ctx context.Context, path string) (*Store, error) {
	if err := prepareDatabaseFile(path); err != nil {
		return nil, err
	}
	return openStore(ctx, path)
}

func openStore(ctx context.Context, path string) (*Store, error) {
	dsn := databaseDSN(path)
	write, err := sql.Open("sqlite", dsn+"&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("storage: open write pool: %w", err)
	}
	write.SetMaxOpenConns(1)
	write.SetMaxIdleConns(1)

	read, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = write.Close()
		return nil, fmt.Errorf("storage: open read pool: %w", err)
	}
	readConnections := runtime.GOMAXPROCS(0)
	read.SetMaxOpenConns(readConnections)
	read.SetMaxIdleConns(readConnections)

	store := &Store{write: write, read: read}
	if err := store.ping(ctx); err != nil {
		_ = store.Close()
		return nil, err
	}
	return store, nil
}

// Close releases both database pools. SQLite manages WAL checkpointing independently.
func (s *Store) Close() error {
	return errors.Join(s.read.Close(), s.write.Close())
}

// InsertEntry inserts one history row and its search index entry atomically.
func (s *Store) InsertEntry(ctx context.Context, entry Entry) error {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: insert entry: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	actor := entry.Actor
	if actor == "" {
		actor = "user"
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO history (
			id, command, cwd, username, hostname, shell, session_id,
			exit_code, duration_ms, actor, started_at, dedupe_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Command, entry.CWD, entry.Username, entry.Hostname,
		entry.Shell, entry.SessionID, entry.ExitCode, entry.Duration.Milliseconds(),
		actor, entry.StartedAt.UnixNano(), entry.DedupeKey,
	)
	if err != nil {
		return fmt.Errorf("storage: insert entry: insert history: %w", err)
	}
	rowID, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("storage: insert entry: read row ID: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO history_fts (rowid, command)
		VALUES (?, ?)`, rowID, entry.Command); err != nil {
		return fmt.Errorf("storage: insert entry: index command: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: insert entry: commit: %w", err)
	}
	return nil
}

func (s *Store) ping(ctx context.Context) error {
	if err := s.write.PingContext(ctx); err != nil {
		return fmt.Errorf("storage: ping write pool: %w", err)
	}
	if err := s.read.PingContext(ctx); err != nil {
		return fmt.Errorf("storage: ping read pool: %w", err)
	}
	return nil
}

func prepareDatabaseFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), _directoryMode); err != nil {
		return fmt.Errorf("storage: create data directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("storage: create database file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("storage: close new database file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("storage: secure database file: %w", err)
	}
	return nil
}

func databaseDSN(path string) string {
	query := url.Values{}
	query.Set("_busy_timeout", "5000")
	query.Set("_foreign_keys", "on")
	query.Set("_journal_mode", "WAL")
	query.Set("_synchronous", "NORMAL")
	query.Set("mode", "rw")
	return (&url.URL{
		Scheme:   "file",
		Path:     path,
		RawQuery: query.Encode(),
	}).String()
}
