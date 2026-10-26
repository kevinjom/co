package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAcceptsChildAndAbsoluteInsideRoot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.go")
	if err := os.WriteFile(path, []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	want, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, candidate := range []string{"a.go", path} {
		got, err := resolve(root, candidate)
		if err != nil {
			t.Fatalf("resolve %q: %v", candidate, err)
		}
		if got != want {
			t.Fatalf("resolve %q = %s, want %s", candidate, got, want)
		}
	}
}

func TestResolveRejectsEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	cases := []string{"..", secret, "link"}
	for _, candidate := range cases {
		if _, err := resolve(root, candidate); err == nil {
			t.Fatalf("resolve %q: expected an error", candidate)
		}
	}
}
