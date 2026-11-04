package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrepSkipsGitAndCapsMatches(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte("secret-pattern\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("secret-pattern\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	set := New(root)
	got, err := set.Run(t.Context(), "grep", map[string]any{
		"pattern": "secret-pattern",
		"path":    ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, ".git") {
		t.Fatalf("grep searched .git: %q", got)
	}
	want := "matches: 1\nmain.go:1:secret-pattern"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	var hits strings.Builder
	for range 60 {
		hits.WriteString("hit\n")
	}
	if err := os.WriteFile(filepath.Join(root, "lines.txt"), []byte(hits.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = set.Run(t.Context(), "grep", map[string]any{
		"pattern": "hit",
		"path":    "lines.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "matches: 50\n") || !strings.Contains(got, "truncated: true\n") {
		t.Fatalf("cap not applied: %q", got)
	}
	if strings.Count(got, "hit") != maxGrepMatches {
		t.Fatalf("match lines = %d, want %d", strings.Count(got, "hit"), maxGrepMatches)
	}
}
