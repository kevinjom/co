package agent

import (
	"context"
	"strings"
	"testing"

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
	over := "over " + session.tools.Search() + " for definitions, references, and diagnostics."
	if after == before || !strings.Contains(after, "lsp_definition") || !strings.Contains(after, "lsp_symbols or lsp_diagnostics") || !strings.Contains(after, over) {
		t.Fatalf("prompt %q", after)
	}
}
