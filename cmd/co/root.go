package main

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kevinjom/co/internal/command"
	"github.com/kevinjom/co/internal/config"
	"github.com/kevinjom/co/internal/logs"
	"github.com/kevinjom/co/internal/provider"
	"github.com/kevinjom/co/internal/theme"
	"github.com/kevinjom/co/internal/transcript"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func newRootCmd() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:           "co",
		Short:         "an coding assistant that cooks code for the current directory",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return initConfig(cmd)
		},
		RunE: run,
	}

	cmd.PersistentFlags().String("config", "", "config file (default $HOME/.co/config.yaml)")
	cmd.PersistentFlags().String("log-level", config.DefaultLogLevel, "log level (debug, info, warn, error)")
	cmd.Flags().String("model", "", "model as provider/model-id")
	cmd.Flags().String("resume", "", "resume a session id, or latest")
	cmd.Flags().Bool("no-history", false, "do not save the session transcript")
	cmd.Flags().Bool("clear-scrollback", false, "erase the terminal scrollback before the TUI starts")
	cmd.Flags().Int("max-tool-rounds", config.DefaultMaxToolRounds, "model calls that may include tools")
	cmd.Flags().String("theme", theme.Default, "TUI color theme ("+strings.Join(theme.Names(), ", ")+")")

	if err := bindFlag(cmd.PersistentFlags(), "config"); err != nil {
		return nil, err
	}
	if err := bindFlag(cmd.PersistentFlags(), "log-level"); err != nil {
		return nil, err
	}
	if err := bindFlag(cmd.Flags(), "model"); err != nil {
		return nil, err
	}
	if err := bindFlag(cmd.Flags(), "resume"); err != nil {
		return nil, err
	}
	if err := bindFlag(cmd.Flags(), "no-history"); err != nil {
		return nil, err
	}
	if err := bindFlag(cmd.Flags(), "clear-scrollback"); err != nil {
		return nil, err
	}
	if err := bindFlag(cmd.Flags(), "max-tool-rounds"); err != nil {
		return nil, err
	}
	if err := bindFlag(cmd.Flags(), "theme"); err != nil {
		return nil, err
	}

	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newOllamaCmd())
	sessionCmd, err := newSessionCmd()
	if err != nil {
		return nil, err
	}
	cmd.AddCommand(sessionCmd)
	return cmd, nil
}

func bindFlag(set *pflag.FlagSet, name string) error {
	flag := set.Lookup(name)
	if flag == nil {
		return fmt.Errorf("flag %q is not defined", name)
	}
	if err := viper.BindPFlag(name, flag); err != nil {
		return fmt.Errorf("binding flag %q: %w", name, err)
	}
	return nil
}

func initConfig(cmd *cobra.Command) error {
	cfgFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("finding home directory: %w", err)
		}
		if err := config.EnsureFile(provider.HomeConfig(home)); err != nil {
			return err
		}
		if err := provider.EnsureModels(provider.HomeModels(home)); err != nil {
			return err
		}
		if err := setDefaultConfig(home); err != nil {
			return err
		}
	}

	if err := config.Prepare(); err != nil {
		return err
	}

	if err := viper.ReadInConfig(); err != nil {
		var missing viper.ConfigFileNotFoundError
		if !errors.As(err, &missing) {
			return fmt.Errorf("reading config: %w", err)
		}
	}
	// resume is a one-shot flag. A value in the settings file would reopen
	// the same session on every launch.
	config.ClearResume(flagChanged(cmd, "resume"))
	if _, err := config.Load(); err != nil {
		return usageError(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding home directory: %w", err)
	}
	sink, err := logs.Open(home, config.Current().LogRetentionDays)
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{
		Level: config.Current().Level,
	})))
	setLogSink(sink)
	return nil
}

var (
	logSinkMu sync.Mutex
	logSink   io.Closer
)

func setLogSink(next io.Closer) {
	logSinkMu.Lock()
	prev := logSink
	logSink = next
	logSinkMu.Unlock()
	if prev != nil {
		_ = prev.Close()
	}
}

func closeLogSink() error {
	logSinkMu.Lock()
	prev := logSink
	logSink = nil
	logSinkMu.Unlock()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if prev == nil {
		return nil
	}
	return prev.Close()
}

// setDefaultConfig prefers ~/.co/config.yaml and otherwise uses ./.co.yaml.
// That file holds the default model, theme, log-level, log-retention-days, no-history, clear-scrollback, and max-tool-rounds.
func setDefaultConfig(home string) error {
	candidates := []string{
		provider.HomeConfig(home),
		filepath.Join(".", ".co.yaml"),
	}
	for _, path := range candidates {
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("reading config: %w", err)
		}
		if info.IsDir() {
			continue
		}
		viper.SetConfigFile(path)
		return nil
	}
	return nil
}

func flagChanged(cmd *cobra.Command, name string) bool {
	flag := cmd.Root().Flags().Lookup(name)
	return flag != nil && flag.Changed
}

func run(cmd *cobra.Command, _ []string) error {
	return start(cmd, "")
}

func start(cmd *cobra.Command, modelOverride string) error {
	flagModel := config.ExplicitModel(flagChanged(cmd, "model"))
	if strings.TrimSpace(modelOverride) != "" {
		flagModel = strings.TrimSpace(modelOverride)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("finding home directory: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}
	configFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return err
	}

	cat, err := provider.Load(provider.LoadOptions{
		Home:       home,
		ProjectDir: cwd,
		ConfigFile: configFile,
		FlagModel:  flagModel,
	})
	if err != nil {
		return err
	}

	signals := startSignals(cmd.Context())
	defer signals.close()

	settings := config.Current()
	app := command.New(
		cat,
		home,
		cwd,
		settings.NoHistory,
		settings.MaxToolRounds,
	)
	defer app.Close()

	resumeID := settings.Resume
	interactive := isTerminal(cmd.InOrStdin()) && isTerminal(cmd.OutOrStdout())
	if resumeID != "" {
		if err := app.Resume(signals.ctx, resumeID, flagModel); err != nil {
			if sigErr := signals.err(); sigErr != nil {
				return sigErr
			}
			var parseErr *provider.ParseError
			var inputErr *transcript.InputError
			if errors.As(err, &parseErr) || errors.As(err, &inputErr) {
				return usageError(err)
			}
			return err
		}
	} else if model := cat.Selected(); model == "" {
		if !interactive {
			return usageError(fmt.Errorf("model is required: set --model as provider/model-id, or set model in ~/.co/config.yaml"))
		}
	} else if err := app.Activate(signals.ctx, model); err != nil {
		if sigErr := signals.err(); sigErr != nil {
			return sigErr
		}
		var parseErr *provider.ParseError
		if errors.As(err, &parseErr) {
			return usageError(err)
		}
		err = hintUnknownProvider(err)
		if !interactive {
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), err.Error())
	}
	return repl(cmd, signals, app, cwd, settings.ClearScrollback, settings.Theme)
}
