package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/kevinjom/co/internal/tools"
)

const (
	chatPlaceholder   = "what do you want to cook"
	answerPlaceholder = "add your own words"
	askWindow         = 8
)

// pendingAsk is a question one session is waiting on.
type pendingAsk struct {
	question tools.Question
	picked   []bool
	cursor   int
	reply    chan askResult
	// prior is the composer text from before the question, restored after.
	prior string
}

type askResult struct {
	text string
	err  error
}

type askMsg struct {
	tabID    int
	id       int
	question tools.Question
	reply    chan askResult
}

func (m *model) asker(item *tab, turn int, pipe *turnPipe) tools.Asker {
	return func(ctx context.Context, question tools.Question) (string, error) {
		reply := make(chan askResult, 1)
		pipe.push(askMsg{
			tabID:    item.id,
			id:       turn,
			question: question,
			reply:    reply,
		})
		select {
		case result := <-reply:
			if result.err != nil {
				return "", result.err
			}
			return result.text, nil
		case <-ctx.Done():
			select {
			case result := <-reply:
				if result.err != nil {
					return "", result.err
				}
				return result.text, nil
			default:
				return "", ctx.Err()
			}
		}
	}
}

func (m *model) openAsk(item *tab, msg askMsg) {
	if item == nil {
		deliverAsk(msg.reply, askResult{err: errors.New("turn ended")})
		return
	}
	if msg.id != item.turnID {
		deliverAsk(msg.reply, askResult{err: errors.New("turn ended")})
		return
	}
	if item.ask != nil {
		deliverAsk(item.ask.reply, askResult{err: errors.New("turn ended")})
		item.ask = nil
	}
	prior := item.draft
	focused := item.id == m.tab().id
	if focused {
		prior = m.input.Value()
		m.stopComplete()
		m.input.Reset()
		m.input.Placeholder = answerPlaceholder
	}
	item.draft = ""
	item.ask = &pendingAsk{
		question: msg.question,
		picked:   make([]bool, len(msg.question.Options)),
		reply:    msg.reply,
		prior:    prior,
	}
	item.activity = "Ask " + strings.Join(strings.Fields(msg.question.Text), " ")
	if focused {
		m.layout()
		m.dirty = true
	}
}

func (m *model) closeAsk(item *tab) {
	if item == nil || item.ask == nil {
		return
	}
	prior := item.ask.prior
	item.ask = nil
	item.held = nil
	item.draft = prior
	if item.id == m.tab().id {
		m.input.SetValue(prior)
		m.input.Placeholder = chatPlaceholder
		m.layout()
	}
}

// dismissAsk closes a question because the turn is stopping.
// The previous composer text stays out of the box until the turn ends,
// so a second ctrl+c does not wipe it.
func (m *model) dismissAsk(item *tab) {
	if item == nil || item.ask == nil {
		return
	}
	prior := item.ask.prior
	item.ask = nil
	if item.id != m.tab().id {
		item.draft = prior
		return
	}
	item.held = &prior
	m.input.Reset()
	m.input.Placeholder = chatPlaceholder
	m.layout()
}

func (m *model) restoreHeld(item *tab) {
	if item == nil || item.held == nil {
		return
	}
	text := *item.held
	item.held = nil
	item.draft = text
	if item.id == m.tab().id && m.input.Value() == "" {
		m.input.SetValue(text)
		m.input.Placeholder = chatPlaceholder
	}
}

func (m *model) asking() bool {
	return m.mode == modeChat && m.tab().ask != nil
}

func (m *model) moveAsk(delta int) bool {
	ask := m.tab().ask
	if ask == nil || len(ask.question.Options) == 0 {
		return false
	}
	n := len(ask.question.Options)
	ask.cursor = (ask.cursor + delta + n) % n
	m.layout()
	m.paint()
	return true
}

func (m *model) toggleAsk() bool {
	ask := m.tab().ask
	if ask == nil || len(ask.question.Options) == 0 || m.filePickerArmed() {
		return false
	}
	if strings.TrimSpace(m.input.Value()) != "" {
		return false
	}
	index := ask.cursor
	if index < 0 || index >= len(ask.picked) {
		return false
	}
	if ask.question.Multiple {
		ask.picked[index] = !ask.picked[index]
	} else if ask.picked[index] {
		ask.picked[index] = false
	} else {
		for i := range ask.picked {
			ask.picked[i] = false
		}
		ask.picked[index] = true
	}
	m.paint()
	return true
}

// submitAsk sends the reply when this tab is waiting on a question.
// An empty reply keeps the question open and still consumes the key.
func (m *model) submitAsk() bool {
	item := m.tab()
	ask := item.ask
	if ask == nil {
		return false
	}
	indexes := make([]int, 0, len(ask.picked))
	for i, on := range ask.picked {
		if on {
			indexes = append(indexes, i)
		}
	}
	text, err := tools.FormatAnswer(ask.question.Options, indexes, m.input.Value())
	if err != nil {
		return true
	}
	reply := ask.reply
	m.closeAsk(item)
	deliverAsk(reply, askResult{text: text})
	return true
}

func deliverAsk(reply chan askResult, result askResult) {
	if reply == nil {
		return
	}
	select {
	case reply <- result:
	default:
	}
}

func (m *model) askView() string {
	if m.mode != modeChat {
		return ""
	}
	ask := m.tab().ask
	if ask == nil {
		return ""
	}
	width := m.usableWidth()
	rows := []string{m.theme.accent.Bold(true).Render(truncateAsk(ask.question.Text, width))}
	options := ask.question.Options
	if len(options) == 0 {
		return strings.Join(rows, "\n")
	}
	start := askWindowStart(len(options), ask.cursor, askWindow)
	if start > 0 {
		rows = append(rows, m.theme.dim.Render(truncateAsk(fmt.Sprintf("· %d more", start), width)))
	}
	end := start + askWindow
	if end > len(options) {
		end = len(options)
	}
	for i := start; i < end; i++ {
		mark := "○"
		if ask.picked[i] {
			mark = "●"
		}
		pointer := "  "
		style := m.theme.dim
		if i == ask.cursor {
			pointer = "› "
			style = m.theme.accent
		}
		rows = append(rows, style.Render(truncateAsk(pointer+mark+" "+options[i], width)))
	}
	if end < len(options) {
		rows = append(rows, m.theme.dim.Render(truncateAsk(fmt.Sprintf("· %d more", len(options)-end), width)))
	}
	return strings.Join(rows, "\n")
}

func askWindowStart(n, cursor, limit int) int {
	if n <= limit || limit < 1 {
		return 0
	}
	start := cursor - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > n {
		start = n - limit
	}
	return start
}

func truncateAsk(text string, width int) string {
	text = strings.Join(strings.Fields(text), " ")
	if width < 1 {
		return text
	}
	return ansi.Truncate(text, width, "…")
}

// askHelp is the hint under the composer while a question is open.
func askHelp(ask *pendingAsk) string {
	if ask == nil {
		return ""
	}
	if len(ask.question.Options) == 0 {
		return "enter answer  ·  ctrl+c stop"
	}
	return "up/down move  ·  space select  ·  enter answer  ·  ctrl+c stop"
}
