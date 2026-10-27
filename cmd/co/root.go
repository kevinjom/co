package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"co/internal/command"
	"co/internal/provider"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const defaultHost = "http://127.0.0.1:11434"

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

	cmd.PersistentFlags().String("config", "", "config file (default $HOME/.co.yaml)")
	cmd.PersistentFlags().String("log-level", "info", "log level (debug, info, warn, error)")
	cmd.Flags().String("host", defaultHost, "Ollama host")
	cmd.Flags().String("model", "", "model as provider/model-id")
	cmd.Flags().Bool("no-history", false, "do not save the session transcript")

	if err := bindFlag(cmd.PersistentFlags(), "config"); err != nil {
		return nil, err
	}
	if err := bindFlag(cmd.PersistentFlags(), "log-level"); err != nil {
		return nil, err
	}
	if err := bindFlag(cmd.Flags(), "host"); err != nil {
		return nil, err
	}
	if err := bindFlag(cmd.Flags(), "model"); err != nil {
		return nil, err
	}
	if err := bindFlag(cmd.Flags(), "no-history"); err != nil {
		return nil, err
	}

	cmd.AddCommand(newVersionCmd())
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
		viper.AddConfigPath(home)
		viper.AddConfigPath(".")
		viper.SetConfigName(".co")
		viper.SetConfigType("yaml")
	}

	viper.SetEnvPrefix("CO")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	if err := viper.BindEnv("host", "CO_HOST", "OLLAMA_HOST"); err != nil {
		return fmt.Errorf("binding host: %w", err)
	}
	if err := viper.BindEnv("model", "CO_MODEL"); err != nil {
		return fmt.Errorf("binding model: %w", err)
	}
	if err := viper.BindEnv("log-level", "CO_LOG_LEVEL"); err != nil {
		return fmt.Errorf("binding log-level: %w", err)
	}
	if err := viper.BindEnv("no-history", "CO_NO_HISTORY"); err != nil {
		return fmt.Errorf("binding no-history: %w", err)
	}
	viper.SetDefault("host", defaultHost)
	viper.SetDefault("log-level", "info")

	if err := viper.ReadInConfig(); err != nil {
		var missing viper.ConfigFileNotFoundError
		if !errors.As(err, &missing) {
			return fmt.Errorf("reading config: %w", err)
		}
	}

	level, err := parseLogLevel(viper.GetString("log-level"))
	if err != nil {
		return err
	}
	handler := slog.NewTextHandler(cmd.ErrOrStderr(), &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))
	return nil
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
	hostSet := cmd.Flags().Changed("host") || os.Getenv("CO_HOST") != "" || os.Getenv("OLLAMA_HOST") != ""
	host := ""
	if hostSet {
		host = viper.GetString("host")
		if strings.TrimSpace(host) == "" {
			return usageError(fmt.Errorf("host is required: set --host or OLLAMA_HOST"))
		}
	}

	flagModel := ""
	if cmd.Flags().Changed("model") {
		flagModel = viper.GetString("model")
	} else if os.Getenv("CO_MODEL") != "" {
		flagModel = os.Getenv("CO_MODEL")
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
		Host:       host,
		HostSet:    hostSet,
	})
	if err != nil {
		return err
	}

	signals := startSignals(cmd.Context())
	defer signals.close()

	app := command.New(cat, home, cwd, viper.GetBool("no-history"))
	defer app.Close()

	interactive := isTerminal(cmd.InOrStdin()) && isTerminal(cmd.OutOrStdout())
	if model := cat.Selected(); model == "" {
		if !interactive {
			return usageError(fmt.Errorf("model is required: set --model as provider/model-id, or set model in ~/.co.yaml"))
		}
	} else if err := app.Activate(signals.ctx, model); err != nil {
		if sigErr := signals.err(); sigErr != nil {
			return sigErr
		}
		var parseErr *provider.ParseError
		if errors.As(err, &parseErr) {
			return usageError(err)
		}
		return err
	}
	return repl(cmd, signals, app, cwd)
}
