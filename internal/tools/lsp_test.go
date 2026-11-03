package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeLang struct {
	path   string
	line   int
	column int
	query  string
	text   string
	err    error
	called bool
}

func (f *fakeLang) Definition(_ context.Context, path string, line, column int) (string, error) {
	f.called = true
	f.path = path
	f.line = line
	f.column = column
	return f.text, f.err
}

func (f *fakeLang) References(_ context.Context, path string, line, column int) (string, error) {
	f.called = true
	f.path = path
	f.line = line
	f.column = column
	return f.text, f.err
}

func (f *fakeLang) Hover(_ context.Context, path string, line, column int) (string, error) {
	f.called = true
	f.path = path
	return f.text, f.err
}

func (f *fakeLang) Symbols(_ context.Context, path, query string) (string, error) {
	f.called = true
	f.path = path
	f.query = query
	return f.text, f.err
}

func (f *fakeLang) Diagnostics(_ context.Context, path string) (string, error) {
	f.called = true
	f.path = path
	return f.text, f.err
}

func TestLSPToolsStayOffUntilEnabled(t *testing.T) {
	set := New(t.TempDir())
	for _, schema := range set.Schemas() {
		if strings.HasPrefix(schema.Name, "lsp_") {
			t.Fatalf("tool %s registered", schema.Name)
		}
	}
}

func TestLSPDefinitionResolvesInsideRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lang := &fakeLang{text: "locations: 1\nmain.go:1:1"}
	set := New(root)
	set.EnableLSP(lang)

	got, err := set.Run(t.Context(), "lsp_definition", map[string]any{
		"path":   "main.go",
		"line":   float64(4),
		"column": float64(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != lang.text {
		t.Fatalf("got %q", got)
	}
	if !lang.called || lang.line != 4 || lang.column != 2 {
		t.Fatalf("call %+v", lang)
	}
	if lang.path != filepath.Join(root, "main.go") && !sameFile(lang.path, filepath.Join(root, "main.go")) {
		t.Fatalf("path %q", lang.path)
	}
}

func TestLSPRejectsEscapeAndBadArgs(t *testing.T) {
	root := t.TempDir()
	lang := &fakeLang{}
	set := New(root)
	set.EnableLSP(lang)

	_, err := set.Run(t.Context(), "lsp_hover", map[string]any{
		"path":   "../secret.go",
		"line":   1,
		"column": 1,
	})
	if err == nil || lang.called {
		t.Fatalf("err %v called %v", err, lang.called)
	}

	_, err = set.Run(t.Context(), "lsp_symbols", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "set path or query") {
		t.Fatalf("err %v", err)
	}
	_, err = set.Run(t.Context(), "lsp_symbols", map[string]any{"path": "main.go", "query": "Foo"})
	if err == nil || !strings.Contains(err.Error(), "set path or query") {
		t.Fatalf("err %v", err)
	}
}

func TestLSPSymbolsAndDiagnosticsPassThrough(t *testing.T) {
	root := t.TempDir()
	lang := &fakeLang{text: "symbols: 1\nfunction Foo"}
	set := New(root)
	set.EnableLSP(lang)

	got, err := set.Run(t.Context(), "lsp_symbols", map[string]any{"query": "Foo"})
	if err != nil {
		t.Fatal(err)
	}
	if got != lang.text || lang.query != "Foo" || lang.path != "" {
		t.Fatalf("got %q lang %+v", got, lang)
	}

	lang.text = "diagnostics: 0"
	got, err = set.Run(t.Context(), "lsp_diagnostics", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "diagnostics: 0" || lang.path != "" {
		t.Fatalf("got %q path %q", got, lang.path)
	}
}

func sameFile(a, b string) bool {
	left, err1 := filepath.EvalSymlinks(a)
	right, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && left == right
}
