package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCommandCreatesDatabaseAndInstallsHookIdempotently(t *testing.T) {
	dataHome := t.TempDir()
	zshHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ZDOTDIR", zshHome)

	for range 2 {
		cmd := newRootCmd()
		cmd.SetArgs([]string{"init"})
		cmd.SetContext(context.Background())
		cmd.SetOut(&bytes.Buffer{})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("reki init error = %v", err)
		}
	}

	databasePath := filepath.Join(dataHome, "reki", "history.db")
	info, err := os.Stat(databasePath)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("database mode = %o, want 600", got)
	}
	rcData, err := os.ReadFile(filepath.Join(zshHome, ".zshrc"))
	if err != nil {
		t.Fatalf("read zsh rc file: %v", err)
	}
	if got := strings.Count(string(rcData), "# >>> reki initialize >>>"); got != 1 {
		t.Fatalf("zsh hook begin marker count = %d, want 1", got)
	}
}
