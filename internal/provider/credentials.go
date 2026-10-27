package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func credentialsPath(home string) string {
	return filepath.Join(home, ".co", "credentials.json")
}

func loadKeys(home string) (map[string]string, error) {
	path := credentialsPath(home)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("reading credentials: %w", err)
	}
	if len(data) == 0 {
		return map[string]string{}, nil
	}
	keys := map[string]string{}
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("reading credentials: %w", err)
	}
	if keys == nil {
		keys = map[string]string{}
	}
	return keys, nil
}

func storeKey(home, id, key string) error {
	keys, err := loadKeys(home)
	if err != nil {
		return err
	}
	keys[id] = key

	dir := filepath.Join(home, ".co")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}

	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}
	data = append(data, '\n')

	path := credentialsPath(home)
	tmp, err := os.CreateTemp(dir, "credentials.*.json")
	if err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("saving credentials: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("saving credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}
	cleanup = false
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("saving credentials: %w", err)
	}
	return nil
}
