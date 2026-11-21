package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevinjom/co/internal/agent"
	"github.com/kevinjom/co/internal/llm"
	"github.com/kevinjom/co/internal/tools"
)

func TestAskSelectsOptionsAndRestoresTheDraft(t *testing.T) {
	m := newTestModel(t)
	m.tab().busy = true
	m.tab().turnID = 3
	m.input.SetValue("later")
	pipe := newTurnPipe()
	m.tab().pipe = pipe
	pipe.push(askMsg{
		tabID: m.tab().id,
		id:    3,
		question: tools.Question{
			Text:     "Which database?",
			Options:  []string{"postgres", "sqlite", "mysql"},
			Multiple: true,
		},
		reply: make(chan askResult, 1),
	})
	reply := pipe.pending[0].(askMsg).reply

	updated, _ := m.Update(pipeMsg{pipe: pipe})
	got := updated.(*model)
	plain := ansi.Strip(got.View().Content)
	if !strings.Contains(plain, "Which database?") || !strings.Contains(plain, "postgres") || !strings.Contains(plain, "space select") {
		t.Fatalf("view %q", plain)
	}
	if got.input.Value() != "" || got.input.Placeholder != answerPlaceholder {
		t.Fatalf("value %q placeholder %q", got.input.Value(), got.input.Placeholder)
	}

	updated, _ = got.Update(press(' ', 0))
	got = updated.(*model)
	updated, _ = got.Update(press(tea.KeyDown, 0))
	got = updated.(*model)
	updated, _ = got.Update(press(' ', 0))
	got = updated.(*model)
	if !got.tab().ask.picked[0] || !got.tab().ask.picked[1] || got.tab().ask.picked[2] || got.tab().ask.cursor != 1 {
		t.Fatalf("picked %#v cursor %d", got.tab().ask.picked, got.tab().ask.cursor)
	}
	got.input.SetValue("also redis")
	updated, cmd := got.Update(press(tea.KeyEnter, 0))
	got = updated.(*model)
	if cmd != nil || got.tab().ask != nil || got.input.Value() != "later" || len(got.tab().queue) != 0 {
		t.Fatalf("ask %v value %q queue %#v cmd %v", got.tab().ask != nil, got.input.Value(), got.tab().queue, cmd != nil)
	}
	select {
	case result := <-reply:
		if result.err != nil || result.text != "postgres\nsqlite\n\nalso redis" {
			t.Fatalf("result %+v", result)
		}
	default:
		t.Fatal("no reply")
	}
}

func TestAskEmptyEnterStaysOpen(t *testing.T) {
	m := openTestAsk(t, tools.Question{Text: "What name?", Options: []string{"a"}})
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if cmd != nil || got.tab().ask == nil || len(got.tab().queue) != 0 {
		t.Fatalf("ask %v queue %#v", got.tab().ask != nil, got.tab().queue)
	}
}

func TestAskSingleSelectKeepsOneOption(t *testing.T) {
	m := openTestAsk(t, tools.Question{Text: "Which database?", Options: []string{"postgres", "sqlite"}})
	reply := m.tab().ask.reply
	updated, _ := m.Update(press(' ', 0))
	got := updated.(*model)
	updated, _ = got.Update(press(tea.KeyDown, 0))
	got = updated.(*model)
	updated, _ = got.Update(press(' ', 0))
	got = updated.(*model)
	if got.tab().ask.picked[0] || !got.tab().ask.picked[1] {
		t.Fatalf("picked %#v", got.tab().ask.picked)
	}
	updated, _ = got.Update(press(tea.KeyEnter, 0))
	got = updated.(*model)
	result := <-reply
	if result.text != "sqlite" || got.tab().ask != nil {
		t.Fatalf("result %+v ask %v", result, got.tab().ask != nil)
	}
}

func TestAskCancelRestoresTheDraft(t *testing.T) {
	m := newTestModel(t)
	m.tab().busy = true
	m.tab().turnID = 1
	m.input.SetValue("later")
	canceled := false
	m.tab().cancel = func() { canceled = true }
	pipe := newTurnPipe()
	m.tab().pipe = pipe
	pipe.push(askMsg{
		tabID:    m.tab().id,
		id:       1,
		question: tools.Question{Text: "What name?"},
		reply:    make(chan askResult, 1),
	})
	updated, _ := m.Update(pipeMsg{pipe: pipe})
	got := updated.(*model)
	updated, _ = got.Update(press('c', tea.ModCtrl))
	got = updated.(*model)
	if !canceled || got.tab().ask != nil || got.input.Value() != "" {
		t.Fatalf("canceled %v ask %v value %q", canceled, got.tab().ask != nil, got.input.Value())
	}
	updated, _ = got.Update(turnDoneMsg{tabID: got.tab().id, id: got.tab().turnID, err: context.Canceled})
	got = updated.(*model)
	plain := ansi.Strip(got.View().Content)
	if got.tab().busy || got.input.Value() != "later" || strings.Contains(plain, "What name?") {
		t.Fatalf("busy %v value %q view %q", got.tab().busy, got.input.Value(), plain)
	}
}

func TestTurnAsksInTheTUI(t *testing.T) {
	m := newTestModel(t)
	m.tab().session = agent.NewSession(&askChat{}, "test-model", m.cwd, nil)
	cmd := m.startTurn(m.tab(), []string{"where should this live?"})
	got := m
	deadline := time.Now().Add(2 * time.Second)
	for got.tab().ask == nil {
		if time.Now().After(deadline) {
			t.Fatal("question did not open")
		}
		updated, next := got.Update(awaitMsg(t, cmd))
		got = updated.(*model)
		cmd = next
	}
	plain := ansi.Strip(got.View().Content)
	if !strings.Contains(plain, "Which database?") || !strings.Contains(plain, "Ask") {
		t.Fatalf("view %q", plain)
	}
	updated, keyCmd := got.Update(press(' ', 0))
	got = updated.(*model)
	if keyCmd != nil {
		cmd = keyCmd
	}
	updated, keyCmd = got.Update(press(tea.KeyDown, 0))
	got = updated.(*model)
	if keyCmd != nil {
		cmd = keyCmd
	}
	updated, keyCmd = got.Update(press(' ', 0))
	got = updated.(*model)
	if keyCmd != nil {
		cmd = keyCmd
	}
	updated, keyCmd = got.Update(press(tea.KeyEnter, 0))
	got = updated.(*model)
	if keyCmd != nil {
		cmd = keyCmd
	}

	deadline = time.Now().Add(2 * time.Second)
	for got.tab().busy {
		if time.Now().After(deadline) {
			t.Fatalf("state %v", got.tab().state)
		}
		updated, next := got.Update(awaitMsg(t, cmd))
		got = updated.(*model)
		if next != nil {
			cmd = next
		}
	}
	plain = ansi.Strip(got.renderTranscript())
	if got.tab().state != statusDone || !strings.Contains(plain, "sqlite") || !strings.Contains(plain, "using sqlite") {
		t.Fatalf("state %v transcript %q", got.tab().state, plain)
	}
}

func openTestAsk(t *testing.T, question tools.Question) *model {
	t.Helper()
	m := newTestModel(t)
	m.tab().busy = true
	m.tab().turnID = 1
	pipe := newTurnPipe()
	m.tab().pipe = pipe
	pipe.push(askMsg{
		tabID:    m.tab().id,
		id:       1,
		question: question,
		reply:    make(chan askResult, 1),
	})
	updated, _ := m.Update(pipeMsg{pipe: pipe})
	return updated.(*model)
}

type askChat struct {
	n int
}

func (c *askChat) ChatStream(
	_ context.Context,
	_ string,
	messages []llm.Message,
	_ []llm.Tool,
	onChunk func(llm.StreamKind, string),
) (llm.Message, error) {
	c.n++
	if c.n == 1 {
		return llm.Message{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call_ask",
				Type: "function",
				Function: llm.FunctionCall{
					Name: "ask_user",
					Arguments: llm.Arguments{
						"question": "Which database?",
						"options":  []any{"postgres", "sqlite"},
					},
				},
			}},
		}, nil
	}
	var answer string
	for _, msg := range messages {
		if msg.Role == "tool" {
			answer = msg.Content
		}
	}
	if answer != "sqlite" {
		return llm.Message{}, errAsk(answer)
	}
	if onChunk != nil {
		onChunk(llm.StreamContent, "using sqlite")
	}
	return llm.Message{Role: "assistant", Content: "using sqlite"}, nil
}

type errAsk string

func (e errAsk) Error() string { return "unexpected answer " + string(e) }
