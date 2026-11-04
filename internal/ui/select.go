package ui

import (
	"io"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

type selectPhase int

const (
	selectNone selectPhase = iota
	selectDrag
	selectKept
)

// textSelection is a drag across the rendered frame, in terminal cells.
// The two points are inclusive. A press and release on the same cell is
// not a selection.
type textSelection struct {
	phase  selectPhase
	ax, ay int
	bx, by int
}

func (m *model) clearSelection() {
	m.sel = textSelection{}
	m.copied = false
}

func (m *model) onClick(msg tea.Mouse) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	x, y := max(msg.X, 0), max(msg.Y, 0)
	m.copied = false
	m.sel = textSelection{phase: selectDrag, ax: x, ay: y, bx: x, by: y}
	return m, nil
}

func (m *model) onDrag(msg tea.Mouse) (tea.Model, tea.Cmd) {
	if m.sel.phase != selectDrag {
		return m, nil
	}
	m.sel.bx = max(msg.X, 0)
	m.sel.by = max(msg.Y, 0)
	return m, nil
}

func (m *model) onRelease(msg tea.Mouse) (tea.Model, tea.Cmd) {
	if m.sel.phase != selectDrag {
		return m, nil
	}
	m.sel.bx = max(msg.X, 0)
	m.sel.by = max(msg.Y, 0)
	m.sel.phase = selectKept
	text, ok := m.selectionText()
	if !ok {
		m.clearSelection()
		return m, nil
	}
	m.copied = true
	return m, copyClipboard(text)
}

func (m *model) selectionText() (string, bool) {
	return selectedString(m.frame(), m.usableWidth(), m.viewHeight(), m.sel)
}

// copyClipboard writes text where paste can read it.
// OSC 52 is the sequence a remote terminal understands. pbcopy covers
// macOS terminals that ignore that sequence, including Terminal.app.
func copyClipboard(text string) tea.Cmd {
	return tea.Batch(tea.SetClipboard(text), writePasteboard(text))
}

func writePasteboard(text string) tea.Cmd {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		_ = cmd.Run()
		return nil
	}
}

func selectionBounds(sel textSelection) (x1, y1, x2, y2 int, ok bool) {
	if sel.phase == selectNone {
		return 0, 0, 0, 0, false
	}
	x1, y1 = sel.ax, sel.ay
	x2, y2 = sel.bx, sel.by
	if y1 > y2 || (y1 == y2 && x1 > x2) {
		x1, y1, x2, y2 = x2, y2, x1, y1
	}
	if x1 == x2 && y1 == y2 {
		return 0, 0, 0, 0, false
	}
	return x1, y1, x2, y2, true
}

func selectedString(content string, width, height int, sel textSelection) (string, bool) {
	x1, y1, x2, y2, ok := selectionBounds(sel)
	if !ok {
		return "", false
	}
	buf := screenOf(content, width, height)
	rows := []string{}
	for y := y1; y <= y2 && y < buf.Height(); y++ {
		line := buf.Line(y)
		start, end := lineCols(y, y1, x1, y2, x2, len(line))
		start = snapWide(line, start)
		rows = append(rows, strings.TrimRight(rowText(line, start, end), " "))
	}
	for len(rows) > 0 && rows[0] == "" {
		rows = rows[1:]
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	text := strings.Join(rows, "\n")
	if strings.TrimSpace(text) == "" {
		return "", false
	}
	return text, true
}

func highlightSelection(content string, width, height int, sel textSelection) string {
	x1, y1, x2, y2, ok := selectionBounds(sel)
	if !ok {
		return content
	}
	buf := screenOf(content, width, height)
	marked := false
	for y := y1; y <= y2 && y < buf.Height(); y++ {
		line := buf.Line(y)
		start, end := lineCols(y, y1, x1, y2, x2, len(line))
		start = snapWide(line, start)
		for x := start; x < end; x++ {
			cell := line.At(x)
			if cell == nil || cell.IsZero() || cell.Width == 0 {
				continue
			}
			cell.Style.Attrs |= uv.AttrReverse
			marked = true
		}
	}
	if !marked {
		return content
	}
	return buf.Render()
}

func screenOf(content string, width, height int) uv.ScreenBuffer {
	if w := lipgloss.Width(content); w > width {
		width = w
	}
	if h := lipgloss.Height(content); h > height {
		height = h
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	buf := uv.NewScreenBuffer(width, height)
	uv.NewStyledString(content).Draw(&buf, uv.Rect(0, 0, width, height))
	return buf
}

func lineCols(y, y1, x1, y2, x2, width int) (start, end int) {
	start = 0
	end = width
	if y == y1 {
		start = min(max(x1, 0), width)
	}
	if y == y2 {
		end = min(max(x2+1, 0), width)
	}
	if start > end {
		return end, end
	}
	return start, end
}

func snapWide(line uv.Line, x int) int {
	for x > 0 {
		cell := line.At(x)
		if cell == nil || cell.Width != 0 {
			return x
		}
		x--
	}
	return x
}

func rowText(line uv.Line, start, end int) string {
	var b strings.Builder
	for x := start; x < end; x++ {
		cell := line.At(x)
		if cell == nil || cell.IsZero() || cell.Width == 0 {
			continue
		}
		b.WriteString(cell.Content)
	}
	return b.String()
}
