package ollama

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ChatStream sends one streaming chat completion.
// onDelta is called for each non-empty content chunk, in order.
// A nil onDelta is ignored.
// When ctx ends mid-stream, the returned message holds the text received so far
// and its tool calls are dropped, because their arguments may be incomplete.
func (c *Client) ChatStream(
	ctx context.Context,
	model string,
	messages []Message,
	tools []Tool,
	onDelta func(string),
) (Message, error) {
	if err := ctx.Err(); err != nil {
		return Message{}, err
	}
	if messages == nil {
		messages = []Message{}
	}

	payload := chatRequest{
		Model:           model,
		Messages:        messages,
		Tools:           tools,
		Stream:          true,
		ReasoningEffort: defaultReasoningEffort,
	}
	msg, err := c.stream(ctx, payload, onDelta)
	if err != nil && ctx.Err() != nil {
		msg.ToolCalls = nil
		if msg.Role == "" {
			msg.Role = "assistant"
		}
		return msg, ctx.Err()
	}
	return msg, err
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   *string          `json:"content"`
			ToolCalls []streamToolCall `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

type streamToolCall struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type toolDraft struct {
	id   string
	typ  string
	name string
	args strings.Builder
}

func (c *Client) stream(ctx context.Context, payload chatRequest, onDelta func(string)) (Message, error) {
	resp, err := c.send(ctx, "POST", "/v1/chat/completions", payload)
	if err != nil {
		return Message{}, fmt.Errorf("chat: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxResponseBytes)

	var content strings.Builder
	var drafts []toolDraft
	seen := 0
	sawChunk := false

	fail := func(err error) (Message, error) {
		return Message{Role: "assistant", Content: content.String()}, err
	}

	for scanner.Scan() {
		line := scanner.Text()
		seen += len(line) + 1
		if seen > maxResponseBytes {
			return fail(fmt.Errorf("chat: stream exceeds %d bytes", maxResponseBytes))
		}

		data, ok := sseData(line)
		if !ok {
			continue
		}
		if data == "[DONE]" {
			return finishStream(content.String(), drafts)
		}

		sawChunk = true
		if err := applyChunk(data, &content, &drafts, onDelta); err != nil {
			return fail(fmt.Errorf("chat: %w", err))
		}
	}
	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		return fail(fmt.Errorf("chat: reading stream: %w", err))
	}
	if !sawChunk {
		return Message{}, fmt.Errorf("chat: empty stream")
	}
	return finishStream(content.String(), drafts)
}

func sseData(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data:") {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
}

func applyChunk(data string, content *strings.Builder, drafts *[]toolDraft, onDelta func(string)) error {
	var chunk streamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return fmt.Errorf("decoding stream: %w", err)
	}
	if len(chunk.Choices) == 0 {
		return nil
	}
	delta := chunk.Choices[0].Delta
	if delta.Content != nil && *delta.Content != "" {
		content.WriteString(*delta.Content)
		if onDelta != nil {
			onDelta(*delta.Content)
		}
	}
	appendToolCalls(drafts, delta.ToolCalls)
	return nil
}

func appendToolCalls(drafts *[]toolDraft, calls []streamToolCall) {
	for _, call := range calls {
		idx := toolIndex(*drafts, call)
		for len(*drafts) <= idx {
			*drafts = append(*drafts, toolDraft{})
		}
		item := &(*drafts)[idx]
		if call.ID != "" {
			item.id = call.ID
		}
		if call.Type != "" {
			item.typ = call.Type
		}
		item.name += call.Function.Name
		item.args.WriteString(call.Function.Arguments)
	}
}

// toolIndex uses the chunk index when present.
// Ollama often omits index and sends a whole call in one chunk, so a new id or
// name starts another call, and a bare arguments fragment extends the last one.
func toolIndex(drafts []toolDraft, call streamToolCall) int {
	if call.Index != nil && *call.Index >= 0 {
		return *call.Index
	}
	if len(drafts) == 0 {
		return 0
	}
	last := drafts[len(drafts)-1]
	startsNew := call.ID != "" || call.Function.Name != ""
	occupied := last.id != "" || last.name != ""
	if startsNew && occupied {
		return len(drafts)
	}
	return len(drafts) - 1
}

func finishStream(content string, drafts []toolDraft) (Message, error) {
	calls := make([]ToolCall, 0, len(drafts))
	for _, draft := range drafts {
		args, err := decodeArguments(draft.args.String())
		if err != nil {
			return Message{Role: "assistant", Content: content}, fmt.Errorf("chat: %w", err)
		}
		calls = append(calls, ToolCall{
			ID:   draft.id,
			Type: draft.typ,
			Function: FunctionCall{
				Name:      draft.name,
				Arguments: args,
			},
		})
	}
	text := content
	return apiMessage{Role: "assistant", Content: &text, ToolCalls: calls}.message(), nil
}

func decodeArguments(raw string) (Arguments, error) {
	if strings.TrimSpace(raw) == "" {
		return Arguments{}, nil
	}
	var args Arguments
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil, fmt.Errorf("tool arguments: %w", err)
	}
	return args, nil
}
