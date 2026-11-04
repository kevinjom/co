package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/viper"
)

func main() {
	if err := Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(exitCode(err))
	}
}

// Execute runs the root command.
func Execute() error {
	viper.Reset()
	cmd, err := newRootCmd()
	if err != nil {
		return err
	}
	runErr := cmd.ExecuteContext(context.Background())
	closeErr := closeLogSink()
	if runErr != nil {
		return runErr
	}
	return closeErr
}

func exitCode(err error) int {
	var exitErr *exitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return 1
}
