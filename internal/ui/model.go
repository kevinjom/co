package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"

	"github.com/kevinjom/co/internal/agent"
	"github.com/kevinjom/co/internal/command"
)

const (
	paintGap    = time.Second / 30
	eventBuffer = 256
)

type blockKind int

const (
	blockUser blockKind = iota
	blockTool
	blockAssistant
	blockError
	blockNote
	blockReasoning
)

type block struct {
	kind   blockKind
	text   string
	tool   string
	detail string
	toolID string
	failed bool
	done   bool
}

type eventMsg struct {
	tabID int
	id    int
	event agent.Event
}

type paintMsg struct{}

type turnDoneMsg struct {
	tabID  int
	id     int
	answer string
	err    error
}

type openedMsg struct {
	session *agent.Session
	err     error
}

type titleMsg struct {
	tabID int
	title string
}

type inputMode int

const (
	modeChat inputMode = iota
	modeModels
	modeProviders
	modeConnect
	modeSessions
)

// model is the interactive session. The program takes the whole terminal.
// The transcript scrolls inside that frame, a line at a time, and stays
// pinned to the bottom until the user scrolls up.
type model struct {
	tabs    []tab
	focus   int
	nextTab int

	director  Director
	ctx       context.Context
	modelName string
	cwd       string
	mode      inputMode
	startup   bool
	pending   bool

	filter        string
	cursor        int
	modelIDs      []string
	providerIDs   []string
	modelsLoaded  bool
	pickerErr     string
	connectID     string
	secret        string
	completeIndex int

	theme    theme
	input    textarea.Model
	viewport viewport.Model
	spin     spinner.Model
	md       markdownCache

	width  int
	height int

	dirty        bool
	paintWaiting bool
	lastPaint    time.Time

	quitting      bool
	explainResume bool
	exitErr       error
}

func newModel(ctx context.Context, session *agent.Session, modelName, cwd string) *model {
	if ctx == nil {
		ctx = context.Background()
	}
	th := newTheme(true)
	m := &model{
		tabs:      []tab{newTab(1, session)},
		nextTab:   2,
		ctx:       ctx,
		modelName: modelName,
		cwd:       cwd,
		theme:     th,
		input:     newInput(th),
		viewport:  viewport.New(),
		spin:      spinner.New(spinner.WithSpinner(spinner.Dot)),
		width:     80,
		height:    24,
	}
	m.viewport.SoftWrap = true
	// Wheel events are handled on the model so each tick moves one line,
	// matching a terminal scrollback rather than the viewport's 3-line step.
	m.viewport.MouseWheelEnabled = false
	m.spin.Style = lipgloss.NewStyle().Foreground(th.accentColor)
	m.layout()
	m.paint()
	if len(m.tab().blocks) > 0 {
		m.viewport.GotoBottom()
	}
	return m
}

func historyBlocks(session *agent.Session) []block {
	if session == nil {
		return nil
	}
	var blocks []block
	for _, msg := range session.Messages() {
		switch msg.Role {
		case "user":
			blocks = append(blocks, block{kind: blockUser, text: msg.Content})
		case "assistant":
			if strings.TrimSpace(msg.Content) != "" {
				blocks = append(blocks, block{kind: blockAssistant, text: msg.Content})
			}
			for _, call := range msg.ToolCalls {
				blocks = append(blocks, block{
					kind:   blockTool,
					tool:   call.Function.Name,
					detail: agent.ToolDetail(call),
					toolID: call.ID,
				})
			}
		case "tool":
			if msg.ToolCallID == "" {
				continue
			}
			for i := range blocks {
				if blocks[i].kind == blockTool && !blocks[i].done && blocks[i].toolID == msg.ToolCallID {
					blocks[i].done = true
					break
				}
			}
		}
	}
	return blocks
}

func newInput(th theme) textarea.Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.Placeholder = "> Ask about this codebase"
	ta.ShowLineNumbers = false
	ta.CharLimit = 64 * 1024
	ta.MaxHeight = 6
	ta.MinHeight = 2
	ta.DynamicHeight = true
	km := textarea.DefaultKeyMap()
	km.InsertNewline = key.NewBinding(key.WithKeys("ctrl+j", "alt+enter", "shift+enter"))
	ta.KeyMap = km
	styleInput(&ta, th)
	ta.SetHeight(2)
	return ta
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.RequestBackgroundColor}
	if m.mode == modeModels {
		cmds = append(cmds, m.fetchModels())
	} else {
		cmds = append(cmds, m.input.Focus())
	}
	for i := range m.tabs {
		if cmd := m.watchTitle(&m.tabs[i]); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.FocusMsg:
		// A terminal search can scroll an older buffer into view and leave
		// the frame painted over it. Focusing again redraws from the origin.
		return m, func() tea.Msg { return tea.ClearScreen() }

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layout()
		m.paint()
		return m, nil

	case tea.BackgroundColorMsg:
		if msg.IsDark() == m.theme.dark {
			return m, nil
		}
		m.theme = newTheme(msg.IsDark())
		styleInput(&m.input, m.theme)
		m.spin.Style = lipgloss.NewStyle().Foreground(m.theme.accentColor)
		m.layout()
		m.paint()
		return m, nil

	case tea.KeyPressMsg:
		return m.onKey(msg)

	case tea.MouseWheelMsg:
		return m.onWheel(msg)

	case tea.PasteMsg:
		return m.onPaste(msg.Content)

	case modelsLoadedMsg:
		return m.onModels(msg)

	case selectDoneMsg:
		return m.onSelected(msg)

	case connectDoneMsg:
		return m.onConnected(msg)

	case openedMsg:
		return m.onOpened(msg)

	case titleMsg:
		return m, m.onTitle(msg)

	case spinner.TickMsg:
		if !m.anyBusy() {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case eventMsg:
		return m, m.onEvents(msg)

	case paintMsg:
		m.paintWaiting = false
		if m.dirty {
			m.paint()
		}
		return m, nil

	case turnDoneMsg:
		return m.finishTurn(msg)
	}

	if m.tab().busy || m.mode != modeChat {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.layout()
	return m, cmd
}

func (m *model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.pending {
		if msg.String() == "ctrl+c" {
			return m.quit()
		}
		return m, nil
	}
	switch m.mode {
	case modeModels, modeProviders, modeSessions:
		return m.onPickerKey(msg)
	case modeConnect:
		return m.onConnectKey(msg)
	case modeChat:
		return m.onChatKey(msg)
	default:
		panic(fmt.Sprintf("unhandled input mode %d", m.mode))
	}
}

func (m *model) onChatKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.tab().busy {
			m.stopTurn(m.tab())
			return m, nil
		}
		return m.quit()
	case "ctrl+d":
		if m.tab().busy || strings.TrimSpace(m.input.Value()) != "" {
			break
		}
		return m.quit()
	case "pgup":
		m.viewport.PageUp()
		return m, nil
	case "pgdown":
		m.viewport.PageDown()
		return m, nil
	case "up":
		if m.moveCommand(-1) {
			return m, nil
		}
	case "down":
		if m.moveCommand(1) {
			return m, nil
		}
	case "enter", "ctrl+m":
		if m.tab().busy {
			return m, nil
		}
		m.acceptCommand()
		return m, m.submit()
	case "tab":
		return m.completeCommand()
	}

	if m.tab().busy {
		return m, nil
	}
	m.stopComplete()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.layout()
	return m, cmd
}

func (m *model) moveCommand(delta int) bool {
	if m.tab().busy {
		return false
	}
	done := command.Complete(m.input.Value(), m.completeIndex)
	if done.Value == "" {
		return false
	}
	next := done.Index + delta
	if next < 0 || next >= len(done.Matches) {
		return true
	}
	m.completeIndex = next
	m.layout()
	return true
}

func (m *model) completeCommand() (tea.Model, tea.Cmd) {
	if m.tab().busy || !m.acceptCommand() {
		return m, nil
	}
	m.layout()
	return m, nil
}

func (m *model) acceptCommand() bool {
	value := m.input.Value()
	atEnd := m.input.Line() == 0 && !strings.Contains(value, "\n") && m.input.Column() == len([]rune(value))
	if !atEnd {
		return false
	}
	done := command.Complete(value, m.completeIndex)
	if done.Value == "" {
		return false
	}
	m.input.SetValue(done.Value)
	m.completeIndex = 0
	return true
}

func (m *model) stopComplete() {
	m.completeIndex = 0
}

func (m *model) commandOverlay() string {
	if m.mode != modeChat || m.tab().busy {
		return ""
	}
	done := command.Complete(m.input.Value(), m.completeIndex)
	if done.Value == "" {
		return ""
	}
	return strings.Join(m.choiceLines(done.Matches, done.Index), "\n")
}

func (m *model) submit() tea.Cmd {
	m.stopComplete()
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}
	parsed := command.Parse(text)
	switch parsed.Kind {
	case command.KindQuit:
		m.explainResume = true
		_, cmd := m.quit()
		return cmd
	case command.KindModels:
		m.input.Reset()
		if parsed.Arg == "" {
			return m.openPicker()
		}
		return m.selectModel(parsed.Arg, false)
	case command.KindConnect:
		m.input.Reset()
		if parsed.Arg == "" {
			return m.openProviders()
		}
		return m.beginConnect(parsed.Arg)
	case command.KindNew:
		m.input.Reset()
		return m.openSession()
	case command.KindSessions:
		m.input.Reset()
		return m.openSessions()
	case command.KindError:
		m.input.Reset()
		m.addBlock(block{kind: blockError, text: parsed.Err.Error()})
		return nil
	case command.KindChat:
	default:
		panic(fmt.Sprintf("unhandled command kind %d", parsed.Kind))
	}

	if m.session() == nil {
		m.input.Reset()
		m.addBlock(block{kind: blockError, text: "select a model with /models"})
		return nil
	}

	m.input.Reset()
	m.input.Blur()
	spinning := m.anyBusy()
	item := m.tab()
	item.busy = true
	item.state = statusWorking
	item.activity = "thinking"
	item.answered = false
	item.answerOpen = false
	item.reasoningOpen = false
	m.addBlock(block{kind: blockUser, text: text})

	item.turnID++
	turn := item.turnID
	tabID := item.id
	ctx, cancel := context.WithCancel(m.ctx)
	item.cancel = cancel

	ch := make(chan tea.Msg, eventBuffer)
	session := item.session
	go func() {
		defer close(ch)
		answer, err := session.Turn(ctx, text, func(ev agent.Event) {
			ch <- eventMsg{tabID: tabID, id: turn, event: ev}
		})
		ch <- turnDoneMsg{tabID: tabID, id: turn, answer: answer, err: err}
	}()
	item.events = ch
	if spinning {
		return waitEvent(ch)
	}
	return tea.Batch(waitEvent(ch), m.spin.Tick)
}

func (m *model) openSession() tea.Cmd {
	if m.director == nil || !m.director.Ready() || m.session() == nil {
		m.addBlock(block{kind: blockError, text: "select a model with /models"})
		return nil
	}
	director := m.director
	ctx := m.ctx
	return func() tea.Msg {
		session, err := director.Open(ctx)
		return openedMsg{session: session, err: err}
	}
}

func (m *model) openSessions() tea.Cmd {
	if m.sessionCount() == 0 {
		m.addBlock(block{kind: blockError, text: "select a model with /models"})
		return nil
	}
	m.mode = modeSessions
	m.filter = ""
	m.cursor = m.focusedChoice()
	m.pickerErr = ""
	m.layout()
	m.paint()
	return nil
}

func (m *model) onOpened(msg openedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.addBlock(block{kind: blockError, text: msg.err.Error()})
		return m, m.input.Focus()
	}
	m.adoptSession(msg.session)
	return m, tea.Batch(m.input.Focus(), m.watchTitle(m.tab()))
}

func (m *model) onTitle(msg titleMsg) tea.Cmd {
	item := m.tabByID(msg.tabID)
	if item == nil || item.titleCh == nil {
		return nil
	}
	if msg.title != "" && msg.title != item.title {
		item.title = msg.title
	}
	return waitTitle(item.id, item.titleCh)
}

func (m *model) finishTurn(msg turnDoneMsg) (tea.Model, tea.Cmd) {
	item := m.tabByID(msg.tabID)
	if item == nil {
		return m, nil
	}
	if msg.id != item.turnID {
		if item.busy {
			return m, waitEvent(item.events)
		}
		return m, nil
	}
	m.stopTurn(item)
	if m.ctx.Err() != nil {
		m.exitErr = m.ctx.Err()
		m.quitting = true
		return m, tea.Quit
	}

	item.busy = false
	item.activity = ""
	item.answerOpen = false
	showedReasoning := item.reasoningOpen
	item.reasoningOpen = false
	switch {
	case msg.err == nil:
		item.state = statusDone
		if !item.answered && !showedReasoning && strings.TrimSpace(msg.answer) != "" {
			item.blocks = append(item.blocks, block{kind: blockAssistant, text: msg.answer})
		}
	case errors.Is(msg.err, context.Canceled), errors.Is(msg.err, context.DeadlineExceeded):
		item.state = statusInterrupted
		item.blocks = append(item.blocks, block{kind: blockNote, text: "interrupted"})
	default:
		item.state = statusError
		item.blocks = append(item.blocks, block{kind: blockError, text: msg.err.Error()})
	}
	item.answered = false
	if item.id != m.tab().id {
		return m, nil
	}
	m.layout()
	m.paint()
	return m, m.input.Focus()
}

func (m *model) onEvents(msg eventMsg) tea.Cmd {
	item := m.tabByID(msg.tabID)
	if item == nil {
		return nil
	}
	if !m.applyEvent(item, msg) {
		if item.busy {
			return waitEvent(item.events)
		}
		return nil
	}
	focused := item.id == m.tab().id
	for {
		select {
		case next, ok := <-item.events:
			if !ok {
				if focused {
					m.paint()
				}
				return nil
			}
			switch next := next.(type) {
			case eventMsg:
				m.applyEvent(item, next)
			case turnDoneMsg:
				_, cmd := m.finishTurn(next)
				return cmd
			default:
				panic(fmt.Sprintf("unhandled turn message %T", next))
			}
		default:
			if !focused {
				return waitEvent(item.events)
			}
			return tea.Batch(m.schedulePaint(), waitEvent(item.events))
		}
	}
}

func (m *model) applyEvent(item *tab, msg eventMsg) bool {
	if msg.id != item.turnID {
		return false
	}
	switch ev := msg.event.(type) {
	case agent.ReasoningDelta:
		m.appendReasoning(item, ev.Text)
	case agent.AnswerDelta:
		m.appendAnswer(item, ev.Text)
	case agent.ToolStart:
		item.answerOpen = false
		item.reasoningOpen = false
		item.activity = strings.TrimSpace(toolLabel(ev.Name) + " " + ev.Detail)
		item.blocks = append(item.blocks, block{
			kind:   blockTool,
			tool:   ev.Name,
			detail: ev.Detail,
			toolID: ev.ID,
		})
	case agent.ToolDone:
		m.markTool(item, ev)
	default:
		panic(fmt.Sprintf("unhandled agent event %T", msg.event))
	}
	if item.id == m.tab().id {
		m.dirty = true
	}
	return true
}

func (m *model) appendAnswer(item *tab, text string) {
	item.activity = "answering"
	item.answered = true
	item.reasoningOpen = false
	last := len(item.blocks) - 1
	if item.answerOpen && last >= 0 && item.blocks[last].kind == blockAssistant {
		item.blocks[last].text += text
		return
	}
	item.answerOpen = true
	item.blocks = append(item.blocks, block{kind: blockAssistant, text: text})
}

func (m *model) appendReasoning(item *tab, text string) {
	item.activity = "thinking"
	last := len(item.blocks) - 1
	if item.reasoningOpen && last >= 0 && item.blocks[last].kind == blockReasoning {
		item.blocks[last].text += text
		return
	}
	item.reasoningOpen = true
	item.blocks = append(item.blocks, block{kind: blockReasoning, text: text})
}

func (m *model) markTool(item *tab, ev agent.ToolDone) {
	for i := range item.blocks {
		if item.blocks[i].kind == blockTool && item.blocks[i].toolID == ev.ID {
			item.blocks[i].done = true
			item.blocks[i].failed = ev.Err != nil
			return
		}
	}
}

func (m *model) schedulePaint() tea.Cmd {
	if !m.dirty {
		return nil
	}
	if m.lastPaint.IsZero() || time.Since(m.lastPaint) >= paintGap {
		m.paint()
		return nil
	}
	if m.paintWaiting {
		return nil
	}
	m.paintWaiting = true
	wait := paintGap - time.Since(m.lastPaint)
	return tea.Tick(wait, func(time.Time) tea.Msg { return paintMsg{} })
}

func (m *model) quit() (tea.Model, tea.Cmd) {
	for i := range m.tabs {
		m.stopTurn(&m.tabs[i])
	}
	m.quitting = true
	return m, tea.Quit
}

func (m *model) addBlock(b block) {
	item := m.tab()
	item.blocks = append(item.blocks, b)
	m.paint()
}

func (m *model) onWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeModels, modeProviders, modeSessions:
		m.wheelPicker(msg.Button)
		return m, nil
	case modeConnect:
		return m, nil
	case modeChat:
		return m.wheelChat(msg)
	default:
		panic(fmt.Sprintf("unhandled input mode %d", m.mode))
	}
}

func (m *model) wheelPicker(button tea.MouseButton) {
	switch wheelLines(button) {
	case -1:
		if m.cursor > 0 {
			m.cursor--
		}
	case 1:
		if m.cursor+1 < m.choiceCount() {
			m.cursor++
		}
	}
}

func (m *model) wheelChat(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	lines := wheelLines(msg.Button)
	if lines == 0 {
		return m, nil
	}
	if m.pointerOverInput(msg.Y) && !m.tab().busy {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.layout()
		return m, cmd
	}
	if lines < 0 {
		m.viewport.ScrollUp(-lines)
	} else {
		m.viewport.ScrollDown(lines)
	}
	return m, nil
}

func wheelLines(button tea.MouseButton) int {
	switch button {
	case tea.MouseWheelUp:
		return -1
	case tea.MouseWheelDown:
		return 1
	default:
		return 0
	}
}

// pointerOverInput reports whether y is on the composer rather than the transcript.
func (m *model) pointerOverInput(y int) bool {
	top := lipgloss.Height(m.headerLine()) + m.viewport.Height()
	if extra := m.overlayView(); extra != "" {
		top += lipgloss.Height(extra)
	}
	return y >= top
}

func (m *model) paint() {
	m.paintFollow(m.viewport.AtBottom())
}

func (m *model) paintFollow(follow bool) {
	m.dirty = false
	m.lastPaint = time.Now()
	col := m.transcriptColumnWidth()
	m.viewport.SetWidth(col)
	m.viewport.SetContent(m.renderTranscript())
	if m.transcriptOverflows() && col > 1 {
		m.viewport.SetWidth(col - 1)
		m.viewport.SetContent(m.renderTranscript())
	}
	if follow {
		m.viewport.GotoBottom()
	}
}

func (m *model) layout() {
	width := m.usableWidth()
	height := m.height
	if height <= 0 {
		height = 24
	}
	inner := width - inputChrome(false, m.theme).GetHorizontalFrameSize()
	if inner < 1 {
		inner = 1
	}
	m.input.SetWidth(inner)
	inputH := lipgloss.Height(m.inputView())
	overlayH := 0
	if extra := m.overlayView(); extra != "" {
		overlayH = lipgloss.Height(extra)
	}
	const chrome = 4 // header, rule, status, help
	vpH := height - chrome - inputH - overlayH
	if vpH < 1 {
		vpH = 1
	}
	follow := m.viewport.AtBottom()
	m.viewport.SetHeight(vpH)
	m.viewport.SetWidth(m.transcriptWidth())
	if follow {
		m.viewport.GotoBottom()
	}
}

func (m *model) transcriptOverflows() bool {
	return m.viewport.Height() > 0 && m.viewport.TotalLineCount() > m.viewport.Height()
}

func (m *model) transcriptColumnWidth() int {
	width := m.usableWidth()
	if m.showPanel() {
		width -= panelWidth
	}
	if width < 1 {
		return 1
	}
	return width
}

func (m *model) transcriptWidth() int {
	width := m.transcriptColumnWidth()
	if m.transcriptOverflows() && width > 1 {
		return width - 1
	}
	return width
}

func (m *model) usableWidth() int {
	if m.width > 0 {
		return m.width
	}
	return 80
}

func (m *model) contentWidth() int {
	if w := m.viewport.Width(); w > 0 {
		return w
	}
	return m.usableWidth()
}

func (m *model) View() tea.View {
	if m.quitting {
		v := tea.NewView("")
		v.ProgramStatus = &tea.ProgramStatus{State: tea.ProgramStateDone, App: "co"}
		return v
	}

	parts := []string{m.headerLine(), m.transcriptView()}
	if extra := m.overlayView(); extra != "" {
		parts = append(parts, extra)
	}
	parts = append(parts, m.inputView(), m.statusLine(), m.helpLine())
	body := lipgloss.JoinVertical(lipgloss.Left, parts...)
	v := tea.NewView(body)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.ReportFocus = true
	v.WindowTitle = "co"
	v.ProgramStatus = m.programStatus()
	return v
}

func (m *model) programStatus() *tea.ProgramStatus {
	if m.anyBusy() {
		message := "working"
		if m.tab().busy {
			message = m.tab().activity
			if message == "" {
				message = "thinking"
			}
		}
		return &tea.ProgramStatus{
			State:   tea.ProgramStateWorking,
			App:     "co",
			Title:   "co",
			Message: message,
		}
	}
	return &tea.ProgramStatus{
		State:   tea.ProgramStateBlocked,
		Kind:    tea.ProgramStatusKindQuestion,
		App:     "co",
		Title:   "co",
		Message: "waiting for a prompt",
	}
}

func waitEvent(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// Run starts the interactive UI on the alternate screen.
// When no model is selected yet, the model picker is open and esc quits.
func Run(ctx context.Context, app Director, cwd string, clearScrollback bool) error {
	modelName := ""
	var session *agent.Session
	if app != nil && app.Ready() {
		modelName = app.Model()
		session = app.Session()
	}
	m := newModel(ctx, session, modelName, cwd)
	m.director = app
	if app == nil || !app.Ready() {
		m.mode = modeModels
		m.startup = true
		m.layout()
	}
	if clearScrollback && term.IsTerminal(os.Stdout.Fd()) {
		forgetScrollback(os.Stdout, os.Getenv("TERM_PROGRAM"))
	}
	final, err := tea.NewProgram(
		m,
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	).Run()
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	ui, ok := final.(*model)
	if ok {
		ui.announceExit()
	}
	if ok && ui.exitErr != nil {
		return ui.exitErr
	}
	return nil
}

// forgetScrollback drops the terminal scrollback before the alternate screen
// is entered. iTerm's Find searches that buffer and, on a hit from before this
// process, scrolls it into view underneath the fullscreen frame.
func forgetScrollback(w io.Writer, termProgram string) {
	_, _ = io.WriteString(w, ansi.EraseEntireDisplay)
	if termProgram == "iTerm.app" {
		_, _ = io.WriteString(w, "\x1b]1337;ClearScrollback\a")
	}
}

func (m *model) announceExit() {
	if !m.explainResume || m.director == nil {
		return
	}
	note := m.director.ExitNote()
	if note == "" {
		return
	}
	fmt.Fprintln(os.Stderr, note)
}
