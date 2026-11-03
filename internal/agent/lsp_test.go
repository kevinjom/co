package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/kevinjom/co/internal/openai"
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
	session.EnableLSP(idleLanguage{})
	after := session.Messages()[0].Content
	if after == before || !strings.Contains(after, "lsp_definition") {
		t.Fatalf("prompt %q", after)
	}
}

func TestToolDetailForLSP(t *testing.T) {
	definition := ToolDetail(openai.ToolCall{
		Function: openai.FunctionCall{
			Name: "lsp_definition",
			Arguments: openai.Arguments{
				"path":   "a.go",
				"line":   float64(4),
				"column": float64(2),
			},
		},
	})
	if definition != "a.go:4:2" {
		t.Fatalf("definition %q", definition)
	}

	symbols := ToolDetail(openai.ToolCall{
		Function: openai.FunctionCall{
			Name:      "lsp_symbols",
			Arguments: openai.Arguments{"query": "Foo"},
		},
	})
	if symbols != "query=\"Foo\"" {
		t.Fatalf("symbols %q", symbols)
	}

	diagnostics := ToolDetail(openai.ToolCall{
		Function: openai.FunctionCall{
			Name:      "lsp_diagnostics",
			Arguments: openai.Arguments{},
		},
	})
	if diagnostics != "workspace" {
		t.Fatalf("diagnostics %q", diagnostics)
	}
}
