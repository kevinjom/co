package agent

import "fmt"

// Event is one observation from a turn.
// The set is closed: ReasoningDelta, AnswerDelta, ToolStart, and ToolDone.
type Event interface {
	event()
}

// ReasoningDelta is one piece of the model's thinking trace, in order.
type ReasoningDelta struct {
	Text string
}

func (ReasoningDelta) event() {}

// AnswerDelta is one piece of assistant text, in order.
type AnswerDelta struct {
	Text string
}

func (AnswerDelta) event() {}

// ToolStart is emitted before a tool runs.
type ToolStart struct {
	ID     string
	Name   string
	Detail string
}

func (ToolStart) event() {}

// ToolDone is emitted after a tool returns.
// Err is set when the tool failed. The model still receives the failure as text.
type ToolDone struct {
	ID  string
	Err error
}

func (ToolDone) event() {}

// Observer receives events for one turn. A nil observer is ignored.
type Observer func(Event)

func emit(observe Observer, ev Event) {
	if observe == nil || ev == nil {
		return
	}
	switch ev.(type) {
	case ReasoningDelta, AnswerDelta, ToolStart, ToolDone:
		observe(ev)
	default:
		panic(fmt.Sprintf("unhandled agent event %T", ev))
	}
}
