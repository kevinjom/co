package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStubDefinitionHoverAndSync(t *testing.T) {
	root, hub := testHub(t, []serverSpec{stubSpec(t, "lspstub", nil, map[string]string{".go": "go"})})
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := hub.Definition(t.Context(), file, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "locations: 1\nmain.go:1:1" {
		t.Fatalf("definition %q", got)
	}

	hover, err := hub.Hover(t.Context(), file, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if hover != "func Example()" {
		t.Fatalf("hover %q", hover)
	}

	refs, err := hub.References(t.Context(), file, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(refs, "main.go:1:1") || !strings.Contains(refs, "outside the working directory") {
		t.Fatalf("references %q", refs)
	}
	if strings.Contains(refs, "alpha") {
		t.Fatalf("references included file text %q", refs)
	}

	if err := os.WriteFile(file, []byte("beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = hub.Definition(t.Context(), file, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "locations: 1\nmain.go:1:5" {
		t.Fatalf("definition after change %q", got)
	}
}

func TestStubOutsideLocationOmitsBody(t *testing.T) {
	root, hub := testHub(t, []serverSpec{stubSpec(t, "lspstub", nil, map[string]string{".go": "go"})})
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("OUTSIDE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := hub.Definition(t.Context(), file, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "/outside.go:4:2 outside the working directory") {
		t.Fatalf("definition %q", got)
	}
	if strings.Contains(got, "OUTSIDE") {
		t.Fatalf("definition included file text %q", got)
	}
}

func TestStubSymbolsAndDiagnostics(t *testing.T) {
	root, hub := testHub(t, []serverSpec{stubSpec(t, "lspstub", nil, map[string]string{".go": "go"})})
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := hub.Symbols(t.Context(), file, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "symbols: 1\nfunction Example main.go:1:1" {
		t.Fatalf("symbols %q", got)
	}

	got, err = hub.Diagnostics(t.Context(), file)
	if err != nil {
		t.Fatal(err)
	}
	if got != "diagnostics: 1\nmain.go:3:1 error undefined: x" {
		t.Fatalf("diagnostics %q", got)
	}

	got, err = hub.Diagnostics(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "lspstub ") || !strings.Contains(got, "warning unused") || !strings.Contains(got, "outside the working directory") {
		t.Fatalf("workspace diagnostics %q", got)
	}
}

func TestUnknownExtensionDoesNotStartServer(t *testing.T) {
	root, hub := testHub(t, []serverSpec{stubSpec(t, "lspstub", nil, map[string]string{".go": "go"})})
	file := filepath.Join(root, "note.txt")
	if err := os.WriteFile(file, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := hub.Definition(t.Context(), file, 1, 1)
	if err == nil || err.Error() != "no language server for .txt" {
		t.Fatalf("err %v", err)
	}
}

func TestCancelLeavesServerUp(t *testing.T) {
	root, hub := testHub(t, []serverSpec{stubSpec(t, "lspstub", nil, map[string]string{".go": "go"})})
	mainFile := filepath.Join(root, "main.go")
	block := filepath.Join(root, "block.go")
	if err := os.WriteFile(mainFile, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(block, []byte("BLOCK\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Definition(t.Context(), mainFile, 1, 1); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err := hub.Hover(ctx, block, 1, 1)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hover err %v", err)
	}

	got, err := hub.Definition(t.Context(), mainFile, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "main.go:1:1") {
		t.Fatalf("definition after cancel %q", got)
	}
}

func TestBareServerHasNoPullDiagnostics(t *testing.T) {
	root, hub := testHub(t, []serverSpec{
		stubSpec(t, "lspstub", []string{"lsp-stub", "bare"}, map[string]string{".go": "go"}),
	})
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := hub.Diagnostics(t.Context(), file)
	if err == nil || !strings.Contains(err.Error(), "no pull diagnostics") {
		t.Fatalf("err %v", err)
	}
}

func TestWorkspaceSymbolsMergeServers(t *testing.T) {
	files := map[string]string{".go": "go"}
	root, hub := testHub(t, []serverSpec{
		stubSpec(t, "srv-a", nil, files),
		stubSpec(t, "srv-b", nil, files),
	})
	_ = root
	got, err := hub.Symbols(t.Context(), "", "Example")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "srv-a ") || !strings.Contains(got, "srv-b ") {
		t.Fatalf("symbols %q", got)
	}
	if !strings.Contains(got, "symbols: 2") || !strings.Contains(got, "outside the working directory") {
		t.Fatalf("symbols %q", got)
	}
}

func testHub(t *testing.T, specs []serverSpec) (string, *Hub) {
	t.Helper()
	root := t.TempDir()
	hub := newHub(root, specs)
	t.Cleanup(func() {
		if err := hub.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return hub.root, hub
}

func stubSpec(t *testing.T, name string, args []string, files map[string]string) serverSpec {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.Symlink(bin, path); err != nil {
		t.Fatal(err)
	}
	if args == nil {
		args = []string{"lsp-stub"}
	}
	return serverSpec{
		Command: path,
		Args:    args,
		Files:   files,
	}
}
