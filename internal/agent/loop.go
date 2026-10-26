// Package agent runs one read-only tool loop against an Ollama model.
package agent

import (
	"context"
	"fmt"
	"strings"

	"co/internal/ollama"
	"co/internal/tools"
)

// maxToolRounds is how many model calls may include tools.
// If the model is still calling tools after that, one more call is made with no tools.
const maxToolRounds = 8

// Trace receives one line each time a tool runs.
type Trace func(line string)

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
func (s *Session) Turn(ctx context.Context, userText string, trace Trace) (string, error) {
	s.messages = append(s.messages, ollama.Message{
		Role:    "user",
		Content: userText,
	})

	for range maxToolRounds {
		msg, err := s.client.Chat(ctx, s.model, s.messages, s.defs)
		if err != nil {
			return "", err
		}
		s.messages = append(s.messages, msg)
		if len(msg.ToolCalls) == 0 {
			return msg.Content, nil
		}
		s.runTools(ctx, msg.ToolCalls, trace)
	}

	msg, err := s.client.Chat(ctx, s.model, s.messages, nil)
	if err != nil {
		return "", err
	}
	s.messages = append(s.messages, msg)
	return msg.Content, nil
}

func (s *Session) runTools(ctx context.Context, calls []ollama.ToolCall, trace Trace) {
	for _, call := range calls {
		if trace != nil {
			trace(toolTrace(call))
		}
		s.messages = append(s.messages, ollama.Message{
			Role:     "tool",
			Content:  s.tools.Run(ctx, call.Function.Name, call.Function.Arguments),
			ToolName: call.Function.Name,
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

func toolTrace(call ollama.ToolCall) string {
	args := call.Function.Arguments
	if args == nil {
		args = map[string]any{}
	}

	detail := ""
	switch call.Function.Name {
	case "grep":
		pattern, _ := args["pattern"].(string)
		detail = fmt.Sprintf("pattern=%q", pattern)
	default:
		path, _ := args["path"].(string)
		if path == "" {
			path = "."
		}
		detail = "path=" + path
	}
	return "tool " + call.Function.Name + " " + detail
}
