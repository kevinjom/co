package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/kevinjom/co/internal/command"
	"github.com/kevinjom/co/internal/provider"
	"github.com/kevinjom/co/internal/transcript"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func newRootCmd() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:           "co",
		Short:         "Read-only coding assistant for the current directory",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return initConfig(cmd)
		},
		RunE: run,
	}

	cmd.PersistentFlags().String("config", "", "config file (default $HOME/.co/config.yaml)")
	cmd.PersistentFlags().String("log-level", "info", "log level (debug, info, warn, error)")
	cmd.Flags().String("model", "", "model as provider/model-id")
	cmd.Flags().String("resume", "", "resume a session id, or latest")
	cmd.Flags().Bool("no-history", false, "do not save the session transcript")
	cmd.Flags().Bool("clear-scrollback", false, "erase the terminal scrollback before the TUI starts")

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
		if err := setDefaultConfig(home); err != nil {
			return err
		}
	}

	viper.SetEnvPrefix("CO")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	if err := viper.BindEnv("model", "CO_MODEL"); err != nil {
		return fmt.Errorf("binding model: %w", err)
	}
	if err := viper.BindEnv("log-level", "CO_LOG_LEVEL"); err != nil {
		return fmt.Errorf("binding log-level: %w", err)
	}
	if err := viper.BindEnv("resume", "CO_RESUME"); err != nil {
		return fmt.Errorf("binding resume: %w", err)
	}
	if err := viper.BindEnv("no-history", "CO_NO_HISTORY"); err != nil {
		return fmt.Errorf("binding no-history: %w", err)
	}
	if err := viper.BindEnv("clear-scrollback", "CO_CLEAR_SCROLLBACK"); err != nil {
		return fmt.Errorf("binding clear-scrollback: %w", err)
	}
	viper.SetDefault("log-level", "info")
	viper.SetDefault("clear-scrollback", false)

	if err := viper.ReadInConfig(); err != nil {
		var missing viper.ConfigFileNotFoundError
		if !errors.As(err, &missing) {
			return fmt.Errorf("reading config: %w", err)
		}
	}
	// resume is a one-shot flag. A value in the settings file would reopen
	// the same session on every launch.
	ignoreConfigResume(cmd)

	level, err := parseLogLevel(viper.GetString("log-level"))
	if err != nil {
		return err
	}
	handler := slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
	return nil
}

// setDefaultConfig prefers ~/.co/config.yaml and otherwise uses ./.co.yaml.
// That file holds the default model, log-level, no-history, and clear-scrollback.
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

func ignoreConfigResume(cmd *cobra.Command) {
	if strings.TrimSpace(os.Getenv("CO_RESUME")) != "" {
		return
	}
	flag := cmd.Root().Flags().Lookup("resume")
	if flag != nil && flag.Changed {
		return
	}
	viper.Set("resume", "")
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, usageError(fmt.Errorf("invalid log-level %q", raw))
	}
}

func run(cmd *cobra.Command, _ []string) error {
	return start(cmd, "")
}

func start(cmd *cobra.Command, modelOverride string) error {
	flagModel := explicitModel(cmd)
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

	app := command.New(cat, home, cwd, viper.GetBool("no-history"))
	defer app.Close()

	resumeID := strings.TrimSpace(viper.GetString("resume"))
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
	return repl(cmd, signals, app, cwd, viper.GetBool("clear-scrollback"))
}

func explicitModel(cmd *cobra.Command) string {
	if cmd.Flags().Changed("model") {
		return strings.TrimSpace(viper.GetString("model"))
	}
	return strings.TrimSpace(os.Getenv("CO_MODEL"))
}
