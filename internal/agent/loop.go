// Package agent runs one read-only tool loop against a chat model.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/kevinjom/co/internal/config"
	"github.com/kevinjom/co/internal/openai"
	"github.com/kevinjom/co/internal/tools"
)

// Recorder stores completed messages. A nil recorder leaves history off.
type Recorder interface {
	Record(msg openai.Message, interrupted bool) error
}

// Chatter is one OpenAI-compatible chat endpoint.
type Chatter interface {
	ChatStream(
		ctx context.Context,
		model string,
		messages []openai.Message,
		tools []openai.Tool,
		onChunk func(openai.StreamKind, string),
	) (openai.Message, error)
}

var _ Chatter = (*openai.Client)(nil)

// Session is one in-memory conversation.
type Session struct {
	client        Chatter
	model         string
	tools         *tools.Set
	defs          []openai.Tool
	messages      []openai.Message
	recorder      Recorder
	maxToolRounds int
	titleWG       sync.WaitGroup
	titleMu       sync.Mutex
	title         string
	onTitle       func(string)
}

// NewSession starts a conversation rooted at cwd.
// A nil recorder does not write history.
func NewSession(client Chatter, model, cwd string, recorder Recorder) *Session {
	set := tools.New(cwd)
	return &Session{
		client:        client,
		model:         model,
		tools:         set,
		defs:          toolDefinitions(set),
		recorder:      recorder,
		maxToolRounds: config.DefaultMaxToolRounds,
		messages: []openai.Message{
			{Role: "system", Content: systemPrompt(cwd)},
		},
	}
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
func (s *Session) SetModel(client Chatter, model string) {
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
func (s *Session) Restore(messages []openai.Message) {
	s.messages = append(s.messages, messages...)
}

// Messages returns a copy of the conversation, including the system prompt.
func (s *Session) Messages() []openai.Message {
	out := make([]openai.Message, len(s.messages))
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
		s.remember(openai.Message{Role: "user", Content: line}, false)
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

func (s *Session) complete(ctx context.Context, defs []openai.Tool, observe Observer) (openai.Message, error) {
	var reasoning strings.Builder
	msg, err := s.client.ChatStream(ctx, s.model, s.messages, defs, func(kind openai.StreamKind, text string) {
		switch kind {
		case openai.StreamContent:
			emit(observe, AnswerDelta{Text: text})
		case openai.StreamReasoning:
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
func answerText(msg openai.Message, reasoning string) string {
	if strings.TrimSpace(msg.Content) != "" || len(msg.ToolCalls) > 0 {
		return msg.Content
	}
	return reasoning
}

// keepPartial stores a canceled answer so the next turn can see what the user already saw.
// Any other error leaves the session unchanged apart from the user line.
func (s *Session) keepPartial(msg openai.Message, err error) (string, error) {
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
func (s *Session) remember(msg openai.Message, interrupted bool) {
	s.messages = append(s.messages, msg)
	if s.recorder == nil {
		return
	}
	if err := s.recorder.Record(msg, interrupted); err != nil {
		slog.Warn("session history disabled", "err", err)
		s.recorder = nil
	}
}

func (s *Session) runTools(ctx context.Context, calls []openai.ToolCall, observe Observer) {
	for _, call := range calls {
		emit(observe, ToolStart{
			ID:     call.ID,
			Name:   call.Function.Name,
			Detail: ToolDetail(call),
		})
		result, toolErr := s.tools.Run(ctx, call.Function.Name, map[string]any(call.Function.Arguments))
		emit(observe, ToolDone{ID: call.ID, Err: toolErr})
		s.remember(openai.Message{
			Role:       "tool",
			Content:    result,
			ToolCallID: call.ID,
		}, false)
	}
}

func hasUserMessage(messages []openai.Message) bool {
	for _, msg := range messages {
		if msg.Role == "user" {
			return true
		}
	}
	return false
}

func systemPrompt(cwd string) string {
	lines := []string{
		"You are co, a read-only coding assistant.",
		"The working directory is " + cwd + ".",
		"Answer questions about this codebase using tools.",
		"Do not invent file contents or line counts.",
		"If a tool returns an error, say so.",
		"When you have enough information, reply in plain text and stop calling tools.",
	}
	return strings.Join(lines, "\n")
}

func toolDefinitions(set *tools.Set) []openai.Tool {
	schemas := set.Schemas()
	defs := make([]openai.Tool, 0, len(schemas))
	for _, schema := range schemas {
		defs = append(defs, openai.Tool{
			Type: "function",
			Function: openai.ToolSpec{
				Name:        schema.Name,
				Description: schema.Description,
				Parameters:  schema.Parameters,
			},
		})
	}
	return defs
}

// ToolDetail is the short argument summary shown for a tool call.
func ToolDetail(call openai.ToolCall) string {
	args := call.Function.Arguments
	if args == nil {
		args = openai.Arguments{}
	}

	switch call.Function.Name {
	case "grep":
		pattern, _ := args["pattern"].(string)
		return fmt.Sprintf("pattern=%q", pattern)
	default:
		path, _ := args["path"].(string)
		if path == "" {
			path = "."
		}
		return "path=" + path
	}
}
