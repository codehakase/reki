package capture

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"uuid"
)

func TestRunCapturesRedactedRow(t *testing.T) {
	newCaptureEnv(t)
	ctx := context.Background()
	initTestDatabase(t, ctx)

	stderr := runCapture(t, payloadBody("sess-e2e"))
	if stderr.String() != "" {
		t.Fatalf("warning = %q, want silence on a healthy run", stderr.String())
	}

	var (
		command, actor, session, shell, username string
		exitCode                                 int
		durationMS, startedAt                    int64
	)
	id := readCapturedRow(t, ctx, "sess-e2e",
		&command, &actor, &session, &shell, &username, &exitCode, &durationMS, &startedAt,
	)

	if !strings.Contains(command, "[redacted]") {
		t.Fatalf("command = %q, want the redacted marker", command)
	}
	if strings.Contains(command, "secret") {
		t.Fatalf("command = %q, want the secret value removed", command)
	}
	if actor != "user" {
		t.Fatalf("actor = %q, want user", actor)
	}
	if session != "sess-e2e" || shell != "zsh" || exitCode != 1 || durationMS != 42 {
		t.Fatalf("captured context = session %q shell %q exit %d duration %d, want sess-e2e/zsh/1/42",
			session, shell, exitCode, durationMS)
	}
	if username == "" {
		t.Fatal("username empty, want the OS user for a healthy capture")
	}

	// The FTS index must hold the redacted text; assert MATCH itself, never
	// a row count. An unmaintained external-content table quietly returns no
	// MATCH hits while count(*) stays plausible.
	db := openTestDB(t, ctx)
	var matchCount int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM history_fts WHERE history_fts MATCH '"redacted"'`).
		Scan(&matchCount); err != nil {
		t.Fatalf("search history_fts: %v", err)
	}
	if matchCount != 1 {
		t.Fatalf("history_fts match count = %d, want 1", matchCount)
	}

	assertUUIDv7(t, id)
}

func TestRunDropsRowWhenConfigIsInvalid(t *testing.T) {
	newCaptureEnv(t)
	ctx := context.Background()
	initTestDatabase(t, ctx)

	configHome := os.Getenv("XDG_CONFIG_HOME")
	if err := os.MkdirAll(filepath.Join(configHome, "reki"), 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "reki", "config.json"),
		[]byte("{not valid json"), 0o600); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}

	stderr := runCapture(t, payloadBody("sess-config"))
	if got := stderr.String(); !strings.Contains(got, "reki capture:") {
		t.Fatalf("warning = %q, want capture warning", got)
	}

	db := openTestDB(t, ctx)
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM history`).Scan(&count); err != nil {
		t.Fatalf("count history rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("history row count = %d, want 0", count)
	}
}

func assertUUIDv7(t *testing.T, id string) {
	t.Helper()
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("parse UUID %q: %v", id, err)
	}
	if version := parsed[6] >> 4; version != 7 {
		t.Fatalf("UUID version = %d, want 7", version)
	}
}
