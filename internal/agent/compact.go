package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kevinjom/co/internal/llm"
)

// SummaryRole is a stored summary of the turns before it.
// The screen shows it as a summary. A later model request receives it as a
// user message, and omits the turns it replaced. Those turns stay in the session.
const SummaryRole = "summary"

const compactInstruction = "Summarize the conversation so far so it can continue without the earlier messages.\n" +
	"Keep the user's goal, decisions, files changed, and facts from tool results that later work depends on.\n" +
	"Keep what is still unfinished.\n" +
	"Write plain prose. Do not call tools."

const summaryPrefix = "The conversation so far was summarized. Continue from this summary.\n\n"

// Compact asks the current model to summarize the conversation it is using.
// The summary is stored after the existing messages. Those messages stay in
// the session. Later requests send the system prompt, the summary, and
// messages stored after the summary.
// The request that asks for the summary is not stored.
// A failed or canceled request leaves the session unchanged.
// Compact runs between turns. An open tool exchange is refused.
func (s *Session) Compact(ctx context.Context) (string, error) {
	if s == nil || s.client == nil {
		return "", errors.New("nothing to compact")
	}
	request, err := s.compactRequest()
	if err != nil {
		return "", err
	}
	var reasoning strings.Builder
	msg, err := s.client.ChatStream(ctx, s.model, request, nil, func(kind llm.StreamKind, text string) {
		switch kind {
		case llm.StreamContent, llm.StreamReasoning:
			// The finished message carries the summary. Only reasoning is collected here.
			if kind == llm.StreamReasoning {
				reasoning.WriteString(text)
			}
		default:
			panic(fmt.Sprintf("unhandled stream kind %d", kind))
		}
	})
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(msg.Content)
	if text == "" {
		text = strings.TrimSpace(reasoning.String())
	}
	if text == "" {
		return "", errors.New("empty summary")
	}
	s.remember(llm.Message{
		Role:    SummaryRole,
		Content: text,
		Usage:   msg.Usage,
	}, false)
	return text, nil
}

// compactRequest is the summary call. The instruction is the last message
// and is not part of the session.
func (s *Session) compactRequest() ([]llm.Message, error) {
	s.msgMu.Lock()
	defer s.msgMu.Unlock()
	if !s.compactableLocked() {
		return nil, errors.New("nothing to compact")
	}
	wire := s.wireLocked()
	if toolRoundOpen(wire) {
		return nil, errors.New("wait for the turn to finish")
	}
	request := make([]llm.Message, len(wire)+1)
	copy(request, wire)
	request[len(wire)] = llm.Message{Role: "user", Content: compactInstruction}
	return request, nil
}

// compactableLocked reports that a turn since the latest summary is stored.
// s.msgMu is held by the caller.
func (s *Session) compactableLocked() bool {
	start := 0
	for i := len(s.messages) - 1; i >= 0; i-- {
		if s.messages[i].Role == SummaryRole {
			start = i + 1
			break
		}
	}
	for _, msg := range s.messages[start:] {
		switch msg.Role {
		case "user", "assistant", "tool":
			return true
		}
	}
	return false
}

// toolRoundOpen reports an assistant tool call that has no tool result yet.
func toolRoundOpen(messages []llm.Message) bool {
	pending := 0
	for _, msg := range messages {
		switch msg.Role {
		case "assistant":
			if pending > 0 {
				return true
			}
			pending = len(msg.ToolCalls)
		case "tool":
			if pending > 0 {
				pending--
			}
		}
	}
	return pending > 0
}
