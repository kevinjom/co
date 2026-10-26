package main

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version, commit, and date are set with -ldflags -X at build time.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "version",
		Short:         "Print version information",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"co %s (commit: %s, built: %s)\n",
				version,
				commit,
				date,
			)
			info, ok := debug.ReadBuildInfo()
			if !ok {
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "go: %s\n", info.GoVersion)
			return nil
		},
	}
}
