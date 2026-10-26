package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"co/internal/agent"
	"co/internal/ollama"

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
	cmd.Flags().String("model", "", "Ollama model name")

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
	if err := viper.BindEnv("model", "CO_MODEL", "OLLAMA_MODEL"); err != nil {
		return fmt.Errorf("binding model: %w", err)
	}
	if err := viper.BindEnv("log-level", "CO_LOG_LEVEL"); err != nil {
		return fmt.Errorf("binding log-level: %w", err)
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
	host := viper.GetString("host")
	model := viper.GetString("model")
	if model == "" {
		return usageError(fmt.Errorf("model is required: set --model or OLLAMA_MODEL"))
	}
	if host == "" {
		return usageError(fmt.Errorf("host is required: set --host or OLLAMA_HOST"))
	}

	signals := startSignals(cmd.Context())
	defer signals.close()

	client := ollama.New(host)
	if err := client.Show(signals.ctx, model); err != nil {
		if sigErr := signals.err(); sigErr != nil {
			return sigErr
		}
		return fmt.Errorf("checking model %q: %w", model, err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}
	session := agent.NewSession(client, model, cwd)
	return repl(cmd, signals, session, model, cwd)
}
