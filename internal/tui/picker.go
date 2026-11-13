package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kevinjom/co/internal/command"
	palette "github.com/kevinjom/co/internal/theme"
)

const pickerWindow = 8

type modelsLoadedMsg struct {
	ids []string
	err error
}

type selectDoneMsg struct {
	err        error
	fromPicker bool
}

type connectDoneMsg struct {
	err error
}

func (m *model) openPicker() tea.Cmd {
	if m.director == nil {
		m.addBlock(block{kind: blockError, text: "models are unavailable"})
		return nil
	}
	m.mode = modeModels
	m.filter = ""
	m.cursor = 0
	m.modelIDs = nil
	m.modelsLoaded = false
	m.pickerErr = ""
	m.layout()
	m.paint()
	return m.fetchModels()
}

// returnToModels shows the model list again.
// The picker is the only screen until a session exists, so leaving /connect
// comes back here instead of an empty composer.
func (m *model) returnToModels() tea.Cmd {
	if !m.modelsLoaded {
		return m.openPicker()
	}
	m.mode = modeModels
	m.filter = ""
	m.cursor = 0
	m.pickerErr = ""
	m.layout()
	m.paint()
	return nil
}

// beginSlash opens the composer on "/" so command completion can run.
// Until a session exists the model picker owns every key, which blocks /connect.
func (m *model) beginSlash() (tea.Model, tea.Cmd) {
	m.mode = modeChat
	m.filter = ""
	m.pickerErr = ""
	m.completeIndex = 0
	setCaret(&m.input, "/", 1)
	m.layout()
	m.paint()
	return m, m.input.Focus()
}

// takeFilterCommand runs a slash command typed into the model picker.
// A pasted "/connect gemini" would otherwise be a filter with no matches.
func (m *model) takeFilterCommand() (bool, tea.Cmd) {
	if m.mode != modeModels || m.session() != nil {
		return false, nil
	}
	text := strings.TrimSpace(m.filter)
	if !strings.HasPrefix(text, "/") {
		return false, nil
	}
	parsed := command.Parse(text, m.skillNames())
	switch parsed.Kind {
	case command.KindChat:
		return false, nil
	case command.KindError:
		m.pickerErr = parsed.Err.Error()
		m.layout()
		m.paint()
		return true, nil
	case command.KindQuit, command.KindModels, command.KindConnect, command.KindNew,
		command.KindSessions, command.KindTheme, command.KindSkill, command.KindStats:
		m.filter = ""
		m.pickerErr = ""
		m.mode = modeChat
		m.input.SetValue(text)
		return true, m.submit()
	default:
		panic(fmt.Sprintf("unhandled command kind %d", parsed.Kind))
	}
}

func (m *model) fetchModels() tea.Cmd {
	if m.director == nil {
		return func() tea.Msg {
			return modelsLoadedMsg{err: errors.New("models are unavailable")}
		}
	}
	director := m.director
	ctx := m.ctx
	return func() tea.Msg {
		ids, err := director.Models(ctx)
		if ids == nil {
			ids = []string{}
		}
		return modelsLoadedMsg{ids: ids, err: err}
	}
}

func (m *model) onModels(msg modelsLoadedMsg) (tea.Model, tea.Cmd) {
	m.modelIDs = msg.ids
	m.modelsLoaded = true
	// /connect can open before this list arrives. Keep that screen as it is.
	if m.mode != modeModels {
		return m, nil
	}
	m.cursor = 0
	m.pickerErr = ""
	if msg.err != nil {
		m.pickerErr = msg.err.Error()
	}
	m.layout()
	m.paint()
	return m, nil
}

func (m *model) selectModel(ref string, fromPicker bool) tea.Cmd {
	if m.director == nil {
		m.addBlock(block{kind: blockError, text: "models are unavailable"})
		return nil
	}
	m.pending = true
	m.pickerErr = ""
	director := m.director
	ctx := m.ctx
	session := m.session()
	return func() tea.Msg {
		err := director.Select(ctx, ref, session)
		return selectDoneMsg{err: err, fromPicker: fromPicker}
	}
}

func (m *model) onSelected(msg selectDoneMsg) (tea.Model, tea.Cmd) {
	m.pending = false
	if msg.err != nil {
		if msg.fromPicker || m.mode == modeModels {
			m.pickerErr = msg.err.Error()
			m.layout()
			m.paint()
			return m, nil
		}
		m.addBlock(block{kind: blockError, text: msg.err.Error()})
		return m, m.input.Focus()
	}
	m.modelName = m.director.Model()
	if m.session() == nil {
		m.adoptSession(m.director.Session())
	}
	first := m.startup
	m.mode = modeChat
	m.startup = false
	m.filter = ""
	m.pickerErr = ""
	if !first {
		m.addBlock(block{kind: blockNote, text: "model " + m.modelName})
	}
	m.layout()
	m.paint()
	return m, tea.Batch(m.input.Focus(), m.watchTitle(m.tab()))
}

func (m *model) openThemes() tea.Cmd {
	m.mode = modeThemes
	m.filter = ""
	m.pickerErr = ""
	m.cursor = 0
	for i, name := range palette.Names() {
		if name == m.theme.name {
			m.cursor = i
			break
		}
	}
	m.layout()
	m.paint()
	return nil
}

func (m *model) useTheme(name string) tea.Cmd {
	if err := m.applyTheme(name, m.theme.dark); err != nil {
		if m.mode == modeThemes {
			m.pickerErr = err.Error()
			m.layout()
			m.paint()
			return nil
		}
		m.addBlock(block{kind: blockError, text: err.Error()})
		return nil
	}
	var saveErr error
	if m.director != nil {
		saveErr = m.director.SaveTheme(m.theme.name)
	}
	m.filter = ""
	m.pickerErr = ""
	m.addBlock(block{kind: blockNote, text: "theme " + m.theme.name})
	if saveErr != nil {
		m.addBlock(block{kind: blockError, text: saveErr.Error()})
	}
	if m.session() == nil {
		return m.returnToModels()
	}
	m.mode = modeChat
	m.layout()
	m.paint()
	return m.input.Focus()
}

func (m *model) openSkills() tea.Cmd {
	m.mode = modeSkills
	m.filter = ""
	m.pickerErr = ""
	m.cursor = 0
	m.layout()
	m.paint()
	return nil
}

func (m *model) insertSkillName(name string) tea.Cmd {
	m.mode = modeChat
	m.filter = ""
	m.pickerErr = ""
	next := "/" + name + " "
	setCaret(&m.input, next, len([]rune(next)))
	m.layout()
	m.paint()
	return m.input.Focus()
}

func (m *model) openProviders() tea.Cmd {
	if m.director == nil {
		m.addBlock(block{kind: blockError, text: "connect is unavailable"})
		return nil
	}
	m.mode = modeProviders
	m.filter = ""
	m.cursor = 0
	m.providerIDs = m.director.Providers()
	m.pickerErr = ""
	m.layout()
	m.paint()
	return nil
}

func (m *model) beginConnect(id string) tea.Cmd {
	if m.director == nil {
		m.addBlock(block{kind: blockError, text: "connect is unavailable"})
		return nil
	}
	if err := m.director.CanConnect(id); err != nil {
		if m.mode == modeProviders {
			m.pickerErr = err.Error()
			m.layout()
			m.paint()
			return nil
		}
		m.addBlock(block{kind: blockError, text: err.Error()})
		return nil
	}
	m.mode = modeConnect
	m.connectID = id
	m.secret = ""
	m.filter = ""
	m.pickerErr = ""
	m.layout()
	m.paint()
	return nil
}

func (m *model) onPickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		if m.session() == nil {
			if m.mode == modeModels {
				return m.quit()
			}
			m.filter = ""
			m.pickerErr = ""
			return m, m.returnToModels()
		}
		m.mode = modeChat
		m.filter = ""
		m.pickerErr = ""
		m.layout()
		m.paint()
		return m, m.input.Focus()
	case "enter", "ctrl+m":
		if handled, cmd := m.takeFilterCommand(); handled {
			return m, cmd
		}
		if m.mode == modeSessions {
			return m.chooseSession()
		}
		ids := m.pickerIDs()
		if len(ids) == 0 {
			m.pickerErr = m.emptyListText()
			m.layout()
			m.paint()
			return m, nil
		}
		if m.cursor >= len(ids) {
			m.cursor = 0
		}
		switch m.mode {
		case modeModels:
			return m, m.selectModel(ids[m.cursor], true)
		case modeProviders:
			return m, m.beginConnect(ids[m.cursor])
		case modeThemes:
			return m, m.useTheme(ids[m.cursor])
		case modeSkills:
			return m, m.insertSkillName(ids[m.cursor])
		default:
			panic(fmt.Sprintf("unhandled input mode %d", m.mode))
		}
	case "up":
		if m.cursor > 0 {
			m.cursor--
			m.paint()
		}
		return m, nil
	case "down":
		if m.cursor+1 < m.choiceCount() {
			m.cursor++
			m.paint()
		}
		return m, nil
	case "pgup":
		m.viewport.PageUp()
		return m, nil
	case "pgdown":
		m.viewport.PageDown()
		return m, nil
	case "backspace":
		runes := []rune(m.filter)
		if len(runes) > 0 {
			m.filter = string(runes[:len(runes)-1])
			m.cursor = 0
			m.pickerErr = ""
			m.layout()
			m.paint()
		}
		return m, nil
	}

	text, ok := typedText(msg)
	if !ok {
		return m, nil
	}
	if m.mode == modeModels && m.session() == nil && m.filter == "" && text == "/" {
		return m.beginSlash()
	}
	m.filter += text
	m.cursor = 0
	m.pickerErr = ""
	m.layout()
	m.paint()
	return m, nil
}

func (m *model) onConnectKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.secret = ""
		return m.quit()
	case "esc":
		m.secret = ""
		m.pickerErr = ""
		if m.session() == nil {
			return m, m.returnToModels()
		}
		m.mode = modeChat
		m.layout()
		m.paint()
		return m, m.input.Focus()
	case "enter", "ctrl+m":
		if strings.TrimSpace(m.secret) == "" {
			m.pickerErr = "api key is empty"
			m.layout()
			m.paint()
			return m, nil
		}
		m.pending = true
		secret := m.secret
		m.secret = ""
		id := m.connectID
		director := m.director
		return m, func() tea.Msg {
			return connectDoneMsg{err: director.Connect(id, secret)}
		}
	case "backspace":
		runes := []rune(m.secret)
		if len(runes) > 0 {
			m.secret = string(runes[:len(runes)-1])
			m.paint()
		}
		return m, nil
	}

	text, ok := typedText(msg)
	if !ok && strings.ContainsAny(msg.Text, "\r\n") {
		text = sanitizeSecret(msg.Text)
		ok = text != ""
	}
	if !ok {
		return m, nil
	}
	m.secret += text
	m.pickerErr = ""
	m.paint()
	return m, nil
}

func (m *model) onPaste(text string) (tea.Model, tea.Cmd) {
	if m.pending {
		return m, nil
	}
	switch m.mode {
	case modeConnect:
		text = sanitizeSecret(text)
		if text == "" {
			return m, nil
		}
		m.secret += text
		m.pickerErr = ""
		m.paint()
		return m, nil
	case modeModels, modeProviders, modeSessions, modeThemes, modeSkills:
		line, _, _ := strings.Cut(text, "\n")
		line = strings.TrimSpace(line)
		if line == "" {
			return m, nil
		}
		m.filter += line
		m.cursor = 0
		m.pickerErr = ""
		m.layout()
		m.paint()
		return m, nil
	case modeChat:
		m.stopComplete()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(tea.PasteMsg{Content: text})
		m.layout()
		return m, tea.Batch(cmd, m.trackFiles())
	default:
		panic(fmt.Sprintf("unhandled input mode %d", m.mode))
	}
}

func sanitizeSecret(text string) string {
	text = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, text)
	return strings.TrimSpace(text)
}

func (m *model) onConnected(msg connectDoneMsg) (tea.Model, tea.Cmd) {
	m.pending = false
	m.secret = ""
	if msg.err != nil {
		m.pickerErr = msg.err.Error()
		m.layout()
		m.paint()
		return m, nil
	}
	m.pickerErr = ""
	m.addBlock(block{kind: blockNote, text: "saved key for " + m.connectID})
	if m.session() == nil {
		return m, m.returnToModels()
	}
	m.mode = modeChat
	m.layout()
	m.paint()
	return m, m.input.Focus()
}

func (m *model) chooseSession() (tea.Model, tea.Cmd) {
	choices := m.sessionChoices()
	if len(choices) == 0 {
		m.pickerErr = "no matching session"
		m.layout()
		m.paint()
		return m, nil
	}
	if m.cursor >= len(choices) {
		m.cursor = 0
	}
	m.switchTo(choices[m.cursor])
	m.mode = modeChat
	m.filter = ""
	m.pickerErr = ""
	m.layout()
	m.paint()
	return m, m.input.Focus()
}

func (m *model) choiceCount() int {
	if m.mode == modeSessions {
		return len(m.sessionChoices())
	}
	return len(m.pickerIDs())
}

func (m *model) pickerIDs() []string {
	var ids []string
	switch m.mode {
	case modeModels:
		ids = m.modelIDs
	case modeProviders:
		ids = m.providerIDs
	case modeThemes:
		ids = palette.Names()
	case modeSkills:
		ids = m.skillNames()
	default:
		return []string{}
	}
	return filterIDs(ids, m.filter)
}

func filterIDs(ids []string, filter string) []string {
	query := strings.ToLower(strings.TrimSpace(filter))
	if query == "" {
		return ids
	}
	out := []string{}
	for _, id := range ids {
		if strings.Contains(strings.ToLower(id), query) {
			out = append(out, id)
		}
	}
	return out
}

func (m *model) emptyListText() string {
	switch m.mode {
	case modeProviders:
		if len(m.providerIDs) == 0 {
			return "no providers accept an API key"
		}
		return "no matching provider"
	case modeThemes:
		return "no matching theme"
	case modeSkills:
		if len(m.skillNames()) == 0 {
			return "no skills"
		}
		return "no matching skill"
	default:
		return "no matching model"
	}
}

func (m *model) inputView() string {
	switch m.mode {
	case modeChat:
		return inputChrome(m.input.Focused(), m.theme).Render(m.input.View())
	case modeModels, modeProviders, modeSessions, modeThemes, modeSkills:
		return m.theme.dim.Render(ansi.Truncate("filter: "+m.filter, m.usableWidth(), "…"))
	case modeConnect:
		dots := strings.Repeat("•", len([]rune(m.secret)))
		text := "key for " + m.connectID + ": " + dots
		return m.theme.dim.Render(ansi.Truncate(text, m.usableWidth(), "…"))
	default:
		panic(fmt.Sprintf("unhandled input mode %d", m.mode))
	}
}

func (m *model) overlayView() string {
	switch m.mode {
	case modeChat:
		if extra := m.commandOverlay(); extra != "" {
			return extra
		}
		return m.fileOverlay()
	case modeModels:
		return m.choiceOverlay(!m.modelsLoaded, "loading models")
	case modeProviders:
		return m.choiceOverlay(false, "")
	case modeSessions:
		return m.sessionOverlay()
	case modeThemes:
		return m.choiceOverlay(false, "")
	case modeSkills:
		return m.choiceOverlay(false, "")
	case modeConnect:
		if m.pickerErr == "" {
			return ""
		}
		return m.theme.bad.Render(ansi.Truncate(m.pickerErr, m.usableWidth(), "…"))
	default:
		panic(fmt.Sprintf("unhandled input mode %d", m.mode))
	}
}

func (m *model) choiceOverlay(loading bool, loadingText string) string {
	width := m.usableWidth()
	lines := []string{}
	if m.pickerErr != "" {
		lines = append(lines, m.theme.bad.Render(ansi.Truncate(m.pickerErr, width, "…")))
	}
	ids := m.pickerIDs()
	switch {
	case loading:
		lines = append(lines, m.theme.dim.Render(loadingText))
	case len(ids) == 0:
		lines = append(lines, m.theme.dim.Render(m.emptyListText()))
	default:
		lines = append(lines, m.choiceLines(ids, m.cursor)...)
	}
	return strings.Join(lines, "\n")
}

func (m *model) sessionOverlay() string {
	width := m.usableWidth()
	lines := []string{}
	if m.pickerErr != "" {
		lines = append(lines, m.theme.bad.Render(ansi.Truncate(m.pickerErr, width, "…")))
	}
	choices := m.sessionChoices()
	if len(choices) == 0 {
		lines = append(lines, m.theme.dim.Render("no matching session"))
		return strings.Join(lines, "\n")
	}
	labels := make([]string, len(choices))
	for i, index := range choices {
		labels[i] = m.tabs[index].title
	}
	lines = append(lines, m.choiceLines(labels, m.cursor)...)
	return strings.Join(lines, "\n")
}

func (m *model) choiceLines(ids []string, cursor int) []string {
	width := m.usableWidth()
	start, view := pickerWindowSlice(ids, cursor)
	lines := make([]string, 0, len(view))
	for i, id := range view {
		prefix := "  "
		style := m.theme.dim
		if start+i == cursor {
			prefix = "> "
			style = m.theme.accent
		}
		line := ansi.Truncate(prefix+id, width, "…")
		lines = append(lines, style.Render(line))
	}
	return lines
}

func pickerWindowSlice[T any](items []T, cursor int) (int, []T) {
	if len(items) <= pickerWindow {
		return 0, items
	}
	start := cursor - pickerWindow/2
	if start < 0 {
		start = 0
	}
	if start+pickerWindow > len(items) {
		start = len(items) - pickerWindow
	}
	return start, items[start : start+pickerWindow]
}

func typedText(msg tea.KeyPressMsg) (string, bool) {
	if msg.Mod != 0 || msg.Text == "" || strings.ContainsAny(msg.Text, "\r\n\t") {
		return "", false
	}
	return msg.Text, true
}
