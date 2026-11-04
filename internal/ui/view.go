package ui

import (
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func styleInput(ta *textarea.Model, th theme) {
	styles := textarea.DefaultStyles(th.dark)
	placeholder := lipgloss.NewStyle().Foreground(th.faint)
	styles.Focused.Placeholder = placeholder
	styles.Blurred.Placeholder = placeholder
	ta.SetStyles(styles)
}

// inputChrome draws the single prompt box. The border stays off the textarea
// style: bubbles paints that style twice while the placeholder is showing.
func inputChrome(focused bool, th theme) lipgloss.Style {
	ink := th.blurBorder
	if focused {
		ink = th.accentColor
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ink).
		Padding(0, 1)
}

func (m *model) headerLine() string {
	label := m.modelName
	if label == "" {
		label = "no model"
	}
	left := m.theme.accent.Render("co") + m.theme.dim.Render("  "+label)
	right := m.theme.dim.Render(shortPath(m.cwd))
	width := m.usableWidth()
	line := joinSides(width, left, right)
	if width < 1 {
		return line
	}
	return line + "\n" + m.theme.dim.Render(strings.Repeat("─", width))
}

func (m *model) statusLine() string {
	var left string
	switch {
	case m.tab().busy:
		label := m.tab().activity
		if label == "" {
			label = "thinking"
		}
		style := m.theme.accent
		ink := m.theme.accentColor
		if label == "thinking" {
			style = m.theme.warn
			ink = m.theme.warnColor
		}
		m.spin.Style = lipgloss.NewStyle().Foreground(ink)
		left = m.spin.View() + style.Render(label)
	case m.copied:
		left = m.theme.dim.Render("copied")
	case m.mode == modeModels:
		left = m.theme.dim.Render("choose a model")
	case m.mode == modeSessions:
		left = m.theme.dim.Render("choose a session")
	case m.mode == modeProviders:
		left = m.theme.dim.Render("choose a provider")
	case m.mode == modeThemes:
		left = m.theme.dim.Render("choose a theme")
	case m.mode == modeConnect:
		left = m.theme.dim.Render("api key")
	case !m.viewport.AtBottom():
		left = m.theme.dim.Render("pgdn to follow")
	default:
		left = m.theme.dim.Render("read-only")
	}
	right := m.theme.dim.Render(m.modelName)
	return joinSides(m.usableWidth(), left, right)
}

// aboveInputHeight is the command list and unsent queue drawn between the transcript and the box.
func (m *model) aboveInputHeight() int {
	height := 0
	if extra := m.overlayView(); extra != "" {
		height += lipgloss.Height(extra)
	}
	if queue := m.queueView(); queue != "" {
		height += lipgloss.Height(queue)
	}
	return height
}

// queueView lists this session's unsent lines, oldest first, just above the box.
func (m *model) queueView() string {
	if m.mode != modeChat {
		return ""
	}
	lines := m.tab().queue
	if len(lines) == 0 {
		return ""
	}
	limit := m.queueLimit()
	note := ""
	if len(lines) > limit {
		keep := limit
		if limit > 1 {
			keep = limit - 1
			note = fmt.Sprintf("· %d more", len(lines)-keep)
		}
		lines = lines[len(lines)-keep:]
	}
	width := m.usableWidth()
	rows := make([]string, 0, len(lines)+1)
	if note != "" {
		rows = append(rows, m.theme.dim.Render(ansi.Truncate(note, width, "…")))
	}
	for _, line := range lines {
		text := strings.Join(strings.Fields(line), " ")
		row := "· " + text
		if width > 0 {
			row = ansi.Truncate(row, width, "…")
		}
		rows = append(rows, m.theme.dim.Render(row))
	}
	return strings.Join(rows, "\n")
}

func (m *model) queueLimit() int {
	inputH := lipgloss.Height(m.inputView())
	overlayH := 0
	if extra := m.overlayView(); extra != "" {
		overlayH = lipgloss.Height(extra)
	}
	height := m.height
	if height <= 0 {
		height = 24
	}
	const frame = 5 // header, rule, status, help, and one transcript row
	limit := height - frame - inputH - overlayH
	if limit < 1 {
		return 1
	}
	return limit
}

func (m *model) helpLine() string {
	text := "enter send  ·  ctrl+j newline  ·  drag copy  ·  wheel/pgup/pgdn scroll  ·  ctrl+c quit"
	switch {
	case m.tab().busy && strings.TrimSpace(m.input.Value()) != "":
		text = "enter queue  ·  ctrl+c clear"
	case m.tab().busy && len(m.tab().queue) > 0:
		text = "enter queue  ·  esc recall  ·  ctrl+c stop"
	case m.tab().busy:
		text = "enter queue  ·  ctrl+j newline  ·  ctrl+c stop"
	case (m.mode == modeModels || m.mode == modeProviders || m.mode == modeSessions || m.mode == modeThemes) && m.session() == nil:
		text = "enter select  ·  esc quit  ·  up/down move"
	case m.mode == modeModels || m.mode == modeProviders || m.mode == modeSessions || m.mode == modeThemes:
		text = "enter select  ·  esc close  ·  up/down move"
	case m.mode == modeConnect:
		text = "enter save  ·  esc cancel"
	case m.commandOverlay() != "":
		text = "up/down move  ·  tab complete  ·  enter select"
	}
	width := m.usableWidth()
	if width > 0 && lipgloss.Width(text) > width {
		text = ansi.Truncate(text, width, "…")
	}
	return m.theme.dim.Render(text)
}

const (
	panelWidth       = 24
	panelMinTerminal = 60
)

func (m *model) showPanel() bool {
	return m.usableWidth() >= panelMinTerminal && m.sessionCount() > 0
}

func (m *model) transcriptView() string {
	body := m.viewport.View()
	bar := m.scrollBar()
	if bar != "" {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, bar)
	}
	if !m.showPanel() {
		return body
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, body, m.sessionPanel())
}

func (m *model) sessionPanel() string {
	height := m.viewport.Height()
	if height < 1 {
		height = 1
	}
	inner := panelWidth - 1
	rows := make([]string, 0, height)
	rows = append(rows, m.panelLine(m.theme.dim, " sessions", inner))
	indexes := m.panelIndexes(height - 1)
	for _, index := range indexes {
		item := m.tabs[index]
		mark := m.statusIcon(item.state)
		rest := " " + item.title
		room := inner - lipgloss.Width(mark)
		if room < 1 {
			room = 1
		}
		style := m.theme.dim
		if index == m.focus {
			style = m.theme.accent
		}
		text := ansi.Truncate(rest, room, "…")
		gap := room - lipgloss.Width(text)
		if gap > 0 {
			text += strings.Repeat(" ", gap)
		}
		rows = append(rows, m.theme.dim.Render("│")+mark+style.Render(text))
	}
	for len(rows) < height {
		rows = append(rows, m.panelLine(m.theme.dim, "", inner))
	}
	return strings.Join(rows, "\n")
}

func (m *model) statusIcon(state sessionStatus) string {
	if state == statusWorking {
		return m.spin.View()
	}
	mark := statusMark(state)
	switch state {
	case statusDone:
		return m.theme.good.Render(mark)
	case statusInterrupted:
		return m.theme.warn.Render(mark)
	case statusError:
		return m.theme.bad.Render(mark)
	default:
		return m.theme.dim.Render(mark)
	}
}

func (m *model) panelIndexes(height int) []int {
	indexes := []int{}
	for i := range m.tabs {
		if m.tabs[i].session != nil {
			indexes = append(indexes, i)
		}
	}
	if height < 1 {
		return nil
	}
	if len(indexes) <= height {
		return indexes
	}
	pos := 0
	for i, index := range indexes {
		if index == m.focus {
			pos = i
			break
		}
	}
	start := pos - height/2
	if start < 0 {
		start = 0
	}
	if start+height > len(indexes) {
		start = len(indexes) - height
	}
	return indexes[start : start+height]
}

func (m *model) panelLine(style lipgloss.Style, text string, inner int) string {
	text = ansi.Truncate(text, inner, "…")
	gap := inner - lipgloss.Width(text)
	if gap > 0 {
		text += strings.Repeat(" ", gap)
	}
	return m.theme.dim.Render("│") + style.Render(text)
}

func (m *model) scrollBar() string {
	height := m.viewport.Height()
	total := m.viewport.TotalLineCount()
	if height <= 0 || total <= height {
		return ""
	}
	thumb := max(1, height*height/total)
	maxOffset := total - height
	offset := m.viewport.YOffset()
	track := height - thumb
	pos := 0
	if track > 0 && maxOffset > 0 {
		pos = min(track, offset*track/maxOffset)
	}
	lines := make([]string, height)
	for i := range lines {
		if i >= pos && i < pos+thumb {
			lines[i] = m.theme.accent.Render("┃")
		} else {
			lines[i] = m.theme.dim.Render("│")
		}
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderTranscript() string {
	width := m.contentWidth()
	blocks := m.tab().blocks
	if len(blocks) == 0 {
		return renderWelcome(width, m.theme)
	}
	parts := make([]string, len(blocks))
	for i, block := range blocks {
		parts[i] = m.renderBlock(block, width)
	}
	return strings.Join(parts, "\n\n") + "\n"
}

func renderWelcome(width int, th theme) string {
	text := "Ask about this directory. Tools are read-only.\n\nenter sends  ·  ctrl+j adds a newline"
	return th.dim.Render(wrapText(text, width))
}

func (m *model) renderBlock(block block, width int) string {
	th := m.theme
	var out string
	switch block.kind {
	case blockUser:
		out = renderUser(block.text, width, th)
	case blockTool:
		out = renderTool(block, width, th)
	case blockAssistant:
		out = renderInset(m.md.render(th, block.text, insetWidth(width), false), width)
	case blockReasoning:
		out = renderReasoning(m.md.render(th, block.text, insetWidth(width), true), width, th)
	case blockError:
		out = renderError(block.text, width, th)
	case blockNote:
		out = renderInset(th.dim.Render(hang("· ", block.text, insetWidth(width))), width)
	default:
		panic(fmt.Sprintf("unknown block kind %d", block.kind))
	}
	return clampWidth(out, width)
}

func renderUser(text string, width int, th theme) string {
	bar := th.accent.Render("┃ ")
	inner := width - lipgloss.Width(bar)
	if inner < 1 {
		inner = 1
	}
	body := hang(th.accent.Bold(true).Render("❯ "), text, inner)
	return prefixLines(body, bar)
}

func renderReasoning(body string, width int, th theme) string {
	label := th.warn.Bold(true).Render("thinking")
	if body == "" {
		return renderInset(label, width)
	}
	return renderInset(label+"\n"+body, width)
}

func renderTool(block block, width int, th theme) string {
	return renderInset(clampWidth(toolLine(block, th), insetWidth(width)), width)
}

func toolLine(block block, th theme) string {
	glyph := "→"
	mark := th.accent
	name := th.accent.Bold(true)
	detail := th.dim
	switch {
	case block.failed:
		glyph = "✗"
		mark = th.bad
		name = th.bad.Bold(true)
		detail = th.bad
	case block.done:
		glyph = "✓"
		mark = th.good
		name = th.dim
		detail = th.dim
	}
	line := mark.Render(glyph) + " " + name.Render(toolLabel(block.tool))
	if strings.TrimSpace(block.detail) != "" {
		line += "  " + detail.Render(block.detail)
	}
	return line
}

func toolLabel(name string) string {
	switch name {
	case "list_dir":
		return "List"
	case "read_file":
		return "Read"
	case "grep":
		return "Grep"
	default:
		if name == "" {
			return "tool"
		}
		return name
	}
}

func renderError(text string, width int, th theme) string {
	bar := th.bad.Render("┃ ")
	inner := width - lipgloss.Width(bar)
	if inner < 1 {
		inner = 1
	}
	return prefixLines(th.bad.Render(wrapText(text, inner)), bar)
}

func renderInset(text string, width int) string {
	return prefixLines(text, strings.Repeat(" ", inset))
}

const inset = 2

func insetWidth(width int) int {
	if width-inset < 1 {
		return 1
	}
	return width - inset
}

func prefixLines(text, prefix string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func hang(prefix, text string, width int) string {
	prefixWidth := lipgloss.Width(prefix)
	bodyWidth := width - prefixWidth
	if bodyWidth < 1 {
		bodyWidth = 1
	}
	lines := strings.Split(wrapText(text, bodyWidth), "\n")
	pad := strings.Repeat(" ", prefixWidth)
	lines[0] = prefix + lines[0]
	for i := 1; i < len(lines); i++ {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

func wrapText(text string, width int) string {
	if width < 1 {
		return text
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, ansi.Wrap(line, width, " "))
	}
	return strings.Join(out, "\n")
}

func joinSides(width int, left, right string) string {
	if width < 1 {
		return ansi.Strip(left)
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, width, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func shortPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	prefix := home + string(os.PathSeparator)
	if strings.HasPrefix(path, prefix) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
