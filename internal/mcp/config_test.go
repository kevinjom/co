package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	home := t.TempDir()
	servers, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 {
		t.Fatalf("servers %#v", servers)
	}
	if _, err := os.Stat(HomePath(home)); !os.IsNotExist(err) {
		t.Fatalf("mcp.yaml created: %v", err)
	}
}

func TestLoad(t *testing.T) {
	home := t.TempDir()
	path := HomePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := `
beta:
  url: https://beta.example/mcp
  headers:
    Authorization: Bearer ${BETA_TOKEN}
alpha:
  url: http://alpha.example/mcp
  headers:
    X-Trace: keep me
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	servers, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 || servers[0].Name != "beta" || servers[1].Name != "alpha" {
		t.Fatalf("servers %#v", servers)
	}
	if servers[0].Headers["Authorization"] != "Bearer ${BETA_TOKEN}" {
		t.Fatalf("header %q", servers[0].Headers["Authorization"])
	}
	if servers[1].URL != "http://alpha.example/mcp" || servers[1].Headers["X-Trace"] != "keep me" {
		t.Fatalf("alpha %#v", servers[1])
	}
}

func TestLoadRejects(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "broken", body: ":\n"},
		{name: "list", body: "- github\n"},
		{name: "name", body: "git hub:\n  url: https://example.com/mcp\n"},
		{name: "duplicate", body: "github:\n  url: https://example.com/mcp\ngithub:\n  url: https://example.com/other\n"},
		{name: "no url", body: "github:\n  headers:\n    A: b\n"},
		{name: "blank url", body: "github:\n  url: \" \"\n"},
		{name: "scheme", body: "github:\n  url: ftp://example.com/mcp\n"},
		{name: "unknown", body: "github:\n  url: https://example.com/mcp\n  command: npx\n"},
		{name: "header", body: "github:\n  url: https://example.com/mcp\n  headers:\n    A:\n      nested: 1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			path := HomePath(home)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(tt.body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(home); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestLoadEmpty(t *testing.T) {
	home := t.TempDir()
	path := HomePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	servers, err := Load(home)
	if err != nil || len(servers) != 0 {
		t.Fatalf("servers %#v err %v", servers, err)
	}
}
