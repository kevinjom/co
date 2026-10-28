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
	"co/internal/openai"
)

func newTestModel(t *testing.T) *model {
	t.Helper()
	dir := t.TempDir()
	session := agent.NewSession(openai.New("http://127.0.0.1:1"), "test-model", dir, nil)
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
	if !m.quitting || cmd == nil || !m.explainResume {
		t.Fatal("expected quit that explains how to resume")
	}
	if m.View().Content != "" {
		t.Fatal("quit view should be empty")
	}
}

func TestCtrlCQuitsWhenIdle(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(press('c', tea.ModCtrl))
	if !m.quitting || cmd == nil || m.explainResume {
		t.Fatal("expected quit without a resume hint")
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
	toolAt := strings.Index(plain, "List")
	answerAt := strings.Index(plain, "two files")
	if toolAt < 0 || answerAt < 0 || toolAt > answerAt || !strings.Contains(plain, "path=.") {
		t.Fatalf("transcript %q", plain)
	}
}

func TestReasoningStreamsBeforeTheAnswer(t *testing.T) {
	m := newTestModel(t)
	m.turnID = 1
	m.busy = true
	m.events = make(chan tea.Msg)
	m.Update(eventMsg{id: 1, event: agent.ReasoningDelta{Text: "Think"}})
	m.Update(eventMsg{id: 1, event: agent.ReasoningDelta{Text: "ing"}})
	m.Update(eventMsg{id: 1, event: agent.AnswerDelta{Text: "Hi"}})
	if len(m.blocks) != 2 || m.blocks[0].kind != blockReasoning || m.blocks[0].text != "Thinking" {
		t.Fatalf("reasoning %+v", m.blocks)
	}
	if m.blocks[1].kind != blockAssistant || m.blocks[1].text != "Hi" {
		t.Fatalf("answer %+v", m.blocks[1])
	}
	plain := ansi.Strip(m.renderTranscript())
	if !strings.Contains(plain, "Thinking") || !strings.Contains(plain, "Hi") {
		t.Fatalf("transcript %q", plain)
	}
}

func TestReasoningOnlyAnswerIsNotDuplicated(t *testing.T) {
	m := newTestModel(t)
	m.turnID = 1
	m.busy = true
	m.events = make(chan tea.Msg)
	m.Update(eventMsg{id: 1, event: agent.ReasoningDelta{Text: "the answer"}})
	updated, _ := m.Update(turnDoneMsg{id: 1, answer: "the answer"})
	got := updated.(*model)
	if len(got.blocks) != 1 || got.blocks[0].kind != blockReasoning || got.blocks[0].text != "the answer" {
		t.Fatalf("blocks %+v", got.blocks)
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
	if len(m.blocks) != 1 || !m.blocks[0].failed || !m.blocks[0].done {
		t.Fatalf("blocks %+v", m.blocks)
	}
	plain := ansi.Strip(m.renderTranscript())
	if !strings.Contains(plain, "Read") || !strings.Contains(plain, "path=missing") {
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

func TestAssistantMarkdownRenders(t *testing.T) {
	m := newTestModel(t)
	m.blocks = []block{{
		kind: blockAssistant,
		text: "# Title\n\nRun `go test` and **bold**.\n\n```go\nfunc main() {}\n```",
	}}
	raw := m.renderTranscript()
	plain := ansi.Strip(raw)
	for _, gone := range []string{"# Title", "**bold**", "`go test`", "```"} {
		if strings.Contains(plain, gone) {
			t.Fatalf("raw markdown %q left in %q", gone, plain)
		}
	}
	for _, want := range []string{"Title", "go test", "bold", "func main() {}"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %q", want, plain)
		}
	}
	if raw == plain {
		t.Fatal("markdown rendered without color")
	}
	if w := lipgloss.Width(raw); w > 80 {
		t.Fatalf("width %d", w)
	}
}

func TestTranscriptKindsStayDistinct(t *testing.T) {
	m := newTestModel(t)
	m.blocks = []block{
		{kind: blockUser, text: "how many lines?"},
		{kind: blockReasoning, text: "Count the files."},
		{kind: blockTool, tool: "list_dir", detail: "path=."},
		{kind: blockTool, tool: "read_file", detail: "path=missing", failed: true, done: true},
		{kind: blockError, text: "something failed"},
		{kind: blockNote, text: "interrupted"},
	}
	plain := ansi.Strip(m.renderTranscript())
	for _, want := range []string{"❯ how many lines?", "thinking", "Count the files.", "List", "path=.", "Read", "path=missing", "something failed", "interrupted"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %q", want, plain)
		}
	}
	raw := m.renderTranscript()
	if lipgloss.Width(raw) > 80 {
		t.Fatalf("width %d\n%s", lipgloss.Width(raw), plain)
	}
	if raw == ansi.Strip(raw) {
		t.Fatal("transcript has no color")
	}
}

func TestWrapKeepsExistingLines(t *testing.T) {
	got := wrapText("one\n\ntwo", 80)
	if got != "one\n\ntwo" {
		t.Fatalf("%q", got)
	}
}

type fakeDirector struct {
	models  []string
	picked  string
	session *agent.Session
	ready   bool
}

func (f *fakeDirector) Models(context.Context) ([]string, error) { return f.models, nil }
func (f *fakeDirector) Select(_ context.Context, ref string) error {
	f.picked = ref
	return nil
}
func (f *fakeDirector) CanConnect(string) error      { return nil }
func (f *fakeDirector) Connect(string, string) error { return nil }
func (f *fakeDirector) Model() string                { return f.picked }
func (f *fakeDirector) Session() *agent.Session      { return f.session }
func (f *fakeDirector) Ready() bool                  { return f.ready }
func (f *fakeDirector) ExitNote() string             { return "" }

func TestModelsCommandDoesNotStartATurn(t *testing.T) {
	m := newTestModel(t)
	director := &fakeDirector{
		models:  []string{"ollama/a", "ollama/b"},
		session: m.session,
		ready:   true,
	}
	m.director = director
	m.input.SetValue("/models")
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if got.busy || got.mode != modeModels || cmd == nil {
		t.Fatalf("busy=%v mode=%d cmd=%v", got.busy, got.mode, cmd != nil)
	}
	loaded := cmd().(modelsLoadedMsg)
	updated, _ = got.Update(loaded)
	got = updated.(*model)
	plain := ansi.Strip(got.View().Content)
	if !strings.Contains(plain, "ollama/a") || !strings.Contains(plain, "ollama/b") {
		t.Fatalf("picker %q", plain)
	}

	updated, cmd = got.Update(press(tea.KeyEnter, 0))
	got = updated.(*model)
	if cmd == nil || got.busy {
		t.Fatalf("busy=%v cmd=%v", got.busy, cmd != nil)
	}
	done := cmd().(selectDoneMsg)
	updated, _ = got.Update(done)
	got = updated.(*model)
	if director.picked != "ollama/a" || got.mode != modeChat || got.modelName != "ollama/a" {
		t.Fatalf("picked %q mode %d name %q", director.picked, got.mode, got.modelName)
	}
	if got.busy || strings.Contains(ansi.Strip(got.renderTranscript()), "❯ /models") {
		t.Fatal("command was sent as a chat turn")
	}
}

func TestUnknownCommandIsNotATurn(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/nope")
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if got.busy || cmd != nil {
		t.Fatalf("busy=%v cmd=%v", got.busy, cmd != nil)
	}
	plain := ansi.Strip(got.renderTranscript())
	if !strings.Contains(plain, "unknown command") {
		t.Fatalf("transcript %q", plain)
	}
}

func TestStartupEscQuits(t *testing.T) {
	m := newTestModel(t)
	m.session = nil
	m.mode = modeModels
	m.startup = true
	updated, cmd := m.Update(press(tea.KeyEsc, 0))
	got := updated.(*model)
	if !got.quitting || cmd == nil {
		t.Fatal("expected quit")
	}
}

func TestPasteFillsConnectKey(t *testing.T) {
	m := newTestModel(t)
	m.director = &fakeDirector{session: m.session, ready: true}
	m.mode = modeConnect
	m.connectID = "gemini"
	updated, _ := m.Update(tea.PasteMsg{Content: "sk-test-key\n"})
	got := updated.(*model)
	if got.secret != "sk-test-key" {
		t.Fatalf("secret %q", got.secret)
	}
	plain := ansi.Strip(got.View().Content)
	if strings.Contains(plain, "sk-test-key") {
		t.Fatal("pasted key is visible")
	}
	if !strings.Contains(plain, strings.Repeat("•", len("sk-test-key"))) {
		t.Fatalf("hidden key missing: %q", plain)
	}
}

func TestResumeRendersPriorTurns(t *testing.T) {
	dir := t.TempDir()
	session := agent.NewSession(openai.New("http://127.0.0.1:1"), "test-model", dir, nil)
	session.Restore([]openai.Message{
		{Role: "user", Content: "how many lines?"},
		{Role: "assistant", ToolCalls: []openai.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: openai.FunctionCall{
				Name:      "list_dir",
				Arguments: openai.Arguments{"path": "."},
			},
		}}},
		{Role: "tool", Content: "a.go", ToolCallID: "call-1"},
		{Role: "assistant", Content: "There are 3 lines."},
	})
	m := newModel(context.Background(), session, "test-model", dir)
	m.width = 80
	m.height = 24
	m.layout()
	m.paint()
	plain := ansi.Strip(m.renderTranscript())
	for _, want := range []string{"❯ how many lines?", "List", "path=.", "There are 3 lines."} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %q", want, plain)
		}
	}
	if strings.Contains(plain, "Ask about this directory") {
		t.Fatal("welcome replaced the resumed transcript")
	}
}

func TestEscClosesPicker(t *testing.T) {
	m := newTestModel(t)
	m.mode = modeModels
	updated, cmd := m.Update(press(tea.KeyEsc, 0))
	got := updated.(*model)
	if got.quitting || got.mode != modeChat || cmd == nil {
		t.Fatalf("quitting=%v mode=%d", got.quitting, got.mode)
	}
}
