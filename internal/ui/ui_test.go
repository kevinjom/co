package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"co/internal/agent"
	"co/internal/ollama"
)

func newTestModel(t *testing.T) *model {
	t.Helper()
	dir := t.TempDir()
	session := agent.NewSession(ollama.New("http://127.0.0.1:1"), "test-model", dir, nil)
	m := newModel(context.Background(), session, "test-model", dir)
	m.width = 80
	m.height = 24
	m.layout()
	m.paint()
	return m
}

func press(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

func TestViewIsInline(t *testing.T) {
	m := newTestModel(t)
	view := m.View()
	if view.AltScreen {
		t.Fatal("alt screen enabled")
	}
	plain := ansi.Strip(view.Content)
	if !strings.Contains(plain, "co") || !strings.Contains(plain, "Ask about this directory") {
		t.Fatalf("view %q", plain)
	}
	if !strings.Contains(plain, "enter send") {
		t.Fatalf("help missing: %q", plain)
	}
	if w := lipgloss.Width(view.Content); w > 80 {
		t.Fatalf("width %d", w)
	}
	if h := lipgloss.Height(view.Content); h != 24 {
		t.Fatalf("height %d\n%s", h, plain)
	}
}

func TestSubmitShowsUserAndStaysBusy(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("how many lines?")
	_, cmd := m.Update(press(tea.KeyEnter, 0))
	if cmd == nil {
		t.Fatal("expected a command")
	}
	if !m.busy || m.status != "thinking" {
		t.Fatalf("busy=%v status=%q", m.busy, m.status)
	}
	plain := ansi.Strip(m.renderTranscript())
	if !strings.Contains(plain, "❯ how many lines?") {
		t.Fatalf("transcript %q", plain)
	}
}

func TestEmptyEnterDoesNothing(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(press(tea.KeyEnter, 0))
	if cmd != nil || m.busy || len(m.blocks) != 0 {
		t.Fatalf("busy=%v blocks=%d cmd=%v", m.busy, len(m.blocks), cmd != nil)
	}
}

func TestExitQuits(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("exit")
	_, cmd := m.Update(press(tea.KeyEnter, 0))
	if !m.quitting || cmd == nil {
		t.Fatal("expected quit")
	}
	if m.View().Content != "" {
		t.Fatal("quit view should be empty")
	}
}

func TestCtrlCQuitsWhenIdle(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(press('c', tea.ModCtrl))
	if !m.quitting || cmd == nil {
		t.Fatal("expected quit")
	}
}

func TestCtrlCCancelsBusyTurn(t *testing.T) {
	m := newTestModel(t)
	canceled := false
	m.busy = true
	m.cancel = func() { canceled = true }
	_, cmd := m.Update(press('c', tea.ModCtrl))
	if !canceled || m.quitting || cmd != nil {
		t.Fatalf("canceled=%v quitting=%v", canceled, m.quitting)
	}
}

func TestTurnMessagesAppendInOrder(t *testing.T) {
	m := newTestModel(t)
	m.turnID = 1
	m.busy = true
	m.events = make(chan tea.Msg)
	_, cmd := m.Update(eventMsg{id: 1, event: agent.ToolStart{ID: "call_1", Name: "list_dir", Detail: "path=."}})
	if cmd == nil {
		t.Fatal("expected to keep listening")
	}
	updated, _ := m.Update(eventMsg{id: 1, event: agent.AnswerDelta{Text: "two files"}})
	updated, _ = updated.(*model).Update(turnDoneMsg{id: 1, answer: "two files"})
	got := updated.(*model)
	if got.busy {
		t.Fatal("still busy")
	}
	plain := ansi.Strip(got.renderTranscript())
	toolAt := strings.Index(plain, "list_dir path=.")
	answerAt := strings.Index(plain, "two files")
	if toolAt < 0 || answerAt < 0 || toolAt > answerAt {
		t.Fatalf("transcript %q", plain)
	}
}

func TestAnswerDeltasGrowOneBlock(t *testing.T) {
	m := newTestModel(t)
	m.turnID = 1
	m.busy = true
	m.events = make(chan tea.Msg)
	m.Update(eventMsg{id: 1, event: agent.AnswerDelta{Text: "hel"}})
	m.Update(eventMsg{id: 1, event: agent.AnswerDelta{Text: "lo"}})
	if len(m.blocks) != 1 || m.blocks[0].kind != blockAssistant || m.blocks[0].text != "hello" {
		t.Fatalf("blocks %+v", m.blocks)
	}
	if m.status != "answering" {
		t.Fatalf("status %q", m.status)
	}
}

func TestToolErrorMarksTheLine(t *testing.T) {
	m := newTestModel(t)
	m.turnID = 1
	m.busy = true
	m.events = make(chan tea.Msg)
	m.Update(eventMsg{id: 1, event: agent.ToolStart{ID: "call_1", Name: "read_file", Detail: "path=missing"}})
	m.Update(eventMsg{id: 1, event: agent.ToolDone{ID: "call_1", Err: errors.New("missing")}})
	if len(m.blocks) != 1 || !m.blocks[0].failed {
		t.Fatalf("blocks %+v", m.blocks)
	}
	plain := ansi.Strip(m.renderTranscript())
	if !strings.Contains(plain, "read_file path=missing") {
		t.Fatalf("transcript %q", plain)
	}
}

func TestCtrlCKeepsPartialAnswer(t *testing.T) {
	m := newTestModel(t)
	m.turnID = 1
	m.busy = true
	m.events = make(chan tea.Msg)
	canceled := false
	m.cancel = func() { canceled = true }
	m.Update(eventMsg{id: 1, event: agent.AnswerDelta{Text: "partial answer"}})
	_, cmd := m.Update(press('c', tea.ModCtrl))
	if !canceled || m.quitting || cmd != nil {
		t.Fatalf("canceled=%v quitting=%v cmd=%v", canceled, m.quitting, cmd != nil)
	}
	updated, cmd := m.Update(turnDoneMsg{id: 1, answer: "partial answer", err: context.Canceled})
	got := updated.(*model)
	plain := ansi.Strip(got.renderTranscript())
	if !strings.Contains(plain, "partial answer") || !strings.Contains(plain, "interrupted") {
		t.Fatalf("transcript %q", plain)
	}
	if got.busy || cmd == nil {
		t.Fatal("expected the turn to finish")
	}
}

func TestStaleTurnIsIgnored(t *testing.T) {
	m := newTestModel(t)
	m.turnID = 2
	m.busy = true
	updated, _ := m.Update(turnDoneMsg{id: 1, answer: "stale"})
	got := updated.(*model)
	if !got.busy || len(got.blocks) != 0 {
		t.Fatalf("busy=%v blocks=%d", got.busy, len(got.blocks))
	}
}

func TestCanceledTurnNotesInterruption(t *testing.T) {
	m := newTestModel(t)
	m.turnID = 1
	m.busy = true
	updated, cmd := m.Update(turnDoneMsg{id: 1, err: context.Canceled})
	got := updated.(*model)
	if got.busy || got.quitting || cmd == nil {
		t.Fatalf("busy=%v quitting=%v", got.busy, got.quitting)
	}
	if !strings.Contains(ansi.Strip(got.renderTranscript()), "interrupted") {
		t.Fatalf("transcript %q", ansi.Strip(got.renderTranscript()))
	}
}

func TestParentCancelQuits(t *testing.T) {
	m := newTestModel(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.ctx = ctx
	m.turnID = 1
	m.busy = true
	_, cmd := m.Update(turnDoneMsg{id: 1, err: context.Canceled})
	if !m.quitting || cmd == nil || m.exitErr == nil {
		t.Fatal("expected quit on parent cancel")
	}
}

func TestShortPathUsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := shortPath(home); got != "~" {
		t.Fatalf("home %q", got)
	}
	if got := shortPath(home + "/src"); got != "~/src" {
		t.Fatalf("child %q", got)
	}
}

func TestWrapKeepsExistingLines(t *testing.T) {
	got := wrapText("one\n\ntwo", 80)
	if got != "one\n\ntwo" {
		t.Fatalf("%q", got)
	}
}
