package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolve returns the real path of path, which must stay inside root.
// Symlinks are evaluated. A path that escapes root returns an error.
func resolve(root, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	realRoot, absCandidate, err := absInRoot(root, path)
	if err != nil {
		return "", err
	}
	realCandidate, err := filepath.EvalSymlinks(absCandidate)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", path, err)
	}
	if err := within(realRoot, realCandidate, path); err != nil {
		return "", err
	}
	return realCandidate, nil
}

// resolveCreate returns the path of a new file inside root.
// The file must not exist. Missing parent directories are created.
// A symlink that leaves root is rejected.
func resolveCreate(root, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	realRoot, absCandidate, err := absInRoot(root, path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(realRoot, absCandidate)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", path, err)
	}
	if rel == "." {
		return "", fmt.Errorf("path %q is a directory", path)
	}
	if err := within(realRoot, absCandidate, path); err != nil {
		return "", err
	}

	current := realRoot
	parts := strings.Split(rel, string(filepath.Separator))
	pending := -1
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("path %q is outside the working directory", path)
		}
		next := filepath.Join(current, part)
		info, err := os.Lstat(next)
		if err != nil {
			if os.IsNotExist(err) {
				pending = i
				break
			}
			return "", fmt.Errorf("path %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(next)
			if err != nil {
				return "", fmt.Errorf("path %q: %w", path, err)
			}
			if err := within(realRoot, resolved, path); err != nil {
				return "", err
			}
			current = resolved
			continue
		}
		current = next
	}
	if pending < 0 {
		info, err := os.Stat(current)
		if err != nil {
			return "", fmt.Errorf("path %q: %w", path, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("path %q is a directory", path)
		}
		return "", fmt.Errorf("path %q already exists", path)
	}

	for _, part := range parts[pending : len(parts)-1] {
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0o755); err != nil {
			return "", fmt.Errorf("creating directory: %w", err)
		}
		if err := os.Chmod(current, 0o755); err != nil {
			return "", fmt.Errorf("creating directory: %w", err)
		}
	}
	return filepath.Join(current, parts[len(parts)-1]), nil
}

func absInRoot(root, path string) (string, string, error) {
	realRoot, err := jailRoot(root)
	if err != nil {
		return "", "", err
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(realRoot, candidate)
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", "", fmt.Errorf("path %q: %w", path, err)
	}
	return realRoot, filepath.Clean(absCandidate), nil
}

func jailRoot(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("working directory: %w", err)
	}
	return realRoot, nil
}

func within(root, candidate, display string) error {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return fmt.Errorf("path %q: %w", display, err)
	}
	escapes := rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
	if escapes || filepath.IsAbs(rel) {
		return fmt.Errorf("path %q is outside the working directory", display)
	}
	return nil
}
