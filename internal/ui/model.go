package ui

import (
	"context"
	"errors"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"co/internal/agent"
)

type blockKind int

const (
	blockUser blockKind = iota
	blockTool
	blockAssistant
	blockError
	blockNote
)

type block struct {
	kind blockKind
	text string
}

type toolMsg struct {
	id   int
	line string
}

type turnDoneMsg struct {
	id     int
	answer string
	err    error
}

// model is the interactive session. The program stays in Bubble Tea's default
// inline mode: View is redrawn in place, and history scrolls inside the viewport.
type model struct {
	session   *agent.Session
	ctx       context.Context
	modelName string
	cwd       string

	theme    theme
	input    textarea.Model
	viewport viewport.Model
	spin     spinner.Model

	width  int
	height int

	blocks []block
	busy   bool
	status string
	turnID int
	cancel context.CancelFunc
	events <-chan tea.Msg

	quitting bool
	exitErr  error
}

func newModel(ctx context.Context, session *agent.Session, modelName, cwd string) *model {
	if ctx == nil {
		ctx = context.Background()
	}
	th := newTheme(true)
	m := &model{
		session:   session,
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
	m.viewport.MouseWheelEnabled = false
	m.spin.Style = lipgloss.NewStyle().Foreground(th.accentColor)
	m.layout()
	m.paint()
	return m
}

func newInput(th theme) textarea.Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.Placeholder = "Ask about this codebase"
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
	return tea.Batch(m.input.Focus(), tea.RequestBackgroundColor)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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

	case spinner.TickMsg:
		if !m.busy {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case toolMsg:
		if msg.id != m.turnID {
			return m, nil
		}
		m.status = strings.TrimPrefix(msg.line, "tool ")
		m.addBlock(block{kind: blockTool, text: msg.line})
		return m, waitEvent(m.events)

	case turnDoneMsg:
		return m.finishTurn(msg)
	}

	if m.busy {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.layout()
	return m, cmd
}

func (m *model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		if m.busy {
			m.cancelTurn()
			return m, nil
		}
		return m.quit()
	case "ctrl+d":
		if m.busy || strings.TrimSpace(m.input.Value()) != "" {
			break
		}
		return m.quit()
	case "pgup":
		m.viewport.PageUp()
		return m, nil
	case "pgdown":
		m.viewport.PageDown()
		return m, nil
	case "enter", "ctrl+m":
		if m.busy {
			return m, nil
		}
		return m, m.submit()
	}

	if m.busy {
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.layout()
	return m, cmd
}

func (m *model) submit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}
	if text == "exit" || text == "quit" {
		_, cmd := m.quit()
		return cmd
	}

	m.input.Reset()
	m.input.Blur()
	m.busy = true
	m.status = "thinking"
	m.addBlock(block{kind: blockUser, text: text})
	m.layout()

	m.turnID++
	id := m.turnID
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel

	ch := make(chan tea.Msg, 8)
	session := m.session
	go func() {
		defer close(ch)
		answer, err := session.Turn(ctx, text, func(line string) {
			ch <- toolMsg{id: id, line: line}
		})
		ch <- turnDoneMsg{id: id, answer: answer, err: err}
	}()
	m.events = ch
	return tea.Batch(waitEvent(ch), m.spin.Tick)
}

func (m *model) finishTurn(msg turnDoneMsg) (tea.Model, tea.Cmd) {
	if msg.id != m.turnID {
		return m, nil
	}
	m.cancelTurn()
	if m.ctx.Err() != nil {
		m.exitErr = m.ctx.Err()
		m.quitting = true
		return m, tea.Quit
	}

	m.busy = false
	m.status = ""
	switch {
	case msg.err == nil && strings.TrimSpace(msg.answer) != "":
		m.addBlock(block{kind: blockAssistant, text: msg.answer})
	case msg.err != nil && errors.Is(msg.err, context.Canceled):
		m.addBlock(block{kind: blockNote, text: "interrupted"})
	case msg.err != nil:
		m.addBlock(block{kind: blockError, text: msg.err.Error()})
	}
	m.layout()
	return m, m.input.Focus()
}

func (m *model) quit() (tea.Model, tea.Cmd) {
	m.cancelTurn()
	m.quitting = true
	return m, tea.Quit
}

func (m *model) cancelTurn() {
	if m.cancel == nil {
		return
	}
	m.cancel()
	m.cancel = nil
}

func (m *model) addBlock(b block) {
	m.blocks = append(m.blocks, b)
	m.paint()
}

func (m *model) paint() {
	follow := m.viewport.AtBottom()
	m.viewport.SetContent(m.renderTranscript())
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
	m.input.SetWidth(width)
	inputH := lipgloss.Height(m.input.View())
	const chrome = 3 // header, status, help
	vpH := height - chrome - inputH
	if vpH < 1 {
		vpH = 1
	}
	follow := m.viewport.AtBottom()
	m.viewport.SetWidth(width)
	m.viewport.SetHeight(vpH)
	if follow {
		m.viewport.GotoBottom()
	}
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

	body := lipgloss.JoinVertical(
		lipgloss.Left,
		m.headerLine(),
		m.viewport.View(),
		m.input.View(),
		m.statusLine(),
		m.helpLine(),
	)
	v := tea.NewView(body)
	v.WindowTitle = "co"
	v.ProgramStatus = m.programStatus()
	return v
}

func (m *model) programStatus() *tea.ProgramStatus {
	if m.busy {
		message := m.status
		if message == "" {
			message = "thinking"
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

// Run starts the interactive UI. Inline mode is Bubble Tea's default.
func Run(ctx context.Context, session *agent.Session, modelName, cwd string) error {
	m := newModel(ctx, session, modelName, cwd)
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
	if ok && ui.exitErr != nil {
		return ui.exitErr
	}
	return nil
}
