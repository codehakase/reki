package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config holds reki's user-facing settings. It never contains secrets.
type Config struct{}

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
