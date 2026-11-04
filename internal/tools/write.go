package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const maxWriteBytes = 1024 * 1024

func createFileSchema() Schema {
	return Schema{
		Name:        "create_file",
		Description: "Create a new text file. Fails if the path already exists. Parent directories are created. Cannot leave the working directory.",
		Parameters: objectSchema(
			map[string]any{
				"path":     stringProp("File to create, relative to the working directory."),
				"contents": stringProp("Full contents of the new file."),
			},
			[]string{"path", "contents"},
		),
	}
}

func editFileSchema() Schema {
	return Schema{
		Name:        "edit_file",
		Description: "Replace one exact snippet in an existing file. old_string must appear once. Bytes outside that snippet stay unchanged. Fails when the file is missing or the snippet matches zero or more than one time.",
		Parameters: objectSchema(
			map[string]any{
				"path":       stringProp("File to edit, relative to the working directory."),
				"old_string": stringProp("Exact text to replace. It must appear once."),
				"new_string": stringProp("Replacement text. Empty deletes the snippet."),
			},
			[]string{"path", "old_string", "new_string"},
		),
	}
}

func (s *Set) createFile(_ context.Context, args map[string]any) (string, error) {
	path, err := requiredPath(args)
	if err != nil {
		return "", err
	}
	contents, ok, err := requiredString(args, "contents")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("contents is required")
	}
	if len(contents) > maxWriteBytes {
		return "", fmt.Errorf("contents exceed %d bytes", maxWriteBytes)
	}

	dest, err := resolveCreate(s.root, path)
	if err != nil {
		return "", err
	}
	if err := writeNew(filepath.Dir(dest), dest, []byte(contents), 0o644); err != nil {
		if os.IsExist(err) {
			return "", fmt.Errorf("path %q already exists", path)
		}
		return "", fmt.Errorf("creating file: %w", err)
	}
	return fmt.Sprintf("created %s (%d bytes)", path, len(contents)), nil
}

func (s *Set) editFile(_ context.Context, args map[string]any) (string, error) {
	path, err := requiredPath(args)
	if err != nil {
		return "", err
	}
	old, ok, err := requiredString(args, "old_string")
	if err != nil {
		return "", err
	}
	if !ok || old == "" {
		return "", fmt.Errorf("old_string is required")
	}
	replacement, ok, err := requiredString(args, "new_string")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("new_string is required")
	}

	full, err := resolve(s.root, path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path %q is a directory", path)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("path %q is not a regular file", path)
	}
	if info.Size() > maxWriteBytes {
		return "", fmt.Errorf("file exceeds %d bytes", maxWriteBytes)
	}

	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}
	matches := strings.Count(string(data), old)
	if matches != 1 {
		return "", fmt.Errorf("old_string matched %d times", matches)
	}
	updated := strings.Replace(string(data), old, replacement, 1)
	if len(updated) > maxWriteBytes {
		return "", fmt.Errorf("contents exceed %d bytes", maxWriteBytes)
	}
	if err := writeReplace(filepath.Dir(full), full, []byte(updated), info.Mode().Perm()); err != nil {
		return "", err
	}
	return fmt.Sprintf("edited %s", path), nil
}

func requiredPath(args map[string]any) (string, error) {
	path, ok, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	return path, nil
}

func writeNew(dir, dest string, data []byte, mode os.FileMode) error {
	tmp, err := writeTemp(dir, data, mode)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if err := os.Link(tmp, dest); err != nil {
		return err
	}
	return nil
}

func writeReplace(dir, dest string, data []byte, mode os.FileMode) error {
	tmp, err := writeTemp(dir, data, mode)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("writing file: %w", err)
	}
	return nil
}

func writeTemp(dir string, data []byte, mode os.FileMode) (string, error) {
	tmp, err := os.CreateTemp(dir, ".co-write-*")
	if err != nil {
		return "", fmt.Errorf("writing file: %w", err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return "", fmt.Errorf("writing file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(name)
		return "", fmt.Errorf("writing file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("writing file: %w", err)
	}
	return name, nil
}
