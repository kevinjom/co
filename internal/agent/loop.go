// Package agent runs one read-only tool loop against an Ollama model.
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"co/internal/ollama"
	"co/internal/tools"
)

// maxToolRounds is how many model calls may include tools.
// If the model is still calling tools after that, one more call is made with no tools.
const maxToolRounds = 8

// Session is the in-memory conversation for one process.
type Session struct {
	client   *ollama.Client
	model    string
	tools    *tools.Set
	defs     []ollama.Tool
	messages []ollama.Message
}

// NewSession starts a conversation rooted at cwd.
func NewSession(client *ollama.Client, model, cwd string) *Session {
	set := tools.New(cwd)
	return &Session{
		client: client,
		model:  model,
		tools:  set,
		defs:   toolDefinitions(set),
		messages: []ollama.Message{
			{Role: "system", Content: systemPrompt(cwd)},
		},
	}
}

// Turn appends the user line and runs tools until the model answers.
// On a model error the user line stays in the session.
// When the turn is canceled after some text has arrived, that partial answer is kept too.
func (s *Session) Turn(ctx context.Context, userText string, observe Observer) (string, error) {
	s.messages = append(s.messages, ollama.Message{
		Role:    "user",
		Content: userText,
	})

	for range maxToolRounds {
		msg, err := s.complete(ctx, s.defs, observe)
		if err != nil {
			return s.keepPartial(msg, err)
		}
		s.messages = append(s.messages, msg)
		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}
		s.runTools(ctx, msg.ToolCalls, observe)
	}

	msg, err := s.complete(ctx, nil, observe)
	if err != nil {
		return s.keepPartial(msg, err)
	}
	s.messages = append(s.messages, msg)
	return msg.Content, nil
}

func (s *Session) complete(ctx context.Context, defs []ollama.Tool, observe Observer) (ollama.Message, error) {
	return s.client.ChatStream(ctx, s.model, s.messages, defs, func(delta string) {
		emit(observe, AnswerDelta{Text: delta})
	})
}

// keepPartial stores a canceled answer so the next turn can see what the user already saw.
// Any other error leaves the session unchanged apart from the user line.
func (s *Session) keepPartial(msg ollama.Message, err error) (string, error) {
	stopped := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	if !stopped || msg.Content == "" {
		return "", err
	}
	msg.ToolCalls = nil
	if msg.Role == "" {
		msg.Role = "assistant"
	}
	s.messages = append(s.messages, msg)
	return msg.Content, err
}

func (s *Session) runTools(ctx context.Context, calls []ollama.ToolCall, observe Observer) {
	for _, call := range calls {
		emit(observe, ToolStart{
			ID:     call.ID,
			Name:   call.Function.Name,
			Detail: toolDetail(call),
		})
		result, toolErr := s.tools.Run(ctx, call.Function.Name, map[string]any(call.Function.Arguments))
		emit(observe, ToolDone{ID: call.ID, Err: toolErr})
		s.messages = append(s.messages, ollama.Message{
			Role:       "tool",
			Content:    result,
			ToolCallID: call.ID,
		})
	}
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

func toolDefinitions(set *tools.Set) []ollama.Tool {
	schemas := set.Schemas()
	defs := make([]ollama.Tool, 0, len(schemas))
	for _, schema := range schemas {
		defs = append(defs, ollama.Tool{
			Type: "function",
			Function: ollama.ToolSpec{
				Name:        schema.Name,
				Description: schema.Description,
				Parameters:  schema.Parameters,
			},
		})
	}
	return defs
}

func toolDetail(call ollama.ToolCall) string {
	args := call.Function.Arguments
	if args == nil {
		args = ollama.Arguments{}
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
