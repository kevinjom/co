package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateFileSchema(t *testing.T) {
	t.Parallel()

	schema := createFileSchema()
	if schema.Name != "create_file" {
		t.Fatalf("schema.Name = %q, want %q", schema.Name, "create_file")
	}
	if schema.Description == "" {
		t.Fatal("schema.Description is empty")
	}
	params := schema.Parameters
	if params["type"] != "object" {
		t.Fatalf("params[type] = %v, want object", params["type"])
	}
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatalf("params[required] is not []string: %T", params["required"])
	}
	wantRequired := []string{"path", "contents"}
	if len(required) != len(wantRequired) {
		t.Fatalf("required = %v, want %v", required, wantRequired)
	}
	for i, r := range wantRequired {
		if required[i] != r {
			t.Fatalf("required[%d] = %q, want %q", i, required[i], r)
		}
	}
}

func TestEditFileSchema(t *testing.T) {
	t.Parallel()

	schema := editFileSchema()
	if schema.Name != "edit_file" {
		t.Fatalf("schema.Name = %q, want %q", schema.Name, "edit_file")
	}
	if schema.Description == "" {
		t.Fatal("schema.Description is empty")
	}
	params := schema.Parameters
	if params["type"] != "object" {
		t.Fatalf("params[type] = %v, want object", params["type"])
	}
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatalf("params[required] is not []string: %T", params["required"])
	}
	wantRequired := []string{"path", "old_string", "new_string"}
	if len(required) != len(wantRequired) {
		t.Fatalf("required = %v, want %v", required, wantRequired)
	}
	for i, r := range wantRequired {
		if required[i] != r {
			t.Fatalf("required[%d] = %q, want %q", i, required[i], r)
		}
	}
}

func TestCreateFile(t *testing.T) {
	t.Parallel()

	t.Run("writes file and creates parent directories", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		set := New(root)

		got, err := set.Run(t.Context(), "create_file", map[string]any{
			"path":     "a/b/c.txt",
			"contents": "hi\n",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		const wantResult = "created a/b/c.txt (3 bytes)"
		if got != wantResult {
			t.Fatalf("result = %q, want %q", got, wantResult)
		}

		full := filepath.Join(root, "a", "b", "c.txt")
		body, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(body) != "hi\n" {
			t.Fatalf("file contents = %q, want %q", string(body), "hi\n")
		}

		info, err := os.Stat(full)
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o644 {
			t.Fatalf("file perm = %04o, want 0644", perm)
		}
	})

	t.Run("refuses to overwrite existing file", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		set := New(root)

		if _, err := set.Run(t.Context(), "create_file", map[string]any{
			"path":     "file.txt",
			"contents": "original",
		}); err != nil {
			t.Fatalf("initial create failed: %v", err)
		}

		_, err := set.Run(t.Context(), "create_file", map[string]any{
			"path":     "file.txt",
			"contents": "replacement",
		})
		if err == nil {
			t.Fatal("expected error when file already exists, got nil")
		}
		if !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("error %q does not contain 'already exists'", err.Error())
		}

		body, err := os.ReadFile(filepath.Join(root, "file.txt"))
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(body) != "original" {
			t.Fatalf("file contents changed to %q, want %q", string(body), "original")
		}
	})

	t.Run("rejects oversized contents", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		set := New(root)

		_, err := set.Run(t.Context(), "create_file", map[string]any{
			"path":     "big.txt",
			"contents": strings.Repeat("a", maxWriteBytes+1),
		})
		if err == nil {
			t.Fatal("expected error for oversized contents, got nil")
		}
		if !strings.Contains(err.Error(), "contents exceed") {
			t.Fatalf("error %q does not contain 'contents exceed'", err.Error())
		}

		if _, statErr := os.Stat(filepath.Join(root, "big.txt")); !os.IsNotExist(statErr) {
			t.Fatalf("file exists: %v", statErr)
		}
	})

	t.Run("validates input arguments", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name        string
			args        map[string]any
			wantErrPart string
		}{
			{
				name:        "missing path",
				args:        map[string]any{"contents": "hello"},
				wantErrPart: "path is required",
			},
			{
				name:        "empty path",
				args:        map[string]any{"path": "", "contents": "hello"},
				wantErrPart: "path is required",
			},
			{
				name:        "whitespace path",
				args:        map[string]any{"path": "   ", "contents": "hello"},
				wantErrPart: "path is required",
			},
			{
				name:        "non-string path",
				args:        map[string]any{"path": 123, "contents": "hello"},
				wantErrPart: "expected a string",
			},
			{
				name:        "missing contents",
				args:        map[string]any{"path": "file.txt"},
				wantErrPart: "contents is required",
			},
			{
				name:        "non-string contents",
				args:        map[string]any{"path": "file.txt", "contents": 123},
				wantErrPart: "expected a string",
			},
			{
				name:        "path escaping root",
				args:        map[string]any{"path": "../escape.txt", "contents": "hello"},
				wantErrPart: "outside the working directory",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				set := New(root)

				_, err := set.Run(t.Context(), "create_file", tt.args)
				if err == nil {
					t.Fatalf("create_file(%#v) expected error, got nil", tt.args)
				}
				if !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErrPart)
				}
			})
		}
	})
}

func TestEditFile(t *testing.T) {
	t.Parallel()

	t.Run("replaces snippet and preserves permissions", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		full := filepath.Join(root, "note.txt")
		if err := os.WriteFile(full, []byte("alpha beta alpha\n"), 0o600); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		set := New(root)
		got, err := set.Run(t.Context(), "edit_file", map[string]any{
			"path":       "note.txt",
			"old_string": "beta",
			"new_string": "gamma",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "edited note.txt" {
			t.Fatalf("result = %q, want %q", got, "edited note.txt")
		}

		body, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		const wantBody = "alpha gamma alpha\n"
		if string(body) != wantBody {
			t.Fatalf("contents = %q, want %q", string(body), wantBody)
		}

		info, err := os.Stat(full)
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("mode = %04o, want 0600", perm)
		}
	})

	t.Run("deletes snippet when new_string is empty", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		full := filepath.Join(root, "note.txt")
		if err := os.WriteFile(full, []byte("alpha beta alpha\n"), 0o644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		set := New(root)
		got, err := set.Run(t.Context(), "edit_file", map[string]any{
			"path":       "note.txt",
			"old_string": "beta",
			"new_string": "",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "edited note.txt" {
			t.Fatalf("result = %q, want %q", got, "edited note.txt")
		}

		body, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		const wantBody = "alpha  alpha\n"
		if string(body) != wantBody {
			t.Fatalf("contents = %q, want %q", string(body), wantBody)
		}
	})

	t.Run("follows symlink inside root", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		target := filepath.Join(root, "target.txt")
		if err := os.WriteFile(target, []byte("alpha beta\n"), 0o644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		linkPath := filepath.Join(root, "link")
		if err := os.Symlink(target, linkPath); err != nil {
			t.Fatalf("Symlink failed: %v", err)
		}

		set := New(root)
		if _, err := set.Run(t.Context(), "edit_file", map[string]any{
			"path":       "link",
			"old_string": "beta",
			"new_string": "gamma",
		}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		body, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		const wantBody = "alpha gamma\n"
		if string(body) != wantBody {
			t.Fatalf("target contents = %q, want %q", string(body), wantBody)
		}

		info, err := os.Lstat(linkPath)
		if err != nil {
			t.Fatalf("Lstat failed: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("symlink was replaced with regular file")
		}
	})

	t.Run("rejects symlink escape", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		outside := t.TempDir()
		secret := filepath.Join(outside, "secret")
		if err := os.WriteFile(secret, []byte("original"), 0o644); err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}
		if err := os.Symlink(secret, filepath.Join(root, "link")); err != nil {
			t.Fatalf("Symlink failed: %v", err)
		}

		set := New(root)
		_, err := set.Run(t.Context(), "edit_file", map[string]any{
			"path":       "link",
			"old_string": "original",
			"new_string": "pwned",
		})
		if err == nil {
			t.Fatal("expected error for symlink escape, got nil")
		}

		body, err := os.ReadFile(secret)
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(body) != "original" {
			t.Fatalf("outside file changed to %q", body)
		}
	})

	t.Run("rejects invalid arguments and conditions", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name        string
			setup       func(t *testing.T, root string)
			args        map[string]any
			wantErrPart string
		}{
			{
				name:        "missing path",
				args:        map[string]any{"old_string": "a", "new_string": "b"},
				wantErrPart: "path is required",
			},
			{
				name:        "empty path",
				args:        map[string]any{"path": "", "old_string": "a", "new_string": "b"},
				wantErrPart: "path is required",
			},
			{
				name:        "whitespace path",
				args:        map[string]any{"path": "   ", "old_string": "a", "new_string": "b"},
				wantErrPart: "path is required",
			},
			{
				name:        "non-string path",
				args:        map[string]any{"path": 123, "old_string": "a", "new_string": "b"},
				wantErrPart: "expected a string",
			},
			{
				name:        "missing old_string",
				args:        map[string]any{"path": "note.txt", "new_string": "b"},
				wantErrPart: "old_string is required",
			},
			{
				name:        "empty old_string",
				args:        map[string]any{"path": "note.txt", "old_string": "", "new_string": "b"},
				wantErrPart: "old_string is required",
			},
			{
				name:        "non-string old_string",
				args:        map[string]any{"path": "note.txt", "old_string": 123, "new_string": "b"},
				wantErrPart: "expected a string",
			},
			{
				name:        "missing new_string",
				args:        map[string]any{"path": "note.txt", "old_string": "a"},
				wantErrPart: "new_string is required",
			},
			{
				name:        "non-string new_string",
				args:        map[string]any{"path": "note.txt", "old_string": "a", "new_string": 123},
				wantErrPart: "expected a string",
			},
			{
				name:        "file does not exist",
				args:        map[string]any{"path": "missing.txt", "old_string": "a", "new_string": "b"},
				wantErrPart: "missing.txt",
			},
			{
				name: "target path is a directory",
				setup: func(t *testing.T, root string) {
					if err := os.Mkdir(filepath.Join(root, "subdir"), 0o755); err != nil {
						t.Fatal(err)
					}
				},
				args:        map[string]any{"path": "subdir", "old_string": "a", "new_string": "b"},
				wantErrPart: "is a directory",
			},
			{
				name: "old_string not found",
				setup: func(t *testing.T, root string) {
					if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("alpha beta alpha\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				},
				args:        map[string]any{"path": "note.txt", "old_string": "nope", "new_string": "x"},
				wantErrPart: "matched 0 times",
			},
			{
				name: "old_string matches multiple times",
				setup: func(t *testing.T, root string) {
					if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("alpha beta alpha\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				},
				args:        map[string]any{"path": "note.txt", "old_string": "alpha", "new_string": "x"},
				wantErrPart: "matched 2 times",
			},
			{
				name: "file exceeds max write bytes",
				setup: func(t *testing.T, root string) {
					big := strings.Repeat("x", maxWriteBytes+1)
					if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte(big), 0o644); err != nil {
						t.Fatal(err)
					}
				},
				args:        map[string]any{"path": "note.txt", "old_string": "x", "new_string": "y"},
				wantErrPart: "file exceeds",
			},
			{
				name: "replacement exceeds max write bytes",
				setup: func(t *testing.T, root string) {
					if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("small"), 0o644); err != nil {
						t.Fatal(err)
					}
				},
				args:        map[string]any{"path": "note.txt", "old_string": "small", "new_string": strings.Repeat("x", maxWriteBytes+1)},
				wantErrPart: "contents exceed",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				if tt.setup != nil {
					tt.setup(t, root)
				}

				notePath := filepath.Join(root, "note.txt")
				var original []byte
				if data, err := os.ReadFile(notePath); err == nil {
					original = data
				}

				set := New(root)
				_, err := set.Run(t.Context(), "edit_file", tt.args)
				if err == nil {
					t.Fatalf("edit_file(%#v) expected error, got nil", tt.args)
				}
				if !strings.Contains(err.Error(), tt.wantErrPart) {
					t.Fatalf("error %q does not contain %q", err.Error(), tt.wantErrPart)
				}

				if original != nil {
					after, err := os.ReadFile(notePath)
					if err != nil {
						t.Fatalf("ReadFile failed: %v", err)
					}
					if string(after) != string(original) {
						t.Fatalf("file contents modified on failed edit: got %q, want %q", string(after), string(original))
					}
				}
			})
		}
	})
}
