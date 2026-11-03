package main

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/kevinjom/co/internal/config"
	"github.com/kevinjom/co/internal/transcript"

	"github.com/spf13/cobra"
)

func newSessionCmd() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:           "session",
		Short:         "Manage saved sessions",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	list, err := newSessionListCmd()
	if err != nil {
		return nil, err
	}
	cmd.AddCommand(list)
	return cmd, nil
}

func newSessionListCmd() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:           "list",
		Short:         "List recent sessions for this directory",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runSessionList,
	}
	cmd.Flags().IntP(
		"limit",
		"n",
		config.DefaultSessionLimit,
		"maximum number of sessions to list",
	)
	if err := bindFlag(cmd.Flags(), "limit"); err != nil {
		return nil, err
	}
	return cmd, nil
}

func runSessionList(cmd *cobra.Command, _ []string) error {
	limit, err := sessionLimit(cmd)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}
	sessions, err := transcript.List(home, cwd, limit)
	if err != nil {
		return err
	}
	return writeSessionList(cmd.OutOrStdout(), sessions)
}

func sessionLimit(cmd *cobra.Command) (int, error) {
	limit, err := config.SessionLimit(cmd.Flags().Changed("limit"))
	if err != nil {
		return 0, usageError(err)
	}
	return limit, nil
}

func writeSessionList(w io.Writer, sessions []transcript.Summary) error {
	if len(sessions) == 0 {
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tUPDATED\tMODEL\tTITLE"); err != nil {
		return err
	}
	for _, session := range sessions {
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\n",
			session.ID,
			session.Updated.UTC().Format(time.RFC3339),
			session.Model,
			session.Title,
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}
