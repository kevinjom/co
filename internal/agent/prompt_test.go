package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/kevinjom/co/internal/llm"
	"github.com/kevinjom/co/internal/llm/openai"
)

func TestSystemPromptNamesOneSearch(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	tests := []struct {
		name    string
		search  string
		lsp     bool
		want    string
		over    string
		missing []string
	}{
		{
			name:    "rg",
			search:  "rg",
			want:    "Search file contents with rg.",
			missing: []string{"Search file contents with ag.", "grep"},
		},
		{
			name:    "ag",
			search:  "ag",
			want:    "Search file contents with ag.",
			missing: []string{"Search file contents with rg.", "grep"},
		},
		{
			name:    "grep tool",
			search:  "grep",
			want:    "Search file contents with the grep tool.",
			missing: []string{"Search file contents with rg.", "Search file contents with ag."},
		},
		{
			name:    "rg with language tools",
			search:  "rg",
			lsp:     true,
			want:    "Search file contents with rg.",
			over:    "over rg for definitions, references, and diagnostics.",
			missing: []string{"Search file contents with ag.", "grep"},
		},
		{
			name:    "ag with language tools",
			search:  "ag",
			lsp:     true,
			want:    "Search file contents with ag.",
			over:    "over ag for definitions, references, and diagnostics.",
			missing: []string{"Search file contents with rg.", "grep"},
		},
		{
			name:    "grep tool with language tools",
			search:  "grep",
			lsp:     true,
			want:    "Search file contents with the grep tool.",
			over:    "over grep for definitions, references, and diagnostics.",
			missing: []string{"Search file contents with rg.", "Search file contents with ag."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := systemPrompt(cwd, tt.lsp, nil, tt.search, false)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("prompt missing %q\n%s", tt.want, got)
			}
			if tt.over != "" && !strings.Contains(got, tt.over) {
				t.Fatalf("prompt missing %q\n%s", tt.over, got)
			}
			for _, phrase := range tt.missing {
				if strings.Contains(got, phrase) {
					t.Fatalf("prompt contains %q\n%s", phrase, got)
				}
			}
		})
	}
}

func TestNewSessionPromptUsesInstalledSearch(t *testing.T) {
	t.Parallel()

	session := NewSession(openai.New("http://127.0.0.1:1"), "m", t.TempDir(), nil)
	search := session.tools.Search()
	if offersGrep(session.defs) != (search == "grep") {
		t.Fatalf("grep offered=%t search=%s", offersGrep(session.defs), search)
	}
	instruction := searchInstruction(search)
	prompt := session.Messages()[0].Content
	if !strings.Contains(prompt, instruction) {
		t.Fatalf("prompt %q", prompt)
	}
	session.EnableLSP(idleLanguage{})
	after := session.Messages()[0].Content
	want := "over " + search + " for definitions, references, and diagnostics."
	if !strings.Contains(after, want) || !strings.Contains(after, instruction) {
		t.Fatalf("prompt %q", after)
	}
	if offersGrep(session.defs) != (search == "grep") {
		t.Fatalf("grep offered=%t search=%s", offersGrep(session.defs), search)
	}
}

func TestEnableMCPMentionsTools(t *testing.T) {
	t.Parallel()

	session := NewSession(openai.New("http://127.0.0.1:1"), "m", t.TempDir(), nil)
	before := session.Messages()[0].Content
	session.EnableMCP(idleMCP{})
	after := session.Messages()[0].Content
	if strings.Contains(before, "mcp_search") {
		t.Fatalf("prompt before enable %q", before)
	}
	if !strings.Contains(after, "mcp_search") || !strings.Contains(after, "mcp_call") {
		t.Fatalf("prompt %q", after)
	}
	found := map[string]bool{}
	for _, def := range session.defs {
		found[def.Function.Name] = true
	}
	if !found["mcp_search"] || !found["mcp_call"] {
		t.Fatalf("tools %#v", session.defs)
	}
}

type idleMCP struct{}

func (idleMCP) Search(context.Context, string) (string, error) { return "", nil }
func (idleMCP) Call(context.Context, string, string, map[string]any) (string, error) {
	return "", nil
}

func offersGrep(defs []llm.Tool) bool {
	for _, def := range defs {
		if def.Function.Name == "grep" {
			return true
		}
	}
	return false
}
