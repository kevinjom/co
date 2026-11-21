package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const agentsmdFile = "AGENTS.md"

// loadAgentsmd returns the system-prompt section for the closest AGENTS.md.
// An empty string means the prompt stays as it is without project instructions.
//
// Lookup starts at cwd and climbs to the git root, inclusive. The git root is
// the nearest directory that contains a .git entry (a directory, a file, or a
// symlink). The first usable AGENTS.md on that walk wins. When cwd is not
// inside a git repository, only cwd/AGENTS.md is considered.
//
// A closer file that is empty, not UTF-8, or unreadable hides any parent file.
func loadAgentsmd(cwd string) string {
	start, err := filepath.Abs(cwd)
	if err != nil {
		slog.Warn("agentsmd", "path", cwd, "err", err)
		return ""
	}
	root, inRepo := findGitRoot(start)
	dir := start
	for {
		body, found, ok := readAgentsmd(filepath.Join(dir, agentsmdFile))
		if found {
			if !ok {
				return ""
			}
			rel, err := filepath.Rel(start, filepath.Join(dir, agentsmdFile))
			if err != nil {
				rel = agentsmdFile
			}
			return agentsmdBlock(body, filepath.ToSlash(rel))
		}
		if !inRepo || dir == root {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// findGitRoot returns the nearest directory at or above start that contains a .git entry.
func findGitRoot(start string) (string, bool) {
	dir := start
	for {
		_, err := os.Lstat(filepath.Join(dir, ".git"))
		if err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// readAgentsmd reports whether a regular file named AGENTS.md is present.
// found is set when that file exists, including when its contents cannot be used.
// ok is set when found is set and body should be shown.
func readAgentsmd(path string) (body string, found, ok bool) {
	info, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("agentsmd", "path", path, "err", err)
			return "", true, false
		}
		return "", false, false
	}
	if !info.Mode().IsRegular() {
		return "", false, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("agentsmd", "path", path, "err", err)
		return "", true, false
	}
	if !utf8.Valid(raw) {
		slog.Warn("agentsmd", "path", path, "err", "file is not UTF-8")
		return "", true, false
	}
	text := strings.TrimSpace(strings.TrimPrefix(string(raw), "\uFEFF"))
	if text == "" {
		return "", true, false
	}
	return text, true, true
}

func agentsmdBlock(body, rel string) string {
	return strings.Join([]string{
		"Project instructions from " + rel + ":",
		body,
		"Tool rules above still apply.",
		"The user's message wins when it conflicts with these project instructions.",
	}, "\n")
}
