package agent

import (
	"fmt"
	"strings"

	"github.com/kevinjom/co/internal/llm"
)

// Stats counts one session.
// Token fields come from provider usage on kept assistant messages.
// A title request is not one of those messages.
type Stats struct {
	UserTurns        int
	AssistantAnswers int
	ToolCalls        int
	Input            int
	Output           int
	LatestInput      int
	// Requests is the number of kept assistant messages, one per model request.
	Requests int
	// Reported is how many of those requests included provider usage.
	Reported int
	// LatestKnown is set when the latest kept model request reported usage.
	LatestKnown bool
}

// Stats counts completed messages in the session.
// Messages still in flight, such as an answer being streamed, are not included.
// A resumed session counts the messages loaded from its transcript.
func (s *Session) Stats() Stats {
	var st Stats
	if s == nil {
		return st
	}
	s.msgMu.Lock()
	defer s.msgMu.Unlock()
	var latest *llm.Message
	for i := range s.messages {
		msg := &s.messages[i]
		switch msg.Role {
		case "user":
			st.UserTurns++
		case "assistant":
			st.Requests++
			if strings.TrimSpace(msg.Content) != "" {
				st.AssistantAnswers++
			}
			if msg.Usage != nil {
				st.Reported++
				st.Input += msg.Usage.Input
				st.Output += msg.Usage.Output
			}
			latest = msg
		case "tool":
			st.ToolCalls++
		}
	}
	if latest != nil && latest.Usage != nil {
		st.LatestKnown = true
		st.LatestInput = latest.Usage.Input
	}
	return st
}

// Text is the block /stats prints.
// Token lines are unknown when no request reported usage.
// A mix of reported and missing usage is marked incomplete.
func (st Stats) Text() string {
	return strings.Join([]string{
		fmt.Sprintf("user turns: %d", st.UserTurns),
		fmt.Sprintf("assistant answers: %d", st.AssistantAnswers),
		fmt.Sprintf("tool calls: %d", st.ToolCalls),
		"tokens: " + st.tokens(),
		"context: " + st.context(),
	}, "\n")
}

func (st Stats) tokens() string {
	if st.Reported == 0 {
		return "unknown"
	}
	line := fmt.Sprintf("%d in, %d out", st.Input, st.Output)
	if st.Reported != st.Requests {
		line += " (incomplete)"
	}
	return line
}

func (st Stats) context() string {
	if !st.LatestKnown {
		return "unknown"
	}
	return fmt.Sprintf("%d in", st.LatestInput)
}
