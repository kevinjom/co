// Package agent runs one tool loop against a chat model.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/kevinjom/co/internal/config"
	"github.com/kevinjom/co/internal/llm"
	"github.com/kevinjom/co/internal/skill"
	"github.com/kevinjom/co/internal/tools"
)

// Recorder stores completed messages. A nil recorder leaves history off.
// RecordSkillUse stores a skill named by the message most recently recorded.
type Recorder interface {
	Record(msg llm.Message, interrupted bool) error
	RecordSkillUse(name, dir, body string) error
}

// Session is one in-memory conversation.
type Session struct {
	client        llm.Chatter
	model         string
	cwd           string
	tools         *tools.Set
	defs          []llm.Tool
	messages      []llm.Message
	recorder      Recorder
	maxToolRounds int
	catalog       *skill.Catalog
	attached      [][]skill.Skill
	lsp           bool
	titleWG       sync.WaitGroup
	titleMu       sync.Mutex
	title         string
	onTitle       func(string)
}

// NewSession starts a conversation rooted at cwd.
// A nil recorder does not write history.
func NewSession(client llm.Chatter, model, cwd string, recorder Recorder) *Session {
	set := tools.New(cwd)
	return &Session{
		client:        client,
		model:         model,
		cwd:           cwd,
		tools:         set,
		defs:          toolDefinitions(set),
		recorder:      recorder,
		maxToolRounds: config.DefaultMaxToolRounds,
		attached:      [][]skill.Skill{nil},
		messages: []llm.Message{
			{
				Role:    "system",
				Content: systemPrompt(cwd, false, nil, set.Search()),
			},
		},
	}
}

// EnableLSP adds the read-only language-server tools and tells the model to prefer them.
// A nil service is ignored. The server is not started here.
func (s *Session) EnableLSP(lang tools.Language) {
	if lang == nil {
		return
	}
	s.tools.EnableLSP(lang)
	s.defs = toolDefinitions(s.tools)
	s.lsp = true
	s.refreshPrompt()
}

// SetMaxToolRounds sets how many model calls in a turn may include tools.
// A value below 1 is ignored.
func (s *Session) SetMaxToolRounds(n int) {
	if n < 1 {
		return
	}
	s.maxToolRounds = n
}

// SetModel points later turns at a different endpoint and wire model id.
// The message list is left as it is.
func (s *Session) SetModel(client llm.Chatter, model string) {
	s.client = client
	s.model = model
}

// Model is the wire model id for later turns.
func (s *Session) Model() string {
	return s.model
}

// DropRecorder turns history off for the rest of the process.
func (s *Session) DropRecorder() {
	s.recorder = nil
}

// Restore appends messages from an earlier run after the system prompt.
// Those messages are not written again.
func (s *Session) Restore(messages []llm.Message) {
	s.messages = append(s.messages, messages...)
	for range messages {
		s.attached = append(s.attached, nil)
	}
}

// BindSkills attaches skills to messages restored onto a fresh session.
// skills[i] belongs to restored message i. The stored message text is unchanged.
func (s *Session) BindSkills(skills [][]skill.Skill) {
	if s == nil || len(skills) == 0 || len(s.attached) != len(skills)+1 {
		return
	}
	for i, group := range skills {
		s.attached[i+1] = group
	}
}

// Messages returns a copy of the conversation, including the system prompt.
func (s *Session) Messages() []llm.Message {
	out := make([]llm.Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// Turn appends the user line and runs tools until the model answers.
// On a model error the user line stays in the session.
// When the turn is canceled after some text has arrived, that partial answer is kept too.
func (s *Session) Turn(ctx context.Context, userText string, observe Observer) (string, error) {
	return s.Reply(ctx, []string{userText}, observe)
}

// Reply appends each line as its own user message, then runs one tool loop.
// The model answers the lines once. An empty list does not call the model.
// On a model error the lines stay in the session.
func (s *Session) Reply(ctx context.Context, lines []string, observe Observer) (string, error) {
	if len(lines) == 0 {
		return "", nil
	}
	first := !hasUserMessage(s.messages)
	for _, line := range lines {
		s.keep(llm.Message{Role: "user", Content: line}, false, s.skillsOn(line))
	}
	if first {
		s.storeFallbackTitle(lines[0])
		s.refineTitle(lines[0])
	}
	return s.converse(ctx, observe)
}

func (s *Session) converse(ctx context.Context, observe Observer) (string, error) {
	for range s.maxToolRounds {
		msg, err := s.complete(ctx, s.defs, observe)
		if err != nil {
			return s.keepPartial(msg, err)
		}
		s.remember(msg, false)
		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}
		s.runTools(ctx, msg.ToolCalls, observe)
	}

	msg, err := s.complete(ctx, nil, observe)
	if err != nil {
		return s.keepPartial(msg, err)
	}
	s.remember(msg, false)
	return msg.Content, nil
}

func (s *Session) complete(ctx context.Context, defs []llm.Tool, observe Observer) (llm.Message, error) {
	var reasoning strings.Builder
	msg, err := s.client.ChatStream(ctx, s.model, s.wireMessages(), defs, func(kind llm.StreamKind, text string) {
		switch kind {
		case llm.StreamContent:
			emit(observe, AnswerDelta{Text: text})
		case llm.StreamReasoning:
			reasoning.WriteString(text)
			emit(observe, ReasoningDelta{Text: text})
		default:
			panic(fmt.Sprintf("unhandled stream kind %d", kind))
		}
	})
	msg.Content = answerText(msg, reasoning.String())
	return msg, err
}

// answerText keeps a reasoning-only reply. A tool call stays content-free.
func answerText(msg llm.Message, reasoning string) string {
	if strings.TrimSpace(msg.Content) != "" || len(msg.ToolCalls) > 0 {
		return msg.Content
	}
	return reasoning
}

// keepPartial stores a canceled answer so the next turn can see what the user already saw.
// Any other error leaves the session unchanged apart from the user line.
func (s *Session) keepPartial(msg llm.Message, err error) (string, error) {
	stopped := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	if !stopped || msg.Content == "" {
		return "", err
	}
	msg.ToolCalls = nil
	if msg.Role == "" {
		msg.Role = "assistant"
	}
	s.remember(msg, true)
	return msg.Content, err
}

// remember keeps a finished message in the session and, when history is on, in the transcript.
// A write failure disables history for the rest of the process. The turn itself still succeeds.
func (s *Session) remember(msg llm.Message, interrupted bool) {
	s.keep(msg, interrupted, nil)
}

// keep stores msg and any skills that line named.
// The skill text is not part of msg. wireMessages appends it when calling the model.
func (s *Session) keep(msg llm.Message, interrupted bool, skills []skill.Skill) {
	s.messages = append(s.messages, msg)
	s.attached = append(s.attached, skills)
	if s.recorder == nil {
		return
	}
	if err := s.recorder.Record(msg, interrupted); err != nil {
		slog.Warn("session history disabled", "err", err)
		s.recorder = nil
		return
	}
	for _, sk := range skills {
		if err := s.recorder.RecordSkillUse(sk.Name, sk.Dir, sk.Body); err != nil {
			slog.Warn("session history disabled", "err", err)
			s.recorder = nil
			return
		}
	}
}

// wireMessages is the chat request. A stored user line that named a skill
// is copied with that skill's text appended, so later turns send the same bytes.
func (s *Session) wireMessages() []llm.Message {
	out := make([]llm.Message, len(s.messages))
	copy(out, s.messages)
	for i := range out {
		if i >= len(s.attached) {
			break
		}
		out[i].Content = withSkills(out[i].Content, s.attached[i])
	}
	return out
}

func (s *Session) runTools(ctx context.Context, calls []llm.ToolCall, observe Observer) {
	for _, call := range calls {
		emit(observe, ToolStart{
			ID:     call.ID,
			Name:   call.Function.Name,
			Detail: tools.Detail(call.Function.Name, map[string]any(call.Function.Arguments)),
		})
		result, toolErr := s.tools.Run(ctx, call.Function.Name, map[string]any(call.Function.Arguments))
		emit(observe, ToolDone{ID: call.ID, Output: result, Err: toolErr})
		s.remember(llm.Message{
			Role:       "tool",
			Content:    result,
			ToolCallID: call.ID,
		}, false)
	}
}

func hasUserMessage(messages []llm.Message) bool {
	for _, msg := range messages {
		if msg.Role == "user" {
			return true
		}
	}
	return false
}

func toolDefinitions(set *tools.Set) []llm.Tool {
	schemas := set.Schemas()
	defs := make([]llm.Tool, 0, len(schemas))
	for _, schema := range schemas {
		defs = append(defs, llm.Tool{
			Type: "function",
			Function: llm.ToolSpec{
				Name:        schema.Name,
				Description: schema.Description,
				Parameters:  schema.Parameters,
			},
		})
	}
	return defs
}
