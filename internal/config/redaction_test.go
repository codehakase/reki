package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactDefaults(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "aws access key",
			in:   "aws configure set aws_access_key_id AKIAIOSFODNN7EXAMPLE",
			want: "aws configure set aws_access_key_id [redacted]",
		},
		{
			name: "aws key mid-word not matched without full length",
			in:   "echo AKIA1234",
			want: "echo AKIA1234",
		},
		{
			name: "github classic pat",
			in:   "git push https://ghp_0123456789abcdefghijklmnopqrstuvwxyz@github.com/x/y",
			want: "git push https://[redacted]@github.com/x/y",
		},
		{
			name: "github app token",
			in:   "curl -H 'Authorization: token ghs_0123456789abcdefghijklmnopqrstuvwxyz' https://api.github.com",
			want: "curl -H 'Authorization: token [redacted]' https://api.github.com",
		},
		{
			name: "github fine-grained token",
			in:   "gh auth login --with-token <<< github_pat_0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF",
			want: "gh auth login --with-token <<< [redacted]",
		},
		{
			name: "github reko innocuous",
			in:   "github reko",
			want: "github reko",
		},
		{
			name: "slack bot token",
			in: "slack-cli upload --file x --token " +
				"xoxb-123456789012-1234567890123-" + "abcdefghijklmnop",
			want: "slack-cli upload --file x --token [redacted]",
		},
		{
			name: "slack user token",
			in:   "curl -H 'Authorization: Bearer xoxp-1-2-abcdef0123' https://slack.com",
			want: "curl -H 'Authorization: Bearer [redacted]' https://slack.com",
		},
		{
			name: "pem rsa private key header",
			in:   "openssl genrsa > key.pem && sed -n 1p <(echo '-----BEGIN RSA PRIVATE KEY-----')",
			want: "openssl genrsa > key.pem && sed -n 1p <(echo '[redacted]')",
		},
		{
			name: "pem openssh private key header",
			in:   "head -1 id_ed25519 # -----BEGIN OPENSSH PRIVATE KEY-----",
			want: "head -1 id_ed25519 # [redacted]",
		},
		{
			name: "pem private key plain header",
			in:   "cat server.pem # -----BEGIN PRIVATE KEY-----",
			want: "cat server.pem # [redacted]",
		},
		{
			name: "pem public key not matched",
			in:   "cat id_rsa.pub # -----BEGIN PUBLIC KEY-----",
			want: "cat id_rsa.pub # -----BEGIN PUBLIC KEY-----",
		},
		{
			name: "flag equals value",
			in:   "psql --password=hunter2 -c select",
			want: "psql --password=[redacted] -c select",
		},
		{
			name: "flag then separate value",
			in:   "docker login --password hunter2",
			want: "docker login --password [redacted]",
		},
		{
			name: "longer flag names containing keyword",
			in:   "deploy --db-password s3cret --api_token tkn_123 --aws-secret-access-key abc123",
			want: "deploy --db-password [redacted] --api_token [redacted] --aws-secret-access-key [redacted]",
		},
		{
			name: "multi value line",
			in:   "run --password=abc def --token ghi",
			want: "run --password=[redacted] def --token [redacted]",
		},
		{
			name: "flag keyword without value not matched",
			in:   "vault status --token-counter",
			want: "vault status --token-counter",
		},
		{
			name: "kubectl innocuous",
			in:   "kubectl get pods",
			want: "kubectl get pods",
		},
		{
			name: "empty command",
			in:   "",
			want: "",
		},
		{
			name: "no secrets",
			in:   "ls -la ~/.config/reki",
			want: "ls -la ~/.config/reki",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Redact(tt.in); got != tt.want {
				t.Errorf("Redact(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func writeConfig(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "reki"), 0o700); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	path := filepath.Join(dir, "reki", "config.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name       string
		write      bool
		content    string
		wantErr    bool
		errWantIdx string
		wantExtra  int
	}{
		{
			name:    "missing file returns defaults",
			write:   false,
			wantErr: false,
		},
		{
			name:      "empty file returns defaults",
			write:     true,
			content:   "",
			wantExtra: 0,
		},
		{
			name:      "valid extras",
			write:     true,
			content:   `{"redaction_extra_patterns":["MYSECRET[0-9]+"]}`,
			wantExtra: 1,
		},
		{
			name:       "invalid user regex names pattern index",
			write:      true,
			content:    `{"redaction_extra_patterns":["ok[","bad("]}`,
			wantErr:    true,
			errWantIdx: "redaction_extra_patterns[0]",
		},
		{
			name:    "malformed json is an error",
			write:   true,
			content: `{"redaction`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.write {
				writeConfig(t, tt.content)
			} else {
				t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			}
			cfg, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatal("Load() = nil error, want error")
				}
				if tt.errWantIdx != "" && !strings.Contains(err.Error(), tt.errWantIdx) {
					t.Errorf("error %q does not name %q", err, tt.errWantIdx)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if cfg == nil {
				t.Fatal("Load() = nil config, want defaults")
			}
			if len(cfg.extraPatterns) != tt.wantExtra {
				t.Errorf("extraPatterns = %d patterns, want %d", len(cfg.extraPatterns), tt.wantExtra)
			}
		})
	}
}

func TestLoadReturnsExistingFileReadError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "reki", "config.json")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("create directory at config path: %v", err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want a read error")
	}
}

func TestLoadInvalidPatternDoesNotExposePattern(t *testing.T) {
	const sensitivePattern = "literal-secret-("
	writeConfig(t, `{"redaction_extra_patterns":["`+sensitivePattern+`"]}`)
	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid-pattern error")
	}
	if strings.Contains(err.Error(), sensitivePattern) {
		t.Fatalf("Load() error exposes configured pattern: %q", err)
	}
}

func TestConfigRedactExtras(t *testing.T) {
	writeConfig(t, `{"redaction_extra_patterns":["corp_secret_[a-z]+"]}`)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "user pattern applies",
			in:   "deploy --corpsite corp_secret_abcxyz",
			want: "deploy --corpsite [redacted]",
		},
		{
			name: "default patterns still apply",
			in:   "export AKIAIOSFODNN7EXAMPLE corp_secret_abc",
			want: "export [redacted] [redacted]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.Redact(tt.in); got != tt.want {
				t.Errorf("cfg.Redact(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRedactDoesNotLeakSecret(t *testing.T) {
	in := "login --token xoxb-123456789012-1234567890123-abcdefghij"
	got := Redact(in)
	if strings.Contains(got, "abcdefghij") {
		t.Errorf("Redact leaked token fragment: %q", got)
	}
}
