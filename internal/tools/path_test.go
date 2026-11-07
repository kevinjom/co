package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	t.Run("valid paths", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		filePath := filepath.Join(root, "a.go")
		if err := os.WriteFile(filePath, []byte("package a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		subDir := filepath.Join(root, "sub")
		if err := os.Mkdir(subDir, 0o755); err != nil {
			t.Fatal(err)
		}
		subFile := filepath.Join(subDir, "b.go")
		if err := os.WriteFile(subFile, []byte("package b\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		symlink := filepath.Join(root, "link.go")
		if err := os.Symlink(filePath, symlink); err != nil {
			t.Fatal(err)
		}

		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		realFile, err := filepath.EvalSymlinks(filePath)
		if err != nil {
			t.Fatal(err)
		}
		realSubFile, err := filepath.EvalSymlinks(subFile)
		if err != nil {
			t.Fatal(err)
		}

		tests := []struct {
			name      string
			candidate string
			want      string
		}{
			{
				name:      "relative child in root",
				candidate: "a.go",
				want:      realFile,
			},
			{
				name:      "absolute path in root",
				candidate: filePath,
				want:      realFile,
			},
			{
				name:      "nested relative path",
				candidate: filepath.Join("sub", "b.go"),
				want:      realSubFile,
			},
			{
				name:      "empty path resolves to root",
				candidate: "",
				want:      realRoot,
			},
			{
				name:      "whitespace path resolves to root",
				candidate: "   ",
				want:      realRoot,
			},
			{
				name:      "dot resolves to root",
				candidate: ".",
				want:      realRoot,
			},
			{
				name:      "symlink inside root pointing inside root",
				candidate: "link.go",
				want:      realFile,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := resolve(root, tt.candidate)
				if err != nil {
					t.Fatalf("resolve(%q): unexpected error: %v", tt.candidate, err)
				}
				if got != tt.want {
					t.Fatalf("resolve(%q) = %q, want %q", tt.candidate, got, tt.want)
				}
			})
		}
	})

	t.Run("rejected paths", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		outside := t.TempDir()
		secret := filepath.Join(outside, "secret")
		if err := os.WriteFile(secret, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		symlinkOutside := filepath.Join(root, "link_outside")
		if err := os.Symlink(secret, symlinkOutside); err != nil {
			t.Fatal(err)
		}

		tests := []struct {
			name      string
			candidate string
		}{
			{
				name:      "parent directory escape",
				candidate: "..",
			},
			{
				name:      "deep parent directory escape",
				candidate: filepath.Join("..", "..", "etc", "passwd"),
			},
			{
				name:      "absolute path outside root",
				candidate: secret,
			},
			{
				name:      "symlink pointing outside root",
				candidate: "link_outside",
			},
			{
				name:      "non-existent file",
				candidate: "does_not_exist.txt",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := resolve(root, tt.candidate)
				if err == nil {
					t.Fatalf("resolve(%q) = %q, want error", tt.candidate, got)
				}
			})
		}
	})
}

func TestResolveCreate(t *testing.T) {
	t.Parallel()

	t.Run("makes parent directories and returns target path", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()

		dest, err := resolveCreate(root, "a/b/c.txt")
		if err != nil {
			t.Fatalf("resolveCreate unexpected error: %v", err)
		}

		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		wantDest := filepath.Join(realRoot, "a", "b", "c.txt")
		if dest != wantDest {
			t.Fatalf("resolveCreate = %q, want %q", dest, wantDest)
		}

		parent := filepath.Dir(dest)
		info, err := os.Stat(parent)
		if err != nil {
			t.Fatalf("stat parent: %v", err)
		}
		if !info.IsDir() {
			t.Fatalf("parent is not a directory")
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("parent mode = %s, want %s", info.Mode().Perm(), os.FileMode(0o755))
		}

		if _, err := os.Lstat(dest); !os.IsNotExist(err) {
			t.Fatalf("file should not exist yet: %v", err)
		}
	})

	t.Run("resolves through internal symlink directory", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		targetDir := filepath.Join(root, "real_dir")
		if err := os.Mkdir(targetDir, 0o755); err != nil {
			t.Fatal(err)
		}
		linkDir := filepath.Join(root, "link_dir")
		if err := os.Symlink(targetDir, linkDir); err != nil {
			t.Fatal(err)
		}

		dest, err := resolveCreate(root, "link_dir/sub/file.txt")
		if err != nil {
			t.Fatalf("resolveCreate unexpected error: %v", err)
		}

		realTarget, err := filepath.EvalSymlinks(targetDir)
		if err != nil {
			t.Fatal(err)
		}
		wantDest := filepath.Join(realTarget, "sub", "file.txt")
		if dest != wantDest {
			t.Fatalf("resolveCreate = %q, want %q", dest, wantDest)
		}
	})

	t.Run("refuses existing target", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()

		dest, err := resolveCreate(root, "file.txt")
		if err != nil {
			t.Fatalf("resolveCreate unexpected error: %v", err)
		}
		if err := os.WriteFile(dest, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}

		dirPath := filepath.Join(root, "existing_dir")
		if err := os.Mkdir(dirPath, 0o755); err != nil {
			t.Fatal(err)
		}

		tests := []struct {
			name string
			path string
		}{
			{
				name: "existing file",
				path: "file.txt",
			},
			{
				name: "existing directory",
				path: "existing_dir",
			},
			{
				name: "root directory via dot",
				path: ".",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if _, err := resolveCreate(root, tt.path); err == nil {
					t.Fatalf("resolveCreate(%q): expected error, got nil", tt.path)
				}
			})
		}
	})

	t.Run("rejects escapes and invalid paths", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		escaped := filepath.Join(filepath.Dir(root), "co-escaped.txt")

		tests := []struct {
			name      string
			candidate string
		}{
			{
				name:      "empty path",
				candidate: "",
			},
			{
				name:      "whitespace only",
				candidate: "   \t\n",
			},
			{
				name:      "relative escape with parent",
				candidate: "../co-escaped.txt",
			},
			{
				name:      "absolute path outside root",
				candidate: escaped,
			},
			{
				name:      "symlink escaping root",
				candidate: "link/secret.txt",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if _, err := resolveCreate(root, tt.candidate); err == nil {
					t.Fatalf("resolveCreate(%q): expected error, got nil", tt.candidate)
				}
			})
		}

		if _, err := os.Stat(filepath.Join(outside, "secret.txt")); !os.IsNotExist(err) {
			t.Fatalf("unexpected file created through symlink: %v", err)
		}
		if _, err := os.Stat(escaped); !os.IsNotExist(err) {
			t.Fatalf("unexpected file created outside root: %v", err)
		}
	})
}
