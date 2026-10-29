package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Config holds reki's user-facing settings. It never contains secrets.
type Config struct {
	// RedactionExtra holds user-supplied regex patterns that extend the
	// default redaction denylist. Each pattern's full match is replaced
	// with "[redacted]".
	RedactionExtra []string `json:"redaction_extra_patterns,omitempty"`

	extraPatterns []*regexp.Regexp
}

// _redactionFlagPattern matches password/token/secret-style CLI flags with a
// value passed either as flag=value or flag value. The value must not start
// with a shell operator, so redirects and pipes stay unredacted. Groups 1-3
// must survive replacement; only group 4 (the value) is redacted.
const _redactionFlagPattern = `(?i)(^|\s)(--(?:[A-Za-z][[:word:]-]*)?(?:password|passwd|pwd|token|secret|api[-_]?key|apikey)[[:word:]-]*)(=|[ \t]+)([^[:space:]<>|;&]\S*)`

// _redactionPatterns is the default denylist.
var _redactionPatterns = []*regexp.Regexp{
	// AWS access key id.
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	// GitHub PAT (ghp_/gho_/ghu_/ghs_/ghr_) and fine-grained token.
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{30,}`),
	// Slack token (xoxb/xoxp/xoxa/xoxs/xoxr/xoxe).
	regexp.MustCompile(`xox[bpersa]-[A-Za-z0-9-]{10,}`),
	// PEM private key header, including RSA/EC/OPENSSH/ENCRYPTED variants.
	regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`),
	// Secret-value CLI flags; replacement keeps groups 1-3, redacts the value.
	regexp.MustCompile(_redactionFlagPattern),
}

// Dir returns reki's config directory, honoring XDG_CONFIG_HOME.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "reki"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "reki"), nil
}

// Path returns the path to reki's config.json.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads config.json and returns the config. A missing file yields the
// defaults. Other read failures are returned so capture never silently skips
// user-supplied redaction patterns.
func Load() (*Config, error) {
	cfg := &Config{}
	path, err := Path()
	if err != nil {
		return nil, fmt.Errorf("config: resolve path: %w", err)
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	if len(data) == 0 || len(bytes.TrimSpace(data)) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	for i, pattern := range cfg.RedactionExtra {
		re, err := regexp.Compile(pattern)
		if err != nil {
			// Do not include the pattern or regexp error: a user may have put a
			// literal secret in the pattern by mistake.
			return nil, fmt.Errorf(
				"config: redaction_extra_patterns[%d]: invalid regular expression",
				i,
			)
		}
		cfg.extraPatterns = append(cfg.extraPatterns, re)
	}
	return cfg, nil
}

// Redact applies the default denylist to command.
func Redact(command string) string {
	for _, re := range _redactionPatterns {
		command = replace(re, command)
	}
	return command
}

// Redact applies the default denylist plus the user's extra patterns to
// command. Extra patterns replace their full match with "[redacted]".
func (c *Config) Redact(command string) string {
	command = Redact(command)
	for _, re := range c.extraPatterns {
		command = re.ReplaceAllString(command, "[redacted]")
	}
	return command
}

// replace rewrites one default pattern. The flag pattern must keep groups
// 1-3 (banner, flag text, separator) and redact only the value in group 4;
// other patterns have no capture groups, so a plain replacement suffices.
func replace(re *regexp.Regexp, command string) string {
	if re.String() != _redactionFlagPattern {
		return re.ReplaceAllString(command, "[redacted]")
	}
	return re.ReplaceAllString(command, "${1}${2}${3}[redacted]")
}
