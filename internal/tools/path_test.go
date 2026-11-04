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

func TestResolveCreateMakesParentsAndRefusesExisting(t *testing.T) {
	root := t.TempDir()
	dest, err := resolveCreate(root, "a/b/c.txt")
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(dest)
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode().Perm() != 0o755 {
		t.Fatalf("parent mode %s", info.Mode().Perm())
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Fatalf("file should not exist yet: %v", err)
	}

	if err := os.WriteFile(dest, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveCreate(root, "a/b/c.txt"); err == nil {
		t.Fatal("existing file: expected an error")
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveCreate(root, "dir"); err == nil {
		t.Fatal("directory: expected an error")
	}
}

func TestResolveCreateRejectsEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	escaped := filepath.Join(filepath.Dir(root), "co-escaped.txt")

	for _, candidate := range []string{"../co-escaped.txt", escaped, "link/secret.txt"} {
		if _, err := resolveCreate(root, candidate); err == nil {
			t.Fatalf("resolveCreate %q: expected an error", candidate)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); !os.IsNotExist(err) {
		t.Fatalf("wrote through symlink: %v", err)
	}
	if _, err := os.Stat(escaped); !os.IsNotExist(err) {
		t.Fatalf("wrote outside root: %v", err)
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
