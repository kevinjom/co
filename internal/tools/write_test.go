package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateFileWritesAndRefusesSecondTime(t *testing.T) {
	root := t.TempDir()
	set := New(root)
	got, err := set.Run(context.Background(), "create_file", map[string]any{
		"path":     "a/b/c.txt",
		"contents": "hi\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "created a/b/c.txt (3 bytes)" {
		t.Fatalf("result %q", got)
	}
	full := filepath.Join(root, "a", "b", "c.txt")
	body, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hi\n" {
		t.Fatalf("body %q", body)
	}
	info, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %s", info.Mode().Perm())
	}

	if _, err := set.Run(context.Background(), "create_file", map[string]any{
		"path":     "a/b/c.txt",
		"contents": "other",
	}); err == nil {
		t.Fatal("second create: expected an error")
	}
	body, err = os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hi\n" {
		t.Fatalf("body changed to %q", body)
	}
}

func TestCreateFileRejectsOversizedContents(t *testing.T) {
	root := t.TempDir()
	set := New(root)
	_, err := set.Run(context.Background(), "create_file", map[string]any{
		"path":     "big.txt",
		"contents": strings.Repeat("a", maxWriteBytes+1),
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	if _, statErr := os.Stat(filepath.Join(root, "big.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("file exists: %v", statErr)
	}
}

func TestEditFileReplacesOneSnippet(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, "note.txt")
	if err := os.WriteFile(full, []byte("alpha beta alpha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	set := New(root)
	got, err := set.Run(context.Background(), "edit_file", map[string]any{
		"path":       "note.txt",
		"old_string": "beta",
		"new_string": "gamma",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "edited note.txt" {
		t.Fatalf("result %q", got)
	}
	body, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "alpha gamma alpha\n" {
		t.Fatalf("body %q", body)
	}
	info, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %s", info.Mode().Perm())
	}

	got, err = set.Run(context.Background(), "edit_file", map[string]any{
		"path":       "note.txt",
		"old_string": "gamma",
		"new_string": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "edited note.txt" {
		t.Fatalf("result %q", got)
	}
	body, err = os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "alpha  alpha\n" {
		t.Fatalf("body %q", body)
	}
}

func TestEditFileRejectsAmbiguousAndMissing(t *testing.T) {
	root := t.TempDir()
	full := filepath.Join(root, "note.txt")
	original := []byte("alpha beta alpha\n")
	if err := os.WriteFile(full, original, 0o644); err != nil {
		t.Fatal(err)
	}
	set := New(root)
	cases := []map[string]any{
		{"path": "missing.txt", "old_string": "alpha", "new_string": "x"},
		{"path": "note.txt", "old_string": "", "new_string": "x"},
		{"path": "note.txt", "old_string": "nope", "new_string": "x"},
		{"path": "note.txt", "old_string": "alpha", "new_string": "x"},
	}
	for _, args := range cases {
		if _, err := set.Run(context.Background(), "edit_file", args); err == nil {
			t.Fatalf("edit %#v: expected an error", args)
		}
	}
	body, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(original) {
		t.Fatalf("body changed to %q", body)
	}
}

func TestEditFileFollowsSymlinkInsideRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("alpha beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	set := New(root)
	if _, err := set.Run(context.Background(), "edit_file", map[string]any{
		"path":       "link",
		"old_string": "beta",
		"new_string": "gamma",
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "alpha gamma\n" {
		t.Fatalf("body %q", body)
	}
	info, err := os.Lstat(filepath.Join(root, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced")
	}
}

func TestEditFileRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	set := New(root)
	if _, err := set.Run(context.Background(), "edit_file", map[string]any{
		"path":       "link",
		"old_string": "original",
		"new_string": "pwned",
	}); err == nil {
		t.Fatal("expected an error")
	}
	body, err := os.ReadFile(secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "original" {
		t.Fatalf("outside file changed to %q", body)
	}
}
