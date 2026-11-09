package provider

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// defaultModels is the catalog written when ~/.co/models.yaml is missing.
// co ollama adds its provider to this file and leaves these blocks in place.
const defaultModels = `providers:
  gemini:
    base_url: https://generativelanguage.googleapis.com/v1beta/openai
    api_key_env: GEMINI_API_KEY
    models:
      - gemini-flash-latest
  openrouter:
    base_url: https://openrouter.ai/api/v1
    api_key_env: OPENROUTER_API_KEY
    models:
      - qwen/qwen3-coder
`

// EnsureModels creates path with the default providers when it is missing.
// An existing file, including an empty one, is left unchanged.
func EnsureModels(path string) error {
	return writeNew(path, defaultModels)
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
