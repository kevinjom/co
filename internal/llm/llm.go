// Package llm holds chat transcript types and the streaming chat port.
package llm

import "context"

// Compat selects optional OpenAI-compatible fields.
// ReasoningEffort sends reasoning_effort on each chat request.
// ReasoningDelta reads the reasoning field from stream chunks.
type Compat struct {
	ReasoningEffort bool
	ReasoningDelta  bool
}

// StreamKind says which part of a chunk just arrived.
type StreamKind int

const (
	// StreamContent is assistant text that should be shown as the answer.
	StreamContent StreamKind = iota
	// StreamReasoning is the model's thinking trace.
	// Some models, including Qwen, stream the whole reply here and leave content empty.
	StreamReasoning
)

// Chatter is one streaming chat endpoint.
type Chatter interface {
	ChatStream(
		ctx context.Context,
		model string,
		messages []Message,
		tools []Tool,
		onChunk func(StreamKind, string),
	) (Message, error)
}
