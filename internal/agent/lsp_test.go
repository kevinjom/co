package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/kevinjom/co/internal/llm"
	"github.com/kevinjom/co/internal/llm/openai"
)

type idleLanguage struct{}

func (idleLanguage) Definition(context.Context, string, int, int) (string, error) {
	return "", nil
}

func (idleLanguage) References(context.Context, string, int, int) (string, error) {
	return "", nil
}

func (idleLanguage) Hover(context.Context, string, int, int) (string, error) {
	return "", nil
}

func (idleLanguage) Symbols(context.Context, string, string) (string, error) {
	return "", nil
}

func (idleLanguage) Diagnostics(context.Context, string) (string, error) {
	return "", nil
}

func TestEnableLSPMentionsTools(t *testing.T) {
	session := NewSession(openai.New("http://127.0.0.1:1"), "m", t.TempDir(), nil)
	before := session.Messages()[0].Content
	mentionsRead := strings.Contains(before, "read that file with read_file")
	mentionsScope := strings.Contains(before, "do the work there")
	mentionsConfirm := strings.Contains(before, "Do not read the file again")
	mentionsWrite := strings.Contains(before, "create_file") && strings.Contains(before, "edit_file")
	mentionsStyle := strings.Contains(before, "Write simple and clean code.")
	mentionsBash := strings.Contains(before, "Run a command with bash.") && strings.Contains(before, "bash can write anywhere.")
	mentionsLSP := strings.Contains(before, "lsp_symbols")
	ready := mentionsRead && mentionsScope && mentionsConfirm && mentionsWrite && mentionsStyle && mentionsBash
	if !ready || mentionsLSP {
		t.Fatalf("prompt %q", before)
	}
	session.EnableLSP(idleLanguage{})
	after := session.Messages()[0].Content
	if after == before || !strings.Contains(after, "lsp_definition") || !strings.Contains(after, "lsp_symbols or lsp_diagnostics") {
		t.Fatalf("prompt %q", after)
	}
}

func TestToolDetailForLSP(t *testing.T) {
	definition := ToolDetail(llm.ToolCall{
		Function: llm.FunctionCall{
			Name: "lsp_definition",
			Arguments: llm.Arguments{
				"path":   "a.go",
				"line":   float64(4),
				"column": float64(2),
			},
		},
	})
	if definition != "a.go:4:2" {
		t.Fatalf("definition %q", definition)
	}

	symbols := ToolDetail(llm.ToolCall{
		Function: llm.FunctionCall{
			Name:      "lsp_symbols",
			Arguments: llm.Arguments{"query": "Foo"},
		},
	})
	if symbols != "query=\"Foo\"" {
		t.Fatalf("symbols %q", symbols)
	}

	diagnostics := ToolDetail(llm.ToolCall{
		Function: llm.FunctionCall{
			Name:      "lsp_diagnostics",
			Arguments: llm.Arguments{},
		},
	})
	if diagnostics != "workspace" {
		t.Fatalf("diagnostics %q", diagnostics)
	}
}

func TestToolDetailForWeb(t *testing.T) {
	search := ToolDetail(llm.ToolCall{
		Function: llm.FunctionCall{
			Name:      "web_search",
			Arguments: llm.Arguments{"query": "golang http"},
		},
	})
	if search != `query="golang http"` {
		t.Fatalf("search %q", search)
	}

	fetch := ToolDetail(llm.ToolCall{
		Function: llm.FunctionCall{
			Name:      "web_fetch",
			Arguments: llm.Arguments{"url": "https://go.dev"},
		},
	})
	if fetch != "https://go.dev" {
		t.Fatalf("fetch %q", fetch)
	}

	bash := ToolDetail(llm.ToolCall{
		Function: llm.FunctionCall{
			Name:      "bash",
			Arguments: llm.Arguments{"command": "echo hello\necho again"},
		},
	})
	if bash != "echo hello echo again" {
		t.Fatalf("bash %q", bash)
	}
}
