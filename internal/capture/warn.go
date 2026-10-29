package capture

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/codehakase/reki/internal/storage"
)

// Warning hints. They carry no command text or config content.
const (
	_hintInit    = "run `reki init` to create the database"
	_hintDoctor  = "the database may be corrupt; run `reki doctor`"
	_hintGeneric = "the command was dropped; run `reki doctor` for details"
)

// _sessionIDPattern is the safe shape of a sentinel file suffix: hex-style
// groups and dashes only. Anything else cannot be trusted in a file name,
// so warning falls back to at-most-once per process.
var _sessionIDPattern = regexp.MustCompile("^[0-9A-Za-z_-]+$")

// warner prints at most one capture warning line per shell session. With a
// session ID it prints only when it creates the sentinel file
// `<data_dir>/warn-<session_id>`; without one it prints once per process.
type warner struct {
	out       io.Writer
	dataDir   string
	sessionID string
	warned    bool
}

// newWarner builds the warner. dataDir may be empty when the data
// directory cannot be resolved; warning then degrades to at-most-once per
// process, which still bounds terminal output.
func newWarner(out io.Writer, dataDir, sessionID string) *warner {
	return &warner{out: out, dataDir: dataDir, sessionID: sessionID}
}

// warn prints one line for a dropped row. It never prints command text; err
// must therefore be free of payload content.
func (w *warner) warn(err error, hint string) {
	if w == nil || w.out == nil || w.warned {
		return
	}
	if w.sessionID == "" || !_sessionIDPattern.MatchString(w.sessionID) {
		w.warned = true
		w.print(err, hint)
		return
	}
	if !w.createSentinel() {
		return
	}
	w.warned = true
	w.print(err, hint)
}

// createSentinel claims the one-per-session right to warn by exclusively
// creating `<data_dir>/warn-<session_id>` with mode 0600; the directory is
// created with mode 0700 when missing.
func (w *warner) createSentinel() bool {
	name, ok := sentinelName(w.sessionID)
	if !ok {
		return false
	}
	if err := os.MkdirAll(w.dataDir, 0o700); err != nil {
		return false
	}
	file, err := os.OpenFile(filepath.Join(w.dataDir, name), os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return false
	}
	if err := file.Close(); err != nil {
		return false
	}
	return true
}

func sentinelName(sessionID string) (string, bool) {
	if !_sessionIDPattern.MatchString(sessionID) {
		return "", false
	}
	return "warn-" + sessionID, true
}

func (w *warner) print(err error, hint string) {
	// One line, always: driver errors can embed newlines.
	message := strings.ReplaceAll(err.Error(), "\n", " ")
	fmt.Fprintf(w.out, "reki capture: %v (hint: %s)\n", message, hint)
}

// warnPayloadFailure warns for a payload that could not be captured. The
// session ID is best-effort: a fully undecodable document warns once per
// process, while a decode with a usable session ID still uses the sentinel.
func warnPayloadFailure(out io.Writer, sessionID string, err error, hint string) {
	dataDir := ""
	if dir, dirErr := storage.Dir(); dirErr == nil {
		dataDir = dir
	}
	newWarner(out, dataDir, sessionID).warn(err, hint)
}

// hintFor maps a failure to its user hint. The lookups stay simple on
// purpose: SQLite corruption is matched on driver text per this stage's plan.
func hintFor(err error) string {
	if errors.Is(err, storage.ErrDatabaseNotFound) {
		return _hintInit
	}
	message := err.Error()
	if strings.Contains(message, "malformed") || strings.Contains(message, "not a database") {
		return _hintDoctor
	}
	return _hintGeneric
}
