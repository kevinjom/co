package ui

import (
	"fmt"
	"image/color"
	"os"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type theme struct {
	dark        bool
	accent      lipgloss.Style
	dim         lipgloss.Style
	bad         lipgloss.Style
	accentColor color.Color
	blurBorder  color.Color
	faint       color.Color
}

func newTheme(dark bool) theme {
	accent := pickColor(dark, "#0F766E", "#5EEAD4")
	faint := pickColor(dark, "#71717A", "#A1A1AA")
	bad := pickColor(dark, "#BE123C", "#FB7185")
	blur := pickColor(dark, "#D4D4D8", "#3F3F46")
	return theme{
		dark:        dark,
		accent:      lipgloss.NewStyle().Foreground(accent),
		dim:         lipgloss.NewStyle().Foreground(faint),
		bad:         lipgloss.NewStyle().Foreground(bad),
		accentColor: accent,
		blurBorder:  blur,
		faint:       faint,
	}
}

func pickColor(dark bool, light, night string) color.Color {
	if dark {
		return lipgloss.Color(night)
	}
	return lipgloss.Color(light)
}

func styleInput(ta *textarea.Model, th theme) {
	styles := textarea.DefaultStyles(th.dark)
	styles.Focused.Base = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.accentColor).
		Padding(0, 1)
	styles.Blurred.Base = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.blurBorder).
		Padding(0, 1)
	placeholder := lipgloss.NewStyle().Foreground(th.faint)
	styles.Focused.Placeholder = placeholder
	styles.Blurred.Placeholder = placeholder
	ta.SetStyles(styles)
}

func (m *model) headerLine() string {
	left := m.theme.accent.Render("co") + m.theme.dim.Render("  "+m.modelName)
	right := m.theme.dim.Render(shortPath(m.cwd))
	return joinSides(m.usableWidth(), left, right)
}

func (m *model) statusLine() string {
	var left string
	switch {
	case m.busy:
		label := m.status
		if label == "" {
			label = "thinking"
		}
		left = m.spin.View() + label
	case !m.viewport.AtBottom():
		left = m.theme.dim.Render("pgdn to follow")
	default:
		left = m.theme.dim.Render("read-only")
	}
	right := m.theme.dim.Render(m.modelName)
	return joinSides(m.usableWidth(), left, right)
}

func (m *model) helpLine() string {
	text := "enter send  ·  ctrl+j newline  ·  pgup/pgdn scroll  ·  ctrl+c quit"
	if m.busy {
		text = "ctrl+c stop  ·  pgup/pgdn scroll"
	}
	width := m.usableWidth()
	if width > 0 && lipgloss.Width(text) > width {
		text = ansi.Truncate(text, width, "…")
	}
	return m.theme.dim.Render(text)
}

func (m *model) renderTranscript() string {
	width := m.contentWidth()
	if len(m.blocks) == 0 {
		return renderWelcome(width, m.theme)
	}
	parts := make([]string, len(m.blocks))
	for i, block := range m.blocks {
		parts[i] = renderBlock(block, width, m.theme)
	}
	return strings.Join(parts, "\n\n") + "\n"
}

func renderWelcome(width int, th theme) string {
	text := "Ask about this directory. Tools are read-only.\n\nenter sends  ·  ctrl+j adds a newline"
	return th.dim.Render(wrapText(text, width))
}

func renderBlock(block block, width int, th theme) string {
	switch block.kind {
	case blockUser:
		return hang(th.accent.Render("❯ "), block.text, width)
	case blockTool:
		detail := strings.TrimPrefix(block.text, "tool ")
		return th.dim.Render(hang("· ", detail, width))
	case blockAssistant:
		return wrapText(strings.TrimRight(block.text, "\n"), width)
	case blockError:
		return th.bad.Render(wrapText(block.text, width))
	case blockNote:
		return th.dim.Render(block.text)
	default:
		panic(fmt.Sprintf("unknown block kind %d", block.kind))
	}
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
