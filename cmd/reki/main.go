package main

import (
	"fmt"
	"os"

	"github.com/codehakase/reki/internal/capture"
	"github.com/codehakase/reki/internal/shell"
	"github.com/codehakase/reki/internal/storage"
	"github.com/spf13/cobra"
)

// version is the build version, overridable via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "reki",
		Short:         "Local-first shell history with fast search",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newInitCmd(),
		newCaptureCmd(),
		stub("import <shell>", "Import an existing HISTFILE (zsh|fish)"),
		stub("s [query]", "Launch the fuzzy-search TUI"),
		stub("stats", "Show analytics over local history"),
		stub("forget <selector>", "Permanently delete matching entries"),
		stub("export", "Write all history to stdout"),
		stub("doctor", "Report instance health and configuration"),
	)
	return root
}

// newInitCmd creates and migrates the local database, then installs the Zsh
// integration in an idempotent guarded rc-file block.
func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create the local DB and install the Zsh capture hook",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			path, err := storage.Path()
			if err != nil {
				return fmt.Errorf("init: resolve database path: %w", err)
			}
			existed := false
			if _, statErr := os.Stat(path); statErr == nil {
				existed = true
			}
			store, err := storage.OpenOrCreate(ctx, path)
			if err != nil {
				return fmt.Errorf("init: open database %s: %w", path, err)
			}
			defer func() { _ = store.Close() }()
			if err := store.Migrate(ctx); err != nil {
				return fmt.Errorf("init: apply migrations: %w", err)
			}
			rcPath, err := shell.ZshRCPath()
			if err != nil {
				return fmt.Errorf("init: resolve zsh rc file: %w", err)
			}
			executable, err := os.Executable()
			if err != nil {
				return fmt.Errorf("init: resolve executable: %w", err)
			}
			if err := shell.InstallZshHook(rcPath, executable); err != nil {
				return fmt.Errorf("init: install zsh hook: %w", err)
			}
			state := "created"
			if existed {
				state = "already present"
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"database %s at %s; migrations up to date; zsh hook installed in %s\n",
				state,
				path,
				rcPath,
			)
			return nil
		},
	}
}

// newCaptureCmd runs the hook-side capture path. It is hidden because shell
// hooks, not users, invoke it; it exits 0 in every case so a failed capture
// never breaks the shell.
func newCaptureCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "capture",
		Short:  "Record one command from a shell hook (internal)",
		Hidden: true,
		Run: func(cmd *cobra.Command, _ []string) {
			capture.Run(cmd.Context(), os.Stdin, os.Stderr)
		},
	}
}

func stub(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("%q is not implemented yet", cmd.Name())
		},
	}
}
