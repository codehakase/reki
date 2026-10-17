package storage

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var _migrationFiles embed.FS

// Migrate applies each pending schema migration exactly once.
func (s *Store) Migrate(ctx context.Context) error {
	if err := s.ensureMigrationTable(ctx); err != nil {
		return err
	}

	files, err := fs.Glob(_migrationFiles, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("storage: migrations: list: %w", err)
	}

	for _, path := range files {
		version, err := migrationVersion(path)
		if err != nil {
			return err
		}

		contents, err := _migrationFiles.ReadFile(path)
		if err != nil {
			return fmt.Errorf("storage: migrations: read %s: %w", path, err)
		}

		if err := s.applyMigration(ctx, version, string(contents)); err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) ensureMigrationTable(ctx context.Context) error {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: migrations: begin tracking table: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`)
	if err != nil {
		return fmt.Errorf("storage: migrations: create tracking table: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: migrations: commit tracking table: %w", err)
	}

	return nil
}

func (s *Store) applyMigration(ctx context.Context, version int64, contents string) error {
	tx, err := s.write.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("storage: migrations: begin version %d: %w", version, err)
	}
	defer func() { _ = tx.Rollback() }()

	var applied int
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)`, version,
	).Scan(&applied)
	if err != nil {
		return fmt.Errorf("storage: migrations: check version %d: %w", version, err)
	}

	if applied == 1 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, contents); err != nil {
		return fmt.Errorf("storage: migrations: apply version %d: %w", version, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO schema_migrations (version, applied_at)
		VALUES (?, ?)`, version, time.Now().UnixNano()); err != nil {
		return fmt.Errorf("storage: migrations: record version %d: %w", version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: migrations: commit version %d: %w", version, err)
	}

	return nil
}

func migrationVersion(path string) (int64, error) {
	name := filepath.Base(path)
	prefix, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("storage: migrations: invalid name %q", name)
	}
	version, err := strconv.ParseInt(prefix, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("storage: migrations: parse version %q: %w", name, err)
	}
	return version, nil
}
