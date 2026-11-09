package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// EnsureFile creates path with the default settings when it is missing.
// An existing file, including an empty one, is left unchanged.
func EnsureFile(path string) error {
	return writeNew(path, defaultSettings())
}

func defaultSettings() string {
	return fmt.Sprintf(
		"theme: %s\n"+
			"log-level: %s\n"+
			"log-retention-days: %d\n"+
			"no-history: false\n"+
			"clear-scrollback: false\n"+
			"max-tool-rounds: %d\n",
		DefaultTheme,
		DefaultLogLevel,
		DefaultLogRetentionDays,
		DefaultMaxToolRounds,
	)
}

func writeNew(path, body string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	_, writeErr := io.WriteString(f, body)
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(path)
		return fmt.Errorf("writing config %s: %w", path, writeErr)
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return fmt.Errorf("writing config %s: %w", path, closeErr)
	}
	return nil
}
