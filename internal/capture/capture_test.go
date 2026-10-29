package capture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codehakase/reki/internal/storage"
)

func TestParse(t *testing.T) {
	t.Parallel()

	multiline := "echo one\necho two 🚀 emoji\necho three"
	tests := []struct {
		name     string
		body     string
		wantCmd  string
		wantIs   bool
		wantFail bool
	}{
		{
			name:    "happy full document",
			body:    fmt.Sprintf(`{"command":%q,"shell":"zsh","exit_code":3,"session_id":"1b2f-c3a1","started_at_ns":1000000000,"duration_ms":42,"cwd":"/tmp","extra":"ignored"}`, multiline),
			wantCmd: multiline,
		},
		{
			name:    "minimal required fields",
			body:    `{"command":"ls","shell":"fish","exit_code":0}`,
			wantCmd: "ls",
		},
		{
			name:    "unknown fields ignored",
			body:    `{"command":"ls","shell":"zsh","exit_code":0,"future_field":{"nested":[1,2]}}`,
			wantCmd: "ls",
		},
		{name: "ignorespace", body: `{"command":" rm -rf /tmp/x","shell":"zsh","exit_code":0}`, wantIs: true},
		{name: "empty command", body: `{"command":"","shell":"zsh","exit_code":0}`, wantIs: true},
		{name: "missing command", body: `{"shell":"zsh","exit_code":0}`, wantFail: true},
		{name: "missing shell", body: `{"command":"ls","exit_code":0}`, wantFail: true},
		{name: "missing exit_code", body: `{"command":"ls","shell":"zsh"}`, wantFail: true},
		{name: "unsupported shell", body: `{"command":"ls","shell":"bash","exit_code":0}`, wantFail: true},
		{name: "corrupt json", body: `{not json`, wantFail: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload, err := Parse(strings.NewReader(tt.body))
			switch {
			case tt.wantIs:
				if !errors.Is(err, ErrIgnore) {
					t.Fatalf("Parse() error = %v, want %v", err, ErrIgnore)
				}
			case tt.wantFail:
				if err == nil {
					t.Fatal("Parse() error = nil, want an error")
				}
				if errors.Is(err, ErrIgnore) {
					t.Fatalf("Parse() error = %v, not an ErrIgnore", err)
				}
			default:
				if err != nil {
					t.Fatalf("Parse() error = %v, want nil", err)
				}
				if payload.Command == nil || *payload.Command != tt.wantCmd {
					t.Fatalf("command = %v, want %q", payload.Command, tt.wantCmd)
				}
			}
		})
	}
}

func newCaptureEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// runCapture performs one fresh Run, as a new capture process would.
func runCapture(t *testing.T, body string) *strings.Builder {
	t.Helper()
	stderr := &strings.Builder{}
	code := Run(context.Background(), strings.NewReader(body), stderr)
	if code != 0 {
		t.Fatalf("Run() exit code = %d, want 0", code)
	}
	return stderr
}

func payloadBody(session string) string {
	return `{"command":"deploy --password=secret","shell":"zsh","exit_code":1,"session_id":"` + session + `","duration_ms":42,"cwd":"/tmp"}`
}

func TestRunMissingDatabaseWarnsOncePerSession(t *testing.T) {
	newCaptureEnv(t)

	first := runCapture(t, payloadBody("sess-1"))
	if got := first.String(); !strings.Contains(got, _hintInit) || !strings.Contains(got, "reki capture:") {
		t.Fatalf("first warning = %q, want the init hint on one line", got)
	}
	if lines := strings.Count(strings.TrimRight(first.String(), "\n"), "\n") + 1; lines != 1 {
		t.Fatalf("first warning has %d lines, want 1", lines)
	}

	// A fresh process for the same session must stay silent via the sentinel.
	second := runCapture(t, payloadBody("sess-1"))
	if got := second.String(); got != "" {
		t.Fatalf("second warning = %q, want silence", got)
	}

	dataDir, err := storage.Dir()
	if err != nil {
		t.Fatalf("storage.Dir() error = %v", err)
	}
	info, err := os.Stat(filepath.Join(dataDir, "warn-sess-1"))
	if err != nil {
		t.Fatalf("sentinel stat error = %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("sentinel mode = %o, want 600", perm)
	}
}

func TestRunWithoutSessionWarnsPerProcess(t *testing.T) {
	newCaptureEnv(t)

	body := `{"command":"ls","shell":"zsh","exit_code":0}`
	for i := range 2 {
		stderr := runCapture(t, body)
		if !strings.Contains(stderr.String(), "reki capture:") {
			t.Fatalf("run %d warning = %q, want one warning per process without a session", i+1, stderr.String())
		}
	}
}

func TestRunMalformedPayloadWarnsOnceWithShellSession(t *testing.T) {
	newCaptureEnv(t)
	t.Setenv("REKI_SESSION", "sess-malformed")

	first := runCapture(t, `{bad json`)
	if got := first.String(); !strings.Contains(got, "reki capture:") {
		t.Fatalf("first warning = %q, want one warning", got)
	}
	second := runCapture(t, `{bad json`)
	if got := second.String(); got != "" {
		t.Fatalf("second warning = %q, want silence", got)
	}
}

func TestRunCorruptDatabaseMentionsDoctor(t *testing.T) {
	newCaptureEnv(t)

	dataDir, err := storage.Dir()
	if err != nil {
		t.Fatalf("storage.Dir() error = %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "history.db"), []byte("garbage bytes, not sqlite"), 0o600); err != nil {
		t.Fatalf("write corrupt database: %v", err)
	}

	stderr := runCapture(t, payloadBody("sess-corrupt"))
	if got := stderr.String(); !strings.Contains(got, _hintDoctor) {
		t.Fatalf("warning = %q, want the doctor hint", got)
	}
}

func TestRunIgnoresSilently(t *testing.T) {
	newCaptureEnv(t)

	tests := []struct {
		name string
		body string
	}{
		{name: "ignorespace", body: `{"command":" secret-command","shell":"zsh","exit_code":0}`},
		{name: "empty command", body: `{"command":"","shell":"zsh","exit_code":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr := runCapture(t, tt.body)
			if got := stderr.String(); got != "" {
				t.Fatalf("warning = %q, want silence", got)
			}
			dataDir, err := storage.Dir()
			if err != nil {
				t.Fatal(err)
			}
			if entries, _ := os.ReadDir(dataDir); len(entries) != 0 {
				t.Fatalf("data dir contents = %v, want none", entries)
			}
		})
	}
}

func TestRunIgnoresResolvedRekiForget(t *testing.T) {
	newCaptureEnv(t)
	ctx := context.Background()
	initTestDatabase(t, ctx)

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	body := fmt.Sprintf(
		`{"command":"reki forget secret-selector","shell":"zsh","exit_code":0,`+
			`"session_id":"sess-forget","resolved_binary":%q,"subcommand":"forget"}`,
		executable,
	)
	stderr := runCapture(t, body)
	if got := stderr.String(); got != "" {
		t.Fatalf("warning = %q, want silence", got)
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

func TestRunRejectsBadPayloadAndExitsZero(t *testing.T) {
	newCaptureEnv(t)

	tests := []struct {
		name string
		body string
	}{
		{name: "corrupt json", body: `{bad json`},
		{name: "missing required fields", body: `{"shell":"zsh"}`},
		{name: "unsupported shell", body: `{"command":"zebraquituplet-Xy9","shell":"bash","exit_code":0}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr := runCapture(t, tt.body)
			if got := stderr.String(); !strings.Contains(got, "reki capture:") {
				t.Fatalf("warning = %q, want a single warning line", got)
			}
			if strings.Contains(stderr.String(), "zebraquituplet") {
				t.Fatal("warning leaks command text")
			}
		})
	}
}
