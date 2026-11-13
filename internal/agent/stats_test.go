package agent

import (
	"context"
	"testing"

	"github.com/kevinjom/co/internal/llm"
)

func TestTurnStoresProviderUsage(t *testing.T) {
	chat := &usageChat{usage: &llm.Usage{Input: 9, Output: 3}}
	session := NewSession(chat, "m", t.TempDir(), nil)
	if _, err := session.Turn(t.Context(), "hi", nil); err != nil {
		t.Fatal(err)
	}
	if chat.calls != 1 {
		t.Fatalf("model calls %d", chat.calls)
	}
	const want = "user turns: 1\nassistant answers: 1\ntool calls: 0\ntokens: 9 in, 3 out\ncontext: 9 in"
	if got := session.Stats().Text(); got != want {
		t.Fatalf("stats\n%s", got)
	}
}

func TestStatsCountsTheWholeSession(t *testing.T) {
	session := NewSession(&usageChat{}, "m", t.TempDir(), nil)
	session.Restore([]llm.Message{
		{Role: "user", Content: "look"},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:       "c1",
				Function: llm.FunctionCall{Name: "list_dir"},
			}},
			Usage: &llm.Usage{Input: 20, Output: 5},
		},
		{Role: "tool", ToolCallID: "c1", Content: "files"},
		{Role: "assistant", Content: "done", Usage: &llm.Usage{Input: 30, Output: 2}},
		{Role: "user", Content: "more"},
		{Role: "assistant", Content: "ok"},
	})

	const want = "user turns: 2\nassistant answers: 2\ntool calls: 1\ntokens: 50 in, 7 out (incomplete)\ncontext: unknown"
	if got := session.Stats().Text(); got != want {
		t.Fatalf("stats\n%s", got)
	}
}

func TestStatsWithoutUsage(t *testing.T) {
	session := NewSession(&usageChat{}, "m", t.TempDir(), nil)
	if got := session.Stats().Text(); got != "user turns: 0\nassistant answers: 0\ntool calls: 0\ntokens: unknown\ncontext: unknown" {
		t.Fatalf("empty\n%s", got)
	}
	session.Restore([]llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	})
	const want = "user turns: 1\nassistant answers: 1\ntool calls: 0\ntokens: unknown\ncontext: unknown"
	if got := session.Stats().Text(); got != want {
		t.Fatalf("stats\n%s", got)
	}
}

type usageChat struct {
	usage *llm.Usage
	calls int
}

func (c *usageChat) ChatStream(
	_ context.Context,
	_ string,
	_ []llm.Message,
	_ []llm.Tool,
	onChunk func(llm.StreamKind, string),
) (llm.Message, error) {
	c.calls++
	if onChunk != nil {
		onChunk(llm.StreamContent, "ok")
	}
	return llm.Message{Role: "assistant", Content: "ok", Usage: c.usage}, nil
}
