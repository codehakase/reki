package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// Dir returns Reki's data directory, honoring XDG_DATA_HOME.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "reki"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("storage: resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "reki"), nil
}

// Path returns the local history database path.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.db"), nil
}
