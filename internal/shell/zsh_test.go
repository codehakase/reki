package shell

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallZshHookIsIdempotent(t *testing.T) {
	t.Parallel()

	rcPath := filepath.Join(t.TempDir(), ".zshrc")
	if err := os.WriteFile(rcPath, []byte("export BEFORE=1\n"), 0o640); err != nil {
		t.Fatalf("write initial rc file: %v", err)
	}
	for range 2 {
		if err := InstallZshHook(rcPath, "/opt/reki/bin/reki"); err != nil {
			t.Fatalf("InstallZshHook() error = %v", err)
		}
	}

	data, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("read rc file: %v", err)
	}
	contents := string(data)
	if strings.Count(contents, _zshBlockBegin) != 1 || strings.Count(contents, _zshBlockEnd) != 1 {
		t.Fatalf("guard counts are not one each:\n%s", contents)
	}
	if !strings.Contains(contents, "export BEFORE=1") ||
		!strings.Contains(contents, "'/opt/reki/bin/reki'") {
		t.Fatalf("rc file did not preserve content and install binary path:\n%s", contents)
	}
	info, err := os.Stat(rcPath)
	if err != nil {
		t.Fatalf("stat rc file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("rc mode = %o, want 640", got)
	}
}

func TestInstallZshHookReplacesExistingBlock(t *testing.T) {
	t.Parallel()

	rcPath := filepath.Join(t.TempDir(), ".zshrc")
	existing := "before\n" + _zshBlockBegin + "\nold\n" + _zshBlockEnd + "\nafter\n"
	if err := os.WriteFile(rcPath, []byte(existing), 0o600); err != nil {
		t.Fatalf("write initial rc file: %v", err)
	}
	if err := InstallZshHook(rcPath, "/new/reki"); err != nil {
		t.Fatalf("InstallZshHook() error = %v", err)
	}
	data, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("read rc file: %v", err)
	}
	contents := string(data)
	if strings.Contains(contents, "\nold\n") || !strings.Contains(contents, "'/new/reki'") {
		t.Fatalf("existing block was not replaced:\n%s", contents)
	}
	if !strings.HasPrefix(contents, "before\n") || !strings.HasSuffix(contents, "after\n") {
		t.Fatalf("surrounding rc content changed:\n%s", contents)
	}
}

func TestGeneratedZshHookPreservesStatusAndEncodesCommand(t *testing.T) {
	t.Parallel()

	zshPath, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh is not installed")
	}
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.json")
	fakeRekiPath := filepath.Join(dir, "reki")
	fakeReki := "#!/bin/sh\ncat > \"$REKI_TEST_PAYLOAD\"\n"
	if err := os.WriteFile(fakeRekiPath, []byte(fakeReki), 0o700); err != nil {
		t.Fatalf("write fake reki: %v", err)
	}
	rcPath := filepath.Join(dir, ".zshrc")
	if err := InstallZshHook(rcPath, fakeRekiPath); err != nil {
		t.Fatalf("InstallZshHook() error = %v", err)
	}

	const script = `set -o nounset
source "$1"
_reki_preexec "$2"$' forget "quoted"\nsecond line' '' "$2"$' forget "quoted"\nsecond line'
false
_reki_precmd
print -r -- $?
`
	cmd := exec.Command(zshPath, "-f", "-c", script, "reki-hook-test", rcPath, fakeRekiPath)
	cmd.Env = append(os.Environ(), "REKI_TEST_PAYLOAD="+payloadPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("zsh hook run error = %v, output = %q", err, output)
	}
	if got := strings.TrimSpace(string(output)); got != "1" {
		t.Fatalf("status output = %q, want 1", got)
	}

	var data []byte
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(payloadPath)
		if err == nil && len(data) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("read capture payload: %v", err)
	}
	var payload struct {
		Command        string `json:"command"`
		ExitCode       int    `json:"exit_code"`
		Shell          string `json:"shell"`
		ResolvedBinary string `json:"resolved_binary"`
		Subcommand     string `json:"subcommand"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("decode capture payload %q: %v", data, err)
	}
	wantCommand := fakeRekiPath + " forget \"quoted\"\nsecond line"
	if payload.Command != wantCommand {
		t.Fatalf("command = %q, want multiline command", payload.Command)
	}
	if payload.ExitCode != 1 || payload.Shell != "zsh" {
		t.Fatalf("capture context = exit %d shell %q, want 1/zsh", payload.ExitCode, payload.Shell)
	}
	resolvedInfo, err := os.Stat(payload.ResolvedBinary)
	if err != nil {
		t.Fatalf("stat resolved binary: %v", err)
	}
	fakeInfo, err := os.Stat(fakeRekiPath)
	if err != nil {
		t.Fatalf("stat fake reki: %v", err)
	}
	if !os.SameFile(resolvedInfo, fakeInfo) || payload.Subcommand != "forget" {
		t.Fatalf(
			"resolved command = %q %q, want %q forget",
			payload.ResolvedBinary,
			payload.Subcommand,
			fakeRekiPath,
		)
	}
}
