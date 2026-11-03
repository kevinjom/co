package ui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
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
	m.tab().pipe = newTurnPipe()
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

func TestSessionTitleUpdatesThePanel(t *testing.T) {
	m := newTestModel(t)
	m.tab().session.Restore([]openai.Message{{Role: "user", Content: "how many lines are in this file?"}})
	cmd := m.watchTitle(m.tab())
	if cmd == nil {
		t.Fatal("expected a title watch")
	}
	m.tab().session.SetTitle("Count the lines")
	renamed, ok := cmd().(titleMsg)
	if !ok || renamed.title != "Count the lines" {
		t.Fatalf("msg %#v", cmd)
	}
	updated, next := m.Update(renamed)
	got := updated.(*model)
	plain := ansi.Strip(got.sessionPanel())
	if got.tab().title != "Count the lines" || !strings.Contains(plain, "Count the lines") || strings.Contains(plain, "how many lines") {
		t.Fatalf("panel %q", plain)
	}
	if strings.Contains(plain, "done") || strings.Contains(plain, "new") {
		t.Fatalf("status word in %q", plain)
	}
	if next == nil {
		t.Fatal("stopped watching the title")
	}
}

func TestWorkingStatusAnimates(t *testing.T) {
	m := newTestModel(t)
	m.tab().busy = true
	m.tab().state = statusWorking
	m.tab().title = "Count the lines"
	plain := ansi.Strip(m.sessionPanel())
	first := ansi.Strip(m.spin.View())
	if strings.Contains(plain, "working") || first == "" || !strings.Contains(plain, first) {
		t.Fatalf("icon %q panel %q", first, plain)
	}
	updated, cmd := m.Update(m.spin.Tick())
	got := updated.(*model)
	second := ansi.Strip(got.spin.View())
	if second == "" || second == first || !strings.Contains(ansi.Strip(got.sessionPanel()), second) || cmd == nil {
		t.Fatalf("frame %q -> %q cmd %v", first, second, cmd != nil)
	}
}

func TestInputStaysOpenWhileBusy(t *testing.T) {
	m := newTestModel(t)
	m.input.Focus()
	m.input.SetValue("hello")
	m.Update(press(tea.KeyEnter, 0))
	if !m.tab().busy || !m.input.Focused() {
		t.Fatalf("busy %v focused %v", m.tab().busy, m.input.Focused())
	}
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	got := updated.(*model)
	if got.input.Value() != "a" || got.tab().turnID != 1 {
		t.Fatalf("value %q turn %d", got.input.Value(), got.tab().turnID)
	}
}

func TestEnterWhileBusyQueuesTheLine(t *testing.T) {
	m := newTestModel(t)
	m.tab().busy = true
	m.tab().turnID = 4
	m.input.SetValue("look here")
	_, cmd := m.Update(press(tea.KeyEnter, 0))
	if cmd != nil || !m.tab().busy || m.tab().turnID != 4 || m.input.Value() != "" {
		t.Fatalf("busy %v turn %d value %q cmd %v", m.tab().busy, m.tab().turnID, m.input.Value(), cmd != nil)
	}
	if len(m.tab().queue) != 1 || m.tab().queue[0] != "look here" {
		t.Fatalf("queue %#v", m.tab().queue)
	}
	if strings.Contains(ansi.Strip(m.renderTranscript()), "look here") {
		t.Fatal("queued line entered the transcript")
	}
	plain := ansi.Strip(m.View().Content)
	if !strings.Contains(plain, "· look here") || !strings.Contains(plain, "enter queue") {
		t.Fatalf("view %q", plain)
	}
	if h := lipgloss.Height(m.View().Content); h != 24 {
		t.Fatalf("height %d", h)
	}

	m.input.SetValue("and the tests")
	m.Update(press(tea.KeyEnter, 0))
	plain = ansi.Strip(m.View().Content)
	first := strings.Index(plain, "· look here")
	second := strings.Index(plain, "· and the tests")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("queue order %q", plain)
	}
}

func TestFinishedTurnSendsTheQueue(t *testing.T) {
	m := newTestModel(t)
	m.tab().turnID = 1
	m.tab().busy = true
	m.tab().queue = []string{"one", "two"}
	updated, cmd := m.Update(turnDoneMsg{tabID: 1, id: 1, answer: "done"})
	got := updated.(*model)
	if cmd == nil || !got.tab().busy || len(got.tab().queue) != 0 || got.tab().turnID != 2 {
		t.Fatalf("busy %v turn %d queue %#v cmd %v", got.tab().busy, got.tab().turnID, got.tab().queue, cmd != nil)
	}
	plain := ansi.Strip(got.renderTranscript())
	one := strings.Index(plain, "one")
	two := strings.Index(plain, "two")
	if one < 0 || two < 0 || one > two || strings.Contains(plain, "· one") {
		t.Fatalf("transcript %q", plain)
	}
	if strings.Contains(ansi.Strip(got.View().Content), "· one") {
		t.Fatal("queue still shown")
	}
}

func TestCommandWhileBusyIsNotQueued(t *testing.T) {
	m := newTestModel(t)
	m.tab().busy = true
	m.tab().turnID = 2
	m.director = &fakeDirector{models: []string{"ollama/a"}, session: m.tab().session, ready: true}
	m.input.SetValue("/models")
	updated, cmd := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if cmd == nil || got.mode != modeModels || len(got.tab().queue) != 0 || !got.tab().busy {
		t.Fatalf("mode %d busy %v queue %#v cmd %v", got.mode, got.tab().busy, got.tab().queue, cmd != nil)
	}
}

func TestCtrlCClearsTheBoxBeforeStopping(t *testing.T) {
	m := newTestModel(t)
	canceled := false
	m.tab().busy = true
	m.tab().cancel = func() { canceled = true }
	m.tab().queue = []string{"stay"}
	m.input.SetValue("draft")
	m.Update(press('c', tea.ModCtrl))
	if canceled || m.input.Value() != "" || len(m.tab().queue) != 1 || m.quitting {
		t.Fatalf("canceled %v value %q queue %#v", canceled, m.input.Value(), m.tab().queue)
	}
	m.Update(press('c', tea.ModCtrl))
	if !canceled || len(m.tab().queue) != 0 || m.quitting {
		t.Fatalf("canceled %v queue %#v quitting %v", canceled, m.tab().queue, m.quitting)
	}
}

func TestEscRecallsTheNewestQueuedLine(t *testing.T) {
	m := newTestModel(t)
	m.tab().busy = true
	m.tab().queue = []string{"first", "second"}
	m.input.SetValue("typing")
	m.Update(press(tea.KeyEsc, 0))
	if m.input.Value() != "typing" || len(m.tab().queue) != 2 {
		t.Fatalf("value %q queue %#v", m.input.Value(), m.tab().queue)
	}
	m.input.SetValue("")
	m.Update(press(tea.KeyEsc, 0))
	if m.input.Value() != "second" || len(m.tab().queue) != 1 || m.tab().queue[0] != "first" {
		t.Fatalf("value %q queue %#v", m.input.Value(), m.tab().queue)
	}
	plain := ansi.Strip(m.View().Content)
	if strings.Contains(plain, "· second") || !strings.Contains(plain, "· first") {
		t.Fatalf("view %q", plain)
	}
}

func TestQueueBelongsToTheSession(t *testing.T) {
	m := newTestModel(t)
	m.tab().busy = true
	m.tab().queue = []string{"only mine"}
	other := agent.NewSession(openai.New("http://127.0.0.1:1"), "test-model", m.cwd, nil)
	m.tabs = append(m.tabs, newTab(2, other))
	m.switchTo(1)
	if strings.Contains(ansi.Strip(m.View().Content), "only mine") {
		t.Fatal("other session shows the queue")
	}
	m.input.SetValue("hello")
	_, cmd := m.Update(press(tea.KeyEnter, 0))
	if cmd == nil || len(m.tabs[0].queue) != 1 || m.tabs[0].queue[0] != "only mine" {
		t.Fatalf("queue %#v cmd %v", m.tabs[0].queue, cmd != nil)
	}
	m.switchTo(0)
	if !strings.Contains(ansi.Strip(m.View().Content), "· only mine") {
		t.Fatal("queue lost")
	}
}

func TestCoalesceKeepsToolBoundaries(t *testing.T) {
	pending := coalesceTurn(nil, eventMsg{id: 1, event: agent.AnswerDelta{Text: "a"}})
	pending = coalesceTurn(pending, eventMsg{id: 1, event: agent.AnswerDelta{Text: "b"}})
	pending = coalesceTurn(pending, eventMsg{id: 1, event: agent.ToolStart{ID: "1", Name: "list_dir"}})
	pending = coalesceTurn(pending, eventMsg{id: 1, event: agent.AnswerDelta{Text: "c"}})
	if len(pending) != 3 {
		t.Fatalf("len %d", len(pending))
	}
	got, ok := pending[0].(eventMsg)
	if !ok {
		t.Fatalf("first %T", pending[0])
	}
	delta, ok := got.event.(agent.AnswerDelta)
	if !ok || delta.Text != "ab" {
		t.Fatalf("delta %#v", got.event)
	}
}

func TestFocusedStreamPaintsBeforeTheTurnEnds(t *testing.T) {
	m := newTestModel(t)
	gate := &gateChat{entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	release := func() { once.Do(func() { close(gate.release) }) }
	defer release()
	m.tab().session = agent.NewSession(gate, "test-model", m.cwd, nil)
	cmd := m.startTurn(m.tab(), []string{"q"})
	select {
	case <-gate.entered:
	case <-time.After(time.Second):
		t.Fatal("stream did not start")
	}
	updated, cmd := m.Update(awaitMsg(t, cmd))
	got := updated.(*model)
	plain := ansi.Strip(got.renderTranscript())
	if !strings.Contains(plain, "hel") || strings.Contains(plain, "hello") || !got.tab().busy || got.tab().activity != "answering" {
		t.Fatalf("busy %v activity %q transcript %q", got.tab().busy, got.tab().activity, plain)
	}
	release()
	updated, _ = got.Update(awaitMsg(t, cmd))
	got = updated.(*model)
	plain = ansi.Strip(got.renderTranscript())
	if got.tab().busy || got.tab().state != statusDone || !strings.Contains(plain, "hello") {
		t.Fatalf("busy %v state %v transcript %q", got.tab().busy, got.tab().state, plain)
	}
	answers := 0
	for _, block := range got.tab().blocks {
		if block.kind == blockAssistant {
			answers++
			if block.text != "hello" {
				t.Fatalf("answer %q", block.text)
			}
		}
	}
	if answers != 1 {
		t.Fatalf("assistant blocks %d", answers)
	}
}

func TestBackgroundStreamIsNotHeld(t *testing.T) {
	const chunks = 1000
	flood := &floodChat{n: chunks, done: make(chan struct{})}
	m := newTestModel(t)
	m.tab().blocks = []block{{kind: blockUser, text: "visible"}}
	m.tabs = append(m.tabs, newTab(2, agent.NewSession(flood, "test-model", m.cwd, nil)))
	cmd := m.startTurn(&m.tabs[1], []string{"hi"})
	select {
	case <-flood.done:
	case <-time.After(time.Second):
		t.Fatal("background stream blocked on the UI")
	}
	if strings.Contains(ansi.Strip(m.renderTranscript()), "xxx") {
		t.Fatal("visible transcript changed while the other session streamed")
	}

	got := m
	deadline := time.Now().Add(time.Second)
	for got.tabs[1].state != statusDone {
		if time.Now().After(deadline) {
			t.Fatalf("state %v", got.tabs[1].state)
		}
		updated, next := got.Update(awaitMsg(t, cmd))
		got = updated.(*model)
		cmd = next
	}
	if got.focus != 0 {
		t.Fatalf("focus %d", got.focus)
	}
	plain := ansi.Strip(got.renderTranscript())
	if !strings.Contains(plain, "visible") || strings.Contains(plain, "xxx") {
		t.Fatalf("transcript %q", plain)
	}
	answers := 0
	for _, block := range got.tabs[1].blocks {
		if block.kind != blockAssistant {
			continue
		}
		answers++
		if block.text != strings.Repeat("x", chunks) {
			t.Fatalf("answer length %d", len(block.text))
		}
	}
	if answers != 1 {
		t.Fatalf("assistant blocks %d", answers)
	}
	panel := ansi.Strip(got.sessionPanel())
	if !strings.Contains(panel, "●") || !strings.Contains(panel, "○") {
		t.Fatalf("panel %q", panel)
	}
}

func awaitMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	ch := make(chan tea.Msg, len(batch))
	for _, item := range batch {
		go func() {
			if item == nil {
				ch <- nil
				return
			}
			ch <- item()
		}()
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	pending := len(batch)
	for pending > 0 {
		select {
		case got := <-ch:
			pending--
			if _, tick := got.(spinner.TickMsg); tick || got == nil {
				continue
			}
			if _, wake := got.(pipeMsg); wake {
				return got
			}
		case <-timer.C:
			t.Fatal("timed out waiting for the turn")
		}
	}
	t.Fatal("turn produced no event")
	return nil
}

type floodChat struct {
	n    int
	done chan struct{}
}

func (f *floodChat) ChatStream(
	_ context.Context,
	_ string,
	_ []openai.Message,
	_ []openai.Tool,
	onChunk func(openai.StreamKind, string),
) (openai.Message, error) {
	var b strings.Builder
	for range f.n {
		if onChunk != nil {
			onChunk(openai.StreamContent, "x")
		}
		b.WriteString("x")
	}
	close(f.done)
	return openai.Message{Role: "assistant", Content: b.String()}, nil
}

type gateChat struct {
	entered chan struct{}
	release chan struct{}
}

func (g *gateChat) ChatStream(
	_ context.Context,
	_ string,
	_ []openai.Message,
	_ []openai.Tool,
	onChunk func(openai.StreamKind, string),
) (openai.Message, error) {
	if onChunk != nil {
		onChunk(openai.StreamContent, "hel")
	}
	close(g.entered)
	<-g.release
	if onChunk != nil {
		onChunk(openai.StreamContent, "lo")
	}
	return openai.Message{Role: "assistant", Content: "hello"}, nil
}
