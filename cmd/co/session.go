package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/kevinjom/co/internal/transcript"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const defaultSessionLimit = 20

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
		defaultSessionLimit,
		"maximum number of sessions to list",
	)
	if err := bindFlag(cmd.Flags(), "limit"); err != nil {
		return nil, err
	}
	if err := viper.BindEnv("limit", "CO_LIMIT"); err != nil {
		return nil, fmt.Errorf("binding limit: %w", err)
	}
	viper.SetDefault("limit", defaultSessionLimit)
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
	if !cmd.Flags().Changed("limit") {
		if env, ok := os.LookupEnv("CO_LIMIT"); ok && strings.TrimSpace(env) != "" {
			raw := strings.TrimSpace(env)
			if _, err := strconv.Atoi(raw); err != nil {
				return 0, usageError(fmt.Errorf("invalid limit %q", raw))
			}
		}
	}
	limit := viper.GetInt("limit")
	if limit < 1 {
		return 0, usageError(fmt.Errorf("limit must be at least 1"))
	}
	return limit, nil
}

func writeSessionList(w io.Writer, sessions []transcript.Summary) error {
	if len(sessions) == 0 {
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "ID\tUPDATED\tMODEL\tPREVIEW"); err != nil {
		return err
	}
	for _, session := range sessions {
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\n",
			session.ID,
			session.Updated.UTC().Format(time.RFC3339),
			session.Model,
			session.Preview,
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}
