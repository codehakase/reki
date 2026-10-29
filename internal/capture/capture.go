// Package capture implements the direct-write capture path that shell hooks
// run in the background. It reads one JSON document from stdin, redacts the
// command, and writes one history row. Capture is best-effort: it always
// exits 0 and never fails the shell.
package capture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"github.com/codehakase/reki/internal/config"
	"github.com/codehakase/reki/internal/storage"
)

// ErrIgnore marks a payload that must be skipped silently: an empty command
// or a leading-space or sensitive Reki command. Callers exit 0 and print
// nothing.
var ErrIgnore = errors.New("capture: command ignored")

// Payload is the capture document a shell hook sends on stdin. Pointer
// fields distinguish an absent field, which is a validation error for
// required fields, from a present zero value.
type Payload struct {
	Command     *string `json:"command"`
	Shell       *string `json:"shell"`
	ExitCode    *int    `json:"exit_code"`
	SessionID   string  `json:"session_id"`
	StartedAtNS *int64  `json:"started_at_ns"`
	DurationMS  *int64  `json:"duration_ms"`
	CWD         string  `json:"cwd"`
	// ResolvedBinary and Subcommand let the hook identify Reki's own
	// sensitive commands without trusting command text or an alias name.
	ResolvedBinary string `json:"resolved_binary"`
	Subcommand     string `json:"subcommand"`
}

// Parse reads one capture document from r and validates it. It ignores
// unknown fields. On a validation failure it also returns the decoded
// payload, so callers can still honor warn-once for the session ID.
// An empty command or a leading-space command yields ErrIgnore.
func Parse(r io.Reader) (Payload, error) {
	var payload Payload
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return payload, fmt.Errorf("capture: decode payload: %w", err)
	}
	return payload, payload.validate()
}

func (p Payload) validate() error {
	if p.Command == nil {
		return fmt.Errorf("capture: payload missing required field %q", "command")
	}
	if p.Shell == nil {
		return fmt.Errorf("capture: payload missing required field %q", "shell")
	}
	if p.ExitCode == nil {
		return fmt.Errorf("capture: payload missing required field %q", "exit_code")
	}
	if strings.HasPrefix(*p.Command, " ") {
		return fmt.Errorf("%w: ignorespace", ErrIgnore)
	}
	if *p.Command == "" {
		return fmt.Errorf("%w: empty command", ErrIgnore)
	}
	if *p.Shell != "zsh" && *p.Shell != "fish" {
		return fmt.Errorf("capture: unsupported shell %q (v1 supports zsh and fish only)", *p.Shell)
	}
	return nil
}

// Run performs one capture: parse the payload, redact the command, and write
// one history row. It returns 0 in every case, prints at most one warning
// line to warnOut, and recovers panics silently. Warnings never contain
// command text.
func Run(ctx context.Context, in io.Reader, warnOut io.Writer) (exitCode int) {
	defer func() {
		if r := recover(); r != nil {
			exitCode = 0
		}
	}()

	payload, err := Parse(in)
	if err != nil {
		if errors.Is(err, ErrIgnore) {
			return 0
		}
		hint := hintFor(err)
		warnPayloadFailure(warnOut, warningSessionID(payload.SessionID), err, hint)
		return 0
	}
	if isRekiForget(payload) {
		return 0
	}
	warningSession := warningSessionID(payload.SessionID)

	path, err := storage.Path()
	if err != nil {
		warnPayloadFailure(
			warnOut,
			warningSession,
			fmt.Errorf("capture: resolve database path: %w", err),
			_hintGeneric,
		)
		return 0
	}
	warn := newWarner(warnOut, filepath.Dir(path), warningSession)

	cfg, err := config.Load()
	if err != nil {
		warn.warn(err, _hintGeneric)
		return 0
	}
	command := cfg.Redact(*payload.Command)

	store, err := storage.Open(ctx, path)
	if err != nil {
		warn.warn(err, hintFor(err))
		return 0
	}
	defer func() { _ = store.Close() }()

	entry := newEntry(payload, uuid.NewV7().String(), command)
	if err := store.InsertEntry(ctx, entry); err != nil {
		warn.warn(err, hintFor(err))
	}
	return 0
}

func warningSessionID(payloadSessionID string) string {
	if _sessionIDPattern.MatchString(payloadSessionID) {
		return payloadSessionID
	}
	return os.Getenv("REKI_SESSION")
}

// isRekiForget reports whether the shell resolved the command to this Reki
// executable and identified the sensitive forget subcommand.
func isRekiForget(payload Payload) bool {
	if payload.ResolvedBinary == "" || payload.Subcommand != "forget" {
		return false
	}
	currentExecutable, err := os.Executable()
	if err != nil {
		return false
	}
	currentInfo, err := os.Stat(currentExecutable)
	if err != nil {
		return false
	}
	resolvedInfo, err := os.Stat(payload.ResolvedBinary)
	if err != nil {
		return false
	}
	return os.SameFile(currentInfo, resolvedInfo)
}

// newEntry maps the payload to a storage row. Actor is "user" for shell
// capture. A username or hostname lookup failure leaves the field empty and
// must not drop the row; the schema allows NULL there.
func newEntry(payload Payload, id string, command string) storage.Entry {
	startedAt := time.Now()
	if payload.StartedAtNS != nil {
		startedAt = time.Unix(0, *payload.StartedAtNS)
	}

	var duration time.Duration
	if payload.DurationMS != nil {
		duration = time.Duration(*payload.DurationMS) * time.Millisecond
	}

	username := ""
	if u, err := user.Current(); err == nil {
		// A lookup failure keeps the field empty (NULL); the row still lands.
		username = u.Username
	}
	hostname, _ := os.Hostname()

	return storage.Entry{
		ID:        id,
		Command:   command,
		CWD:       payload.CWD,
		Username:  username,
		Hostname:  hostname,
		Shell:     *payload.Shell,
		SessionID: payload.SessionID,
		ExitCode:  *payload.ExitCode,
		Duration:  duration,
		Actor:     "user",
		StartedAt: startedAt,
	}
}
