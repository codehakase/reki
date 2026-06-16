package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// Schema is the DDL for the local history database. It is idempotent so it can
// be applied on every open.
const Schema = `
CREATE TABLE IF NOT EXISTS history (
    id          TEXT PRIMARY KEY,   -- ULID: stable, sortable, unique
    command     TEXT NOT NULL,
    cwd         TEXT,
    user        TEXT,               -- OS user that ran the command
    hostname    TEXT,               -- which machine (matters once syncing)
    shell       TEXT,               -- zsh | bash | fish
    session_id  TEXT,               -- groups commands from one shell session
    exit_code   INTEGER,            -- filter failed commands
    duration_ms INTEGER,            -- "what took forever"
    timestamp   INTEGER NOT NULL,   -- nanoseconds since epoch
    is_deleted  INTEGER DEFAULT 0,  -- tombstone (also used by sync deletes)
    synced      INTEGER DEFAULT 0   -- sync bookkeeping
);
CREATE INDEX IF NOT EXISTS idx_history_time ON history(timestamp);
CREATE INDEX IF NOT EXISTS idx_history_cwd  ON history(cwd);
`

// Entry is a single captured command and its context.
type Entry struct {
	ID         string // ULID
	Command    string
	CWD        string
	User       string
	Hostname   string
	Shell      string
	SessionID  string
	ExitCode   int
	DurationMS int64
	Timestamp  int64 // nanoseconds since epoch
	IsDeleted  bool
	Synced     bool
}

// Dir returns seki's data directory, honoring XDG_DATA_HOME.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "seki"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("storage: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "share", "seki"), nil
}

// Path returns the path to the local history database file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.db"), nil
}
