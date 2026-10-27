package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
	return func() tea.Msg {
		err := director.Select(ctx, ref)
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
	m.session = m.director.Session()
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
	return m, m.input.Focus()
}

func (m *model) beginConnect(id string) tea.Cmd {
	if m.director == nil {
		m.addBlock(block{kind: blockError, text: "connect is unavailable"})
		return nil
	}
	if err := m.director.CanConnect(id); err != nil {
		m.addBlock(block{kind: blockError, text: err.Error()})
		return nil
	}
	m.mode = modeConnect
	m.connectID = id
	m.secret = ""
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
		if m.session == nil {
			return m.quit()
		}
		m.mode = modeChat
		m.filter = ""
		m.pickerErr = ""
		m.layout()
		m.paint()
		return m, m.input.Focus()
	case "enter", "ctrl+m":
		ids := m.visibleModels()
		if len(ids) == 0 {
			m.pickerErr = "no matching model"
			m.layout()
			m.paint()
			return m, nil
		}
		if m.cursor >= len(ids) {
			m.cursor = 0
		}
		return m, m.selectModel(ids[m.cursor], true)
	case "up":
		if m.cursor > 0 {
			m.cursor--
			m.paint()
		}
		return m, nil
	case "down":
		if m.cursor+1 < len(m.visibleModels()) {
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
	case modeModels:
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
		if m.busy {
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(tea.PasteMsg{Content: text})
		m.layout()
		return m, cmd
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
	m.mode = modeChat
	m.pickerErr = ""
	m.addBlock(block{kind: blockNote, text: "saved key for " + m.connectID})
	m.layout()
	m.paint()
	return m, m.input.Focus()
}

func (m *model) visibleModels() []string {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	if query == "" {
		return m.modelIDs
	}
	out := []string{}
	for _, id := range m.modelIDs {
		if strings.Contains(strings.ToLower(id), query) {
			out = append(out, id)
		}
	}
	return out
}

func (m *model) inputView() string {
	switch m.mode {
	case modeChat:
		return m.input.View()
	case modeModels:
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
		return ""
	case modeModels:
		return m.modelsOverlay()
	case modeConnect:
		if m.pickerErr == "" {
			return ""
		}
		return m.theme.bad.Render(ansi.Truncate(m.pickerErr, m.usableWidth(), "…"))
	default:
		panic(fmt.Sprintf("unhandled input mode %d", m.mode))
	}
}

func (m *model) modelsOverlay() string {
	width := m.usableWidth()
	lines := []string{}
	if m.pickerErr != "" {
		lines = append(lines, m.theme.bad.Render(ansi.Truncate(m.pickerErr, width, "…")))
	}
	ids := m.visibleModels()
	switch {
	case !m.modelsLoaded:
		lines = append(lines, m.theme.dim.Render("loading models"))
	case len(ids) == 0:
		lines = append(lines, m.theme.dim.Render("no matching model"))
	default:
		start, view := pickerWindowSlice(ids, m.cursor)
		for i, id := range view {
			idx := start + i
			if idx == m.cursor {
				line := ansi.Truncate("> "+id, width, "…")
				lines = append(lines, m.theme.accent.Render(line))
				continue
			}
			line := ansi.Truncate("  "+id, width, "…")
			lines = append(lines, m.theme.dim.Render(line))
		}
	}
	return strings.Join(lines, "\n")
}

func pickerWindowSlice(ids []string, cursor int) (int, []string) {
	if len(ids) <= pickerWindow {
		return 0, ids
	}
	start := cursor - pickerWindow/2
	if start < 0 {
		start = 0
	}
	if start+pickerWindow > len(ids) {
		start = len(ids) - pickerWindow
	}
	return start, ids[start : start+pickerWindow]
}

func typedText(msg tea.KeyPressMsg) (string, bool) {
	if msg.Mod != 0 || msg.Text == "" || strings.ContainsAny(msg.Text, "\r\n\t") {
		return "", false
	}
	return msg.Text, true
}
