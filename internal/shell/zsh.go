// Package shell installs Reki's supported shell integrations.
package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	_zshBlockBegin = "# >>> reki initialize >>>"
	_zshBlockEnd   = "# <<< reki initialize <<<"
)

// ZshRCPath returns the rc file used by zsh, honoring ZDOTDIR when set.
func ZshRCPath() (string, error) {
	if dir := os.Getenv("ZDOTDIR"); dir != "" {
		return filepath.Join(dir, ".zshrc"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("shell: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".zshrc"), nil
}

// InstallZshHook writes or replaces Reki's guarded block in rcPath.
func InstallZshHook(rcPath, rekiPath string) error {
	targetPath, err := resolveRCPath(rcPath)
	if err != nil {
		return err
	}
	contents, mode, err := readRCFile(targetPath)
	if err != nil {
		return err
	}
	updated, err := upsertZshBlock(contents, zshBlock(rekiPath))
	if err != nil {
		return err
	}
	if updated == contents {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return fmt.Errorf("shell: create rc directory: %w", err)
	}
	if err := writeFile(targetPath, []byte(updated), mode); err != nil {
		return fmt.Errorf("shell: write zsh rc file: %w", err)
	}
	return nil
}

func resolveRCPath(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	if err != nil {
		return "", fmt.Errorf("shell: inspect zsh rc file: %w", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("shell: resolve zsh rc symlink: %w", err)
	}
	return resolved, nil
}

func readRCFile(path string) (string, os.FileMode, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", 0o600, nil
	}
	if err != nil {
		return "", 0, fmt.Errorf("shell: read zsh rc file: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, fmt.Errorf("shell: inspect zsh rc mode: %w", err)
	}
	return string(data), info.Mode().Perm(), nil
}

func upsertZshBlock(contents, block string) (string, error) {
	start := strings.Index(contents, _zshBlockBegin)
	if start < 0 {
		separator := ""
		if contents != "" && !strings.HasSuffix(contents, "\n") {
			separator = "\n"
		}
		return contents + separator + block, nil
	}
	endOffset := strings.Index(contents[start:], _zshBlockEnd)
	if endOffset < 0 {
		return "", errors.New("shell: existing Reki block has no end marker")
	}
	end := start + endOffset + len(_zshBlockEnd)
	if end < len(contents) && contents[end] == '\n' {
		end++
	}
	return contents[:start] + block + contents[end:], nil
}

func writeFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".reki-zshrc-*")
	if err != nil {
		return fmt.Errorf("create temporary rc file: %w", err)
	}
	temporaryPath := file.Name()
	defer func() { _ = os.Remove(temporaryPath) }()

	if err := file.Chmod(mode); err != nil {
		_ = file.Close()
		return fmt.Errorf("set temporary rc mode: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write temporary rc file: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync temporary rc file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary rc file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace rc file: %w", err)
	}
	return nil
}

func zshBlock(rekiPath string) string {
	return _zshBlockBegin + "\n" + strings.ReplaceAll(_zshHookTemplate, "@@REKI_BIN@@", zshQuote(rekiPath)) +
		_zshBlockEnd + "\n"
}

func zshQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

const _zshHookTemplate = `typeset -g _REKI_BIN=@@REKI_BIN@@
typeset -g REKI_SESSION
printf -v REKI_SESSION '%x-%x' "$$" $(( (${RANDOM:-0} % 32768) * 32768 + (${RANDOM:-0} % 32768) ))
export REKI_SESSION
zmodload zsh/datetime 2>/dev/null || true

_reki_json_escape() {
  REPLY="${1:-}"
  REPLY="${REPLY//\\/\\\\}"
  REPLY="${REPLY//\"/\\\"}"
  REPLY="${REPLY//$'\b'/\\b}"
  REPLY="${REPLY//$'\f'/\\f}"
  REPLY="${REPLY//$'\n'/\\n}"
  REPLY="${REPLY//$'\r'/\\r}"
  REPLY="${REPLY//$'\t'/\\t}"
}

_reki_preexec() {
  typeset -g _REKI_COMMAND="${1:-}"
  typeset -g _REKI_STARTED_AT="${EPOCHREALTIME:-}"
  typeset -g _REKI_CWD="${PWD:-}"
  typeset -g _REKI_RESOLVED_BINARY=""
  typeset -g _REKI_SUBCOMMAND=""

  local expanded="${3:-${1:-}}" first=""
  local -a words
  words=( "${(z)expanded}" )
  if (( ${#words} >= 1 )); then
    first="${(Q)words[1]}"
    if [[ "$first" == */* && -x "$first" ]]; then
      _REKI_RESOLVED_BINARY="${first:A}"
    elif [[ -n "$first" && -n "${commands[$first]:-}" ]]; then
      _REKI_RESOLVED_BINARY="${commands[$first]}"
    fi
  fi
  if (( ${#words} >= 2 )); then
    _REKI_SUBCOMMAND="${(Q)words[2]}"
  fi
}

_reki_precmd() {
  local exit_code=$?
  setopt localoptions nobgnice
  if [[ -z "${_REKI_COMMAND:-}" ]]; then
    return "$exit_code"
  fi

  local command_json cwd_json binary_json subcommand_json session_json
  local -i has_timing=0 started_ns=0 duration_ms=0
  _reki_json_escape "${_REKI_COMMAND:-}"; command_json="$REPLY"
  _reki_json_escape "${_REKI_CWD:-}"; cwd_json="$REPLY"
  _reki_json_escape "${_REKI_RESOLVED_BINARY:-}"; binary_json="$REPLY"
  _reki_json_escape "${_REKI_SUBCOMMAND:-}"; subcommand_json="$REPLY"
  _reki_json_escape "${REKI_SESSION:-}"; session_json="$REPLY"

  if [[ -n "${_REKI_STARTED_AT:-}" && -n "${EPOCHREALTIME:-}" ]]; then
    started_ns=$(( _REKI_STARTED_AT * 1000000000 ))
    duration_ms=$(( (EPOCHREALTIME - _REKI_STARTED_AT) * 1000 ))
    has_timing=1
  fi

  if (( has_timing )); then
    printf '{"command":"%s","shell":"zsh","exit_code":%d,"session_id":"%s","started_at_ns":%d,"duration_ms":%d,"cwd":"%s","resolved_binary":"%s","subcommand":"%s"}' \
      "$command_json" "$exit_code" "$session_json" "$started_ns" "$duration_ms" "$cwd_json" "$binary_json" "$subcommand_json" \
      | "$_REKI_BIN" capture >/dev/null &!
  else
    printf '{"command":"%s","shell":"zsh","exit_code":%d,"session_id":"%s","cwd":"%s","resolved_binary":"%s","subcommand":"%s"}' \
      "$command_json" "$exit_code" "$session_json" "$cwd_json" "$binary_json" "$subcommand_json" \
      | "$_REKI_BIN" capture >/dev/null &!
  fi

  unset _REKI_COMMAND _REKI_STARTED_AT _REKI_CWD _REKI_RESOLVED_BINARY _REKI_SUBCOMMAND
  return "$exit_code"
}

if (( ${+preexec_functions} )); then
  (( ${preexec_functions[(Ie)_reki_preexec]} )) || preexec_functions=(_reki_preexec "${preexec_functions[@]}")
else
  typeset -ga preexec_functions=(_reki_preexec)
fi
if (( ${+precmd_functions} )); then
  (( ${precmd_functions[(Ie)_reki_precmd]} )) || precmd_functions=(_reki_precmd "${precmd_functions[@]}")
else
  typeset -ga precmd_functions=(_reki_precmd)
fi
`
