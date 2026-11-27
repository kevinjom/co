package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevinjom/co/internal/agent"
	"github.com/kevinjom/co/internal/llm"
)

func TestCompactKeepsEarlierTurns(t *testing.T) {
	chat := &summaryChat{text: "The user asked how many files. The assistant said three."}
	dir := t.TempDir()
	session := agent.NewSession(chat, "test-model", dir, nil)
	session.Restore([]llm.Message{
		{Role: "user", Content: "how many files?"},
		{Role: "assistant", Content: "Three."},
	})
	m := newModel(context.Background(), session, "test-model", dir, "")
	m.width = 80
	m.height = 24
	m.layout()
	m.paint()
	m.input.SetValue("/compact")
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	updated, _ = got.Update(awaitMsg(t, cmd))
	got = updated.(*model)
	if got.tab().busy {
		t.Fatal("still compacting")
	}
	plain := ansi.Strip(got.renderTranscript())
	userAt := strings.Index(plain, "how many files?")
	summaryAt := strings.Index(plain, "summary")
	if userAt < 0 || summaryAt < userAt || !strings.Contains(plain, "Three.") || !strings.Contains(plain[summaryAt:], "said") {
		t.Fatalf("transcript %q", plain)
	}
	if strings.Contains(plain, "❯ /compact") {
		t.Fatal("command was sent as a chat turn")
	}
	msgs := got.session().Messages()
	if len(msgs) != 4 || msgs[1].Content != "how many files?" || msgs[3].Role != agent.SummaryRole {
		t.Fatalf("messages %+v", msgs)
	}
	if chat.calls != 1 {
		t.Fatalf("calls %d", chat.calls)
	}
}

func TestCompactNeedsAConversation(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/compact")
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	updated, _ = got.Update(awaitMsg(t, cmd))
	got = updated.(*model)
	plain := ansi.Strip(got.renderTranscript())
	if !strings.Contains(plain, "nothing to compact") {
		t.Fatalf("transcript %q", plain)
	}
	if len(got.session().Messages()) != 1 {
		t.Fatalf("messages %d", len(got.session().Messages()))
	}
}

func TestCompactWaitsForTheTurn(t *testing.T) {
	m := newTestModel(t)
	m.tab().busy = true
	m.input.SetValue("/compact")
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if cmd != nil || !got.tab().busy {
		t.Fatal("compact started during a turn")
	}
	plain := ansi.Strip(got.renderTranscript())
	if !strings.Contains(plain, "wait for the turn to finish") {
		t.Fatalf("transcript %q", plain)
	}
}

func TestCanceledCompactKeepsTheTranscript(t *testing.T) {
	m := newTestModel(t)
	m.tab().turnID = 1
	m.tab().busy = true
	m.tab().blocks = []block{{kind: blockUser, text: "keep"}}
	updated, _ := m.Update(compactDoneMsg{tabID: 1, id: 1, err: context.Canceled})
	got := updated.(*model)
	plain := ansi.Strip(got.renderTranscript())
	if !strings.Contains(plain, "keep") || !strings.Contains(plain, "interrupted") || strings.Contains(plain, "summary") {
		t.Fatalf("transcript %q", plain)
	}
	if got.tab().busy {
		t.Fatal("still busy")
	}
}

func TestResumeRendersTheSummary(t *testing.T) {
	dir := t.TempDir()
	session := agent.NewSession(&summaryChat{}, "test-model", dir, nil)
	session.Restore([]llm.Message{
		{Role: "user", Content: "how many files?"},
		{Role: "assistant", Content: "Three."},
		{Role: agent.SummaryRole, Content: "The user asked how many files."},
	})
	m := newModel(context.Background(), session, "test-model", dir, "")
	m.width = 80
	m.height = 24
	m.layout()
	m.paint()
	plain := ansi.Strip(m.renderTranscript())
	userAt := strings.Index(plain, "how many files?")
	summaryAt := strings.Index(plain, "summary")
	bodyAt := strings.Index(plain, "The user asked how many files.")
	if userAt < 0 || summaryAt < 0 || bodyAt < 0 || userAt > summaryAt || summaryAt > bodyAt || !strings.Contains(plain, "Three.") {
		t.Fatalf("transcript %q", plain)
	}
}

type summaryChat struct {
	text  string
	calls int
}

func (c *summaryChat) ChatStream(
	_ context.Context,
	_ string,
	_ []llm.Message,
	_ []llm.Tool,
	_ func(llm.StreamKind, string),
) (llm.Message, error) {
	c.calls++
	return llm.Message{Role: "assistant", Content: c.text}, nil
}
