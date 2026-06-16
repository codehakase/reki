package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config holds seki's user-facing settings. It never contains secrets.
type Config struct{}

// Dir returns seki's config directory, honoring XDG_CONFIG_HOME.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "seki"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "seki"), nil
}

// Path returns the path to seki's config.json.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}
