package ui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/kevinjom/co/internal/agent"
	"github.com/kevinjom/co/internal/transcript"
)

const newSessionTitle = "New session"

type sessionStatus int

const (
	statusNew sessionStatus = iota
	statusWorking
	statusDone
	statusInterrupted
	statusError
)

func (s sessionStatus) label() string {
	switch s {
	case statusNew:
		return "new"
	case statusWorking:
		return "working"
	case statusDone:
		return "done"
	case statusInterrupted:
		return "interrupted"
	case statusError:
		return "error"
	default:
		return ""
	}
}

func statusMark(s sessionStatus) string {
	switch s {
	case statusNew:
		return "○"
	case statusWorking:
		return "•"
	case statusDone:
		return "●"
	case statusInterrupted:
		return "–"
	case statusError:
		return "✕"
	default:
		return " "
	}
}

// tab is one open session and the turn the TUI is showing for it.
type tab struct {
	id            int
	session       *agent.Session
	blocks        []block
	busy          bool
	activity      string
	state         sessionStatus
	title         string
	turnID        int
	cancel        context.CancelFunc
	events        <-chan tea.Msg
	draft         string
	offset        int
	follow        bool
	answered      bool
	answerOpen    bool
	reasoningOpen bool
}

func newTab(id int, session *agent.Session) tab {
	item := tab{
		id:      id,
		session: session,
		blocks:  historyBlocks(session),
		title:   newSessionTitle,
		state:   statusNew,
		follow:  true,
	}
	if title := sessionTitle(session); title != "" {
		item.title = title
		item.state = statusDone
	}
	return item
}

func sessionTitle(session *agent.Session) string {
	if session == nil {
		return ""
	}
	for _, msg := range session.Messages() {
		if msg.Role != "user" {
			continue
		}
		return transcript.FallbackTitle(msg.Content)
	}
	return ""
}

func (m *model) tab() *tab {
	return &m.tabs[m.focus]
}

func (m *model) session() *agent.Session {
	if len(m.tabs) == 0 {
		return nil
	}
	return m.tab().session
}

func (m *model) tabByID(id int) *tab {
	for i := range m.tabs {
		if m.tabs[i].id == id {
			return &m.tabs[i]
		}
	}
	return nil
}

func (m *model) anyBusy() bool {
	for i := range m.tabs {
		if m.tabs[i].busy {
			return true
		}
	}
	return false
}

func (m *model) sessionCount() int {
	count := 0
	for i := range m.tabs {
		if m.tabs[i].session != nil {
			count++
		}
	}
	return count
}

func (m *model) stopTurn(item *tab) {
	if item == nil || item.cancel == nil {
		return
	}
	item.cancel()
	item.cancel = nil
}

func (m *model) switchTo(index int) {
	if index < 0 || index >= len(m.tabs) || index == m.focus {
		return
	}
	current := m.tab()
	current.draft = m.input.Value()
	current.offset = m.viewport.YOffset()
	current.follow = m.viewport.AtBottom()
	m.focus = index
	next := m.tab()
	m.input.SetValue(next.draft)
	m.layout()
	m.paintFollow(next.follow)
	if !next.follow {
		m.viewport.SetYOffset(next.offset)
	}
}

func (item *tab) nameFromPrompt(text string) {
	if item.title != newSessionTitle {
		return
	}
	if title := transcript.FallbackTitle(text); title != "" {
		item.title = title
	}
}

func (m *model) sessionChoices() []int {
	query := strings.ToLower(strings.TrimSpace(m.filter))
	out := []int{}
	for i := range m.tabs {
		if m.tabs[i].session == nil {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(m.tabs[i].title), query) {
			continue
		}
		out = append(out, i)
	}
	return out
}

func (m *model) focusedChoice() int {
	for i, index := range m.sessionChoices() {
		if index == m.focus {
			return i
		}
	}
	return 0
}

func (m *model) adoptSession(session *agent.Session) {
	if m.tab().session == nil {
		item := newTab(m.tab().id, session)
		item.blocks = append(m.tab().blocks, item.blocks...)
		*m.tab() = item
		m.layout()
		m.paint()
		return
	}
	m.tabs = append(m.tabs, newTab(m.nextTab, session))
	m.nextTab++
	m.switchTo(len(m.tabs) - 1)
}
