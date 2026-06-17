package main

import (
	"fmt"
	"os"

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
		stub("init", "Create the local DB and install the shell hook + Ctrl+R binding"),
		stub("import <shell>", "Import an existing HISTFILE (zsh|bash|fish)"),
		stub("s [query]", "Launch the fuzzy-search TUI"),
		stub("stats", "Show analytics over local history"),
		stub("forget", "Delete entries locally"),
		stub("register", "Create a relay account"),
		stub("login", "Log in to a relay account"),
		stub("key", "Print the encryption key to copy to another machine"),
		stub("sync", "Push/pull encrypted deltas to/from the relay"),
	)
	return root
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
