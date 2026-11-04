package tools

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
)

func listDirSchema() Schema {
	return Schema{
		Name:        "list_dir",
		Description: "List one directory level. Each entry is marked file or dir. Path defaults to the working directory.",
		Parameters: objectSchema(
			map[string]any{
				"path": stringProp("Directory to list, relative to the working directory. Defaults to ."),
			},
			[]string{},
		),
	}
}

func (s *Set) listDir(_ context.Context, args map[string]any) (string, error) {
	path, err := optionalPath(args)
	if err != nil {
		return "", err
	}
	full, err := resolve(s.root, path)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path %q is not a directory", path)
	}

	entries, err := os.ReadDir(full)
	if err != nil {
		return "", fmt.Errorf("listing %q: %w", path, err)
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	var b strings.Builder
	fmt.Fprintf(&b, "entries: %d\n", len(entries))
	for _, entry := range entries {
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		fmt.Fprintf(&b, "%s %s\n", kind, entry.Name())
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
