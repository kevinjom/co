package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// resolve returns the real path of path, which must stay inside root.
// Symlinks are evaluated. A path that escapes root returns an error.
func resolve(root, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}

	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(realRoot, candidate)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", path, err)
	}
	realCandidate, err := filepath.EvalSymlinks(absCandidate)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", path, err)
	}

	rel, err := filepath.Rel(realRoot, realCandidate)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", path, err)
	}
	escapes := rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
	if escapes || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q is outside the working directory", path)
	}
	return realCandidate, nil
}
