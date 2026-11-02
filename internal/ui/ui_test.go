package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevinjom/co/internal/agent"
	"github.com/kevinjom/co/internal/openai"
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

func TestViewFillsTheTerminal(t *testing.T) {
	m := newTestModel(t)
	view := m.View()
	if !view.AltScreen {
		t.Fatal("alt screen disabled")
	}
	if view.MouseMode != tea.MouseModeCellMotion {
		t.Fatalf("mouse mode %d", view.MouseMode)
	}
	plain := ansi.Strip(view.Content)
	if !strings.Contains(plain, "co") || !strings.Contains(plain, "Ask about this directory") {
		t.Fatalf("view %q", plain)
	}
	if !strings.Contains(plain, "enter send") {
		t.Fatalf("help missing: %q", plain)
	}
	if !strings.Contains(plain, "> Ask about this codebase") {
		t.Fatalf("placeholder missing: %q", plain)
	}
	if strings.Count(plain, "╭") != 1 {
		t.Fatalf("prompt border: %q", plain)
	}
	if w := lipgloss.Width(view.Content); w > 80 {
		t.Fatalf("width %d", w)
	}
	if h := lipgloss.Height(view.Content); h != 24 {
		t.Fatalf("height %d\n%s", h, plain)
	}
}

func TestForgetScrollback(t *testing.T) {
	var buf strings.Builder
	forgetScrollback(&buf, "iTerm.app")
	if !strings.Contains(buf.String(), "\x1b[3J") || !strings.Contains(buf.String(), "1337;ClearScrollback") {
		t.Fatalf("%q", buf.String())
	}
	buf.Reset()
	forgetScrollback(&buf, "Apple_Terminal")
	if strings.Contains(buf.String(), "1337") || !strings.Contains(buf.String(), "\x1b[3J") {
		t.Fatalf("%q", buf.String())
	}
}

func TestFocusRepaints(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(tea.FocusMsg{})
	if cmd == nil {
		t.Fatal("expected a repaint")
	}
}

func TestSubmitShowsUserAndStaysBusy(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("how many lines?")
	_, cmd := m.Update(press(tea.KeyEnter, 0))
	if cmd == nil {
		t.Fatal("expected a command")
	}
	if !m.tab().busy || m.tab().activity != "thinking" {
		t.Fatalf("busy=%v status=%q", m.tab().busy, m.tab().activity)
	}
	plain := ansi.Strip(m.renderTranscript())
	if !strings.Contains(plain, "❯ how many lines?") {
		t.Fatalf("transcript %q", plain)
	}
}

func TestEmptyEnterDoesNothing(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(press(tea.KeyEnter, 0))
	if cmd != nil || m.tab().busy || len(m.tab().blocks) != 0 {
		t.Fatalf("busy=%v blocks=%d cmd=%v", m.tab().busy, len(m.tab().blocks), cmd != nil)
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
	m.tab().busy = true
	m.tab().cancel = func() { canceled = true }
	_, cmd := m.Update(press('c', tea.ModCtrl))
	if !canceled || m.quitting || cmd != nil {
		t.Fatalf("canceled=%v quitting=%v", canceled, m.quitting)
	}
}

func TestTurnMessagesAppendInOrder(t *testing.T) {
	m := newTestModel(t)
	m.tab().turnID = 1
	m.tab().busy = true
	m.tab().events = make(chan tea.Msg)
	_, cmd := m.Update(eventMsg{tabID: 1, id: 1, event: agent.ToolStart{ID: "call_1", Name: "list_dir", Detail: "path=."}})
	if cmd == nil {
		t.Fatal("expected to keep listening")
	}
	updated, _ := m.Update(eventMsg{tabID: 1, id: 1, event: agent.AnswerDelta{Text: "two files"}})
	updated, _ = updated.(*model).Update(turnDoneMsg{tabID: 1, id: 1, answer: "two files"})
	got := updated.(*model)
	if got.tab().busy {
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
	m.tab().turnID = 1
	m.tab().busy = true
	m.tab().events = make(chan tea.Msg)
	m.Update(eventMsg{tabID: 1, id: 1, event: agent.ReasoningDelta{Text: "Think"}})
	m.Update(eventMsg{tabID: 1, id: 1, event: agent.ReasoningDelta{Text: "ing"}})
	m.Update(eventMsg{tabID: 1, id: 1, event: agent.AnswerDelta{Text: "Hi"}})
	if len(m.tab().blocks) != 2 || m.tab().blocks[0].kind != blockReasoning || m.tab().blocks[0].text != "Thinking" {
		t.Fatalf("reasoning %+v", m.tab().blocks)
	}
	if m.tab().blocks[1].kind != blockAssistant || m.tab().blocks[1].text != "Hi" {
		t.Fatalf("answer %+v", m.tab().blocks[1])
	}
	plain := ansi.Strip(m.renderTranscript())
	if !strings.Contains(plain, "Thinking") || !strings.Contains(plain, "Hi") {
		t.Fatalf("transcript %q", plain)
	}
}

func TestReasoningOnlyAnswerIsNotDuplicated(t *testing.T) {
	m := newTestModel(t)
	m.tab().turnID = 1
	m.tab().busy = true
	m.tab().events = make(chan tea.Msg)
	m.Update(eventMsg{tabID: 1, id: 1, event: agent.ReasoningDelta{Text: "the answer"}})
	updated, _ := m.Update(turnDoneMsg{tabID: 1, id: 1, answer: "the answer"})
	got := updated.(*model)
	if len(got.tab().blocks) != 1 || got.tab().blocks[0].kind != blockReasoning || got.tab().blocks[0].text != "the answer" {
		t.Fatalf("blocks %+v", got.tab().blocks)
	}
}

func TestAnswerDeltasGrowOneBlock(t *testing.T) {
	m := newTestModel(t)
	m.tab().turnID = 1
	m.tab().busy = true
	m.tab().events = make(chan tea.Msg)
	m.Update(eventMsg{tabID: 1, id: 1, event: agent.AnswerDelta{Text: "hel"}})
	m.Update(eventMsg{tabID: 1, id: 1, event: agent.AnswerDelta{Text: "lo"}})
	if len(m.tab().blocks) != 1 || m.tab().blocks[0].kind != blockAssistant || m.tab().blocks[0].text != "hello" {
		t.Fatalf("blocks %+v", m.tab().blocks)
	}
	if m.tab().activity != "answering" {
		t.Fatalf("status %q", m.tab().activity)
	}
}

func TestToolErrorMarksTheLine(t *testing.T) {
	m := newTestModel(t)
	m.tab().turnID = 1
	m.tab().busy = true
	m.tab().events = make(chan tea.Msg)
	m.Update(eventMsg{tabID: 1, id: 1, event: agent.ToolStart{ID: "call_1", Name: "read_file", Detail: "path=missing"}})
	m.Update(eventMsg{tabID: 1, id: 1, event: agent.ToolDone{ID: "call_1", Err: errors.New("missing")}})
	if len(m.tab().blocks) != 1 || !m.tab().blocks[0].failed || !m.tab().blocks[0].done {
		t.Fatalf("blocks %+v", m.tab().blocks)
	}
	plain := ansi.Strip(m.renderTranscript())
	if !strings.Contains(plain, "Read") || !strings.Contains(plain, "path=missing") {
		t.Fatalf("transcript %q", plain)
	}
}

func TestCtrlCKeepsPartialAnswer(t *testing.T) {
	m := newTestModel(t)
	m.tab().turnID = 1
	m.tab().busy = true
	m.tab().events = make(chan tea.Msg)
	canceled := false
	m.tab().cancel = func() { canceled = true }
	m.Update(eventMsg{tabID: 1, id: 1, event: agent.AnswerDelta{Text: "partial answer"}})
	_, cmd := m.Update(press('c', tea.ModCtrl))
	if !canceled || m.quitting || cmd != nil {
		t.Fatalf("canceled=%v quitting=%v cmd=%v", canceled, m.quitting, cmd != nil)
	}
	updated, cmd := m.Update(turnDoneMsg{tabID: 1, id: 1, answer: "partial answer", err: context.Canceled})
	got := updated.(*model)
	plain := ansi.Strip(got.renderTranscript())
	if !strings.Contains(plain, "partial answer") || !strings.Contains(plain, "interrupted") {
		t.Fatalf("transcript %q", plain)
	}
	if got.tab().busy || cmd == nil {
		t.Fatal("expected the turn to finish")
	}
}

func TestStaleTurnIsIgnored(t *testing.T) {
	m := newTestModel(t)
	m.tab().turnID = 2
	m.tab().busy = true
	updated, _ := m.Update(turnDoneMsg{tabID: 1, id: 1, answer: "stale"})
	got := updated.(*model)
	if !got.tab().busy || len(got.tab().blocks) != 0 {
		t.Fatalf("busy=%v blocks=%d", got.tab().busy, len(got.tab().blocks))
	}
}

func TestCanceledTurnNotesInterruption(t *testing.T) {
	m := newTestModel(t)
	m.tab().turnID = 1
	m.tab().busy = true
	updated, cmd := m.Update(turnDoneMsg{tabID: 1, id: 1, err: context.Canceled})
	got := updated.(*model)
	if got.tab().busy || got.quitting || cmd == nil {
		t.Fatalf("busy=%v quitting=%v", got.tab().busy, got.quitting)
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
	m.tab().turnID = 1
	m.tab().busy = true
	_, cmd := m.Update(turnDoneMsg{tabID: 1, id: 1, err: context.Canceled})
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
	m.tab().blocks = []block{{
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
	m.tab().blocks = []block{
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
	models    []string
	providers []string
	picked    string
	session   *agent.Session
	selected  *agent.Session
	opened    []*agent.Session
	cwd       string
	ready     bool
	openErr   error
}

func (f *fakeDirector) Models(context.Context) ([]string, error) { return f.models, nil }
func (f *fakeDirector) Select(_ context.Context, ref string, session *agent.Session) error {
	f.picked = ref
	f.selected = session
	return nil
}
func (f *fakeDirector) Open(context.Context) (*agent.Session, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	dir := f.cwd
	if dir == "" {
		dir = "."
	}
	modelName := f.picked
	if modelName == "" {
		modelName = "test-model"
	}
	session := agent.NewSession(openai.New("http://127.0.0.1:1"), modelName, dir, nil)
	f.opened = append(f.opened, session)
	return session, nil
}
func (f *fakeDirector) Providers() []string          { return f.providers }
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
		session: m.tab().session,
		ready:   true,
	}
	m.director = director
	m.input.SetValue("/models")
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if got.tab().busy || got.mode != modeModels || cmd == nil {
		t.Fatalf("busy=%v mode=%d cmd=%v", got.tab().busy, got.mode, cmd != nil)
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
	if cmd == nil || got.tab().busy {
		t.Fatalf("busy=%v cmd=%v", got.tab().busy, cmd != nil)
	}
	done := cmd().(selectDoneMsg)
	updated, _ = got.Update(done)
	got = updated.(*model)
	if director.picked != "ollama/a" || got.mode != modeChat || got.modelName != "ollama/a" {
		t.Fatalf("picked %q mode %d name %q", director.picked, got.mode, got.modelName)
	}
	if got.tab().busy || strings.Contains(ansi.Strip(got.renderTranscript()), "❯ /models") {
		t.Fatal("command was sent as a chat turn")
	}
}

func TestConnectOpensProviderPicker(t *testing.T) {
	m := newTestModel(t)
	director := &fakeDirector{
		providers: []string{"gemini", "openrouter"},
		session:   m.tab().session,
		ready:     true,
	}
	m.director = director
	m.input.SetValue("/connect")
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if got.tab().busy || got.mode != modeProviders || cmd != nil {
		t.Fatalf("busy=%v mode=%d cmd=%v", got.tab().busy, got.mode, cmd != nil)
	}
	plain := ansi.Strip(got.View().Content)
	if !strings.Contains(plain, "gemini") || !strings.Contains(plain, "openrouter") || !strings.Contains(plain, "choose a provider") {
		t.Fatalf("picker %q", plain)
	}
	if h := lipgloss.Height(got.View().Content); h != 24 {
		t.Fatalf("height %d\n%s", h, plain)
	}

	updated, _ = got.Update(tea.KeyPressMsg{Text: "open"})
	got = updated.(*model)
	plain = ansi.Strip(got.View().Content)
	if strings.Contains(plain, "gemini") || !strings.Contains(plain, "openrouter") {
		t.Fatalf("filter %q", plain)
	}

	updated, cmd = got.Update(press(tea.KeyEnter, 0))
	got = updated.(*model)
	if cmd != nil || got.mode != modeConnect || got.connectID != "openrouter" {
		t.Fatalf("mode %d id %q cmd=%v", got.mode, got.connectID, cmd != nil)
	}
	plain = ansi.Strip(got.View().Content)
	if !strings.Contains(plain, "key for openrouter") {
		t.Fatalf("key prompt %q", plain)
	}
	if strings.Contains(ansi.Strip(got.renderTranscript()), "❯ /connect") {
		t.Fatal("command was sent as a chat turn")
	}
}

func TestConnectWithProviderSkipsPicker(t *testing.T) {
	m := newTestModel(t)
	m.director = &fakeDirector{
		providers: []string{"gemini", "openrouter"},
		session:   m.tab().session,
		ready:     true,
	}
	m.input.SetValue("/connect gemini")
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if cmd != nil || got.mode != modeConnect || got.connectID != "gemini" {
		t.Fatalf("mode %d id %q cmd=%v", got.mode, got.connectID, cmd != nil)
	}
	plain := ansi.Strip(got.View().Content)
	if strings.Contains(plain, "openrouter") || !strings.Contains(plain, "key for gemini") {
		t.Fatalf("view %q", plain)
	}
}

func TestConnectWithNoProviders(t *testing.T) {
	m := newTestModel(t)
	m.director = &fakeDirector{session: m.tab().session, ready: true}
	m.input.SetValue("/connect")
	updated, _ := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	plain := ansi.Strip(got.View().Content)
	if got.mode != modeProviders || !strings.Contains(plain, "no providers accept an API key") {
		t.Fatalf("mode %d view %q", got.mode, plain)
	}
}

func TestTabCompletesSlashCommands(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/")
	m.layout()
	plain := ansi.Strip(m.View().Content)
	if !strings.Contains(plain, "> /models") || !strings.Contains(plain, "  /connect") || !strings.Contains(plain, "up/down move") {
		t.Fatalf("suggestions %q", plain)
	}
	if h := lipgloss.Height(m.View().Content); h != 24 {
		t.Fatalf("height %d\n%s", h, plain)
	}

	updated, _ := m.Update(press(tea.KeyDown, 0))
	got := updated.(*model)
	if got.input.Value() != "/" {
		t.Fatalf("filter %q", got.input.Value())
	}
	plain = ansi.Strip(got.View().Content)
	if !strings.Contains(plain, "> /connect") || !strings.Contains(plain, "  /models") {
		t.Fatalf("moved %q", plain)
	}

	updated, _ = got.Update(press(tea.KeyTab, 0))
	got = updated.(*model)
	if got.input.Value() != "/connect" {
		t.Fatalf("value %q", got.input.Value())
	}
	if plain = ansi.Strip(got.View().Content); strings.Contains(plain, "> /connect") {
		t.Fatalf("list lingered: %q", plain)
	}

	got.input.Focus()
	updated, _ = got.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	got = updated.(*model)
	if got.input.Value() != "/connectx" {
		t.Fatalf("typed %q", got.input.Value())
	}
}

func TestTabCompletesUniqueCommand(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/c")
	m.layout()
	if plain := ansi.Strip(m.View().Content); !strings.Contains(plain, "/connect") {
		t.Fatalf("hint %q", plain)
	}
	updated, _ := m.Update(press(tea.KeyTab, 0))
	got := updated.(*model)
	if got.input.Value() != "/connect" {
		t.Fatalf("value %q", got.input.Value())
	}
	if plain := ansi.Strip(got.View().Content); strings.Contains(plain, "tab complete") {
		t.Fatalf("hint lingered: %q", plain)
	}

	m = newTestModel(t)
	m.input.SetValue("hello")
	updated, _ = m.Update(press(tea.KeyTab, 0))
	got = updated.(*model)
	if got.input.Value() != "hello" {
		t.Fatalf("plain text %q", got.input.Value())
	}
}

func TestUnknownCommandIsNotATurn(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/nope")
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if got.tab().busy || cmd != nil {
		t.Fatalf("busy=%v cmd=%v", got.tab().busy, cmd != nil)
	}
	plain := ansi.Strip(got.renderTranscript())
	if !strings.Contains(plain, "unknown command") {
		t.Fatalf("transcript %q", plain)
	}
}

func TestStartupEscQuits(t *testing.T) {
	m := newTestModel(t)
	m.tab().session = nil
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
	m.director = &fakeDirector{session: m.tab().session, ready: true}
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

func TestWheelScrollsALineAtATime(t *testing.T) {
	m := newTestModel(t)
	lines := make([]string, 80)
	for i := range lines {
		lines[i] = "row"
	}
	m.tab().blocks = []block{{kind: blockNote, text: strings.Join(lines, "\n")}}
	m.layout()
	m.paint()
	if !m.viewport.AtBottom() || m.viewport.YOffset() == 0 {
		t.Fatalf("offset %d bottom %v", m.viewport.YOffset(), m.viewport.AtBottom())
	}
	bottom := m.viewport.YOffset()
	updated, _ := m.Update(tea.MouseWheelMsg{Y: 2, Button: tea.MouseWheelUp})
	got := updated.(*model)
	if got.viewport.YOffset() != bottom-1 || got.viewport.AtBottom() {
		t.Fatalf("offset %d bottom %v", got.viewport.YOffset(), got.viewport.AtBottom())
	}
	plain := ansi.Strip(got.View().Content)
	if !strings.Contains(plain, "pgdn to follow") || !strings.Contains(plain, "┃") {
		t.Fatalf("view %q", plain)
	}
	if lipgloss.Width(got.View().Content) > 80 {
		t.Fatalf("width %d", lipgloss.Width(got.View().Content))
	}

	y := lipgloss.Height(got.headerLine()) + got.viewport.Height()
	offset := got.viewport.YOffset()
	updated, _ = got.Update(tea.MouseWheelMsg{Y: y, Button: tea.MouseWheelUp})
	got = updated.(*model)
	if got.viewport.YOffset() != offset {
		t.Fatalf("wheel over the composer moved the transcript to %d", got.viewport.YOffset())
	}

	updated, _ = got.Update(tea.MouseWheelMsg{Y: 2, Button: tea.MouseWheelDown})
	got = updated.(*model)
	if !got.viewport.AtBottom() {
		t.Fatal("wheel down should pin to the bottom again")
	}
}

func TestWheelMovesThePicker(t *testing.T) {
	m := newTestModel(t)
	m.director = &fakeDirector{providers: []string{"gemini", "openrouter"}, session: m.tab().session, ready: true}
	m.input.SetValue("/connect")
	updated, _ := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	updated, _ = got.Update(tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	got = updated.(*model)
	plain := ansi.Strip(got.View().Content)
	if !strings.Contains(plain, "> openrouter") || strings.Contains(plain, "> gemini") {
		t.Fatalf("picker %q", plain)
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

func TestNewSessionFocusesIt(t *testing.T) {
	m := newTestModel(t)
	m.tab().blocks = []block{{kind: blockUser, text: "first"}}
	m.director = &fakeDirector{ready: true, cwd: m.cwd, session: m.tab().session}
	m.input.SetValue("/new")
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if cmd == nil {
		t.Fatal("expected a new session")
	}
	updated, _ = got.Update(cmd().(openedMsg))
	got = updated.(*model)
	if len(got.tabs) != 2 || got.focus != 1 || got.tab().session == got.tabs[0].session {
		t.Fatalf("tabs %d focus %d", len(got.tabs), got.focus)
	}
	if got.tabs[0].blocks[0].text != "first" {
		t.Fatal("lost the first transcript")
	}
	plain := ansi.Strip(got.renderTranscript())
	if strings.Contains(plain, "first") || !strings.Contains(plain, "Ask about this directory") {
		t.Fatalf("transcript %q", plain)
	}
	view := ansi.Strip(got.View().Content)
	if !strings.Contains(view, "New session") || !strings.Contains(view, "○") {
		t.Fatalf("panel %q", view)
	}
}

func TestBackgroundTurnLeavesTheVisibleTranscript(t *testing.T) {
	m := newTestModel(t)
	m.tab().blocks = []block{{kind: blockUser, text: "visible"}}
	other := agent.NewSession(openai.New("http://127.0.0.1:1"), "test-model", m.cwd, nil)
	m.tabs = append(m.tabs, newTab(2, other))
	back := &m.tabs[1]
	back.busy = true
	back.state = statusWorking
	back.turnID = 1
	back.events = make(chan tea.Msg)
	before := ansi.Strip(m.renderTranscript())
	m.Update(eventMsg{tabID: 2, id: 1, event: agent.AnswerDelta{Text: "hidden"}})
	if ansi.Strip(m.renderTranscript()) != before {
		t.Fatal("visible transcript changed")
	}
	if m.tabs[1].state != statusWorking || len(m.tabs[1].blocks) != 1 || m.tabs[1].blocks[0].text != "hidden" {
		t.Fatalf("background %+v", m.tabs[1].blocks)
	}
	m.input.SetValue("next")
	_, cmd := m.Update(press(tea.KeyEnter, 0))
	if cmd == nil || !m.tab().busy || !m.tabs[1].busy {
		t.Fatalf("focused busy %v background busy %v", m.tab().busy, m.tabs[1].busy)
	}
}

func TestSessionsCommandSwitchesTranscript(t *testing.T) {
	m := newTestModel(t)
	m.tab().blocks = []block{{kind: blockUser, text: "alpha"}}
	m.tab().draft = "keep me"
	other := agent.NewSession(openai.New("http://127.0.0.1:1"), "test-model", m.cwd, nil)
	m.tabs = append(m.tabs, newTab(2, other))
	m.tabs[1].blocks = []block{{kind: blockUser, text: "beta"}}
	m.focus = 1
	m.paint()
	m.input.SetValue("/sessions")
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if got.mode != modeSessions || cmd != nil {
		t.Fatalf("mode %d cmd %v", got.mode, cmd != nil)
	}
	updated, _ = got.Update(press(tea.KeyUp, 0))
	updated, _ = updated.(*model).Update(press(tea.KeyEnter, 0))
	got = updated.(*model)
	if got.focus != 0 || got.mode != modeChat {
		t.Fatalf("focus %d mode %d", got.focus, got.mode)
	}
	plain := ansi.Strip(got.renderTranscript())
	if !strings.Contains(plain, "alpha") || strings.Contains(plain, "beta") {
		t.Fatalf("transcript %q", plain)
	}
	if got.input.Value() != "keep me" {
		t.Fatalf("draft %q", got.input.Value())
	}
	if got.tabs[1].busy {
		t.Fatal("switching canceled the other session")
	}
}

func TestNarrowTerminalHidesTheSessionPanel(t *testing.T) {
	m := newTestModel(t)
	m.width = 40
	m.layout()
	m.paint()
	plain := ansi.Strip(m.View().Content)
	if strings.Contains(plain, "New session") {
		t.Fatalf("panel shown: %q", plain)
	}
}
