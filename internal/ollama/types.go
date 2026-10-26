// Package ollama is a small client for Ollama's OpenAI-compatible chat API.
package ollama

import (
	"encoding/json"
	"strings"
)

// Message is one item in a chat history.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a function the model wants the harness to run.
type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the name and arguments of one tool call.
type FunctionCall struct {
	Name      string    `json:"name"`
	Arguments Arguments `json:"arguments"`
}

// Arguments is a tool-call argument object.
// The OpenAI chat API carries it as a JSON string.
type Arguments map[string]any

func (a Arguments) MarshalJSON() ([]byte, error) {
	if a == nil {
		a = Arguments{}
	}
	raw, err := json.Marshal(map[string]any(a))
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(raw))
}

func (a *Arguments) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*a = Arguments{}
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var encoded string
		if err := json.Unmarshal(data, &encoded); err != nil {
			return err
		}
		if strings.TrimSpace(encoded) == "" {
			*a = Arguments{}
			return nil
		}
		data = []byte(encoded)
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	if obj == nil {
		obj = map[string]any{}
	}
	*a = obj
	return nil
}

// Tool is a tool definition sent on a chat request.
type Tool struct {
	Type     string   `json:"type"`
	Function ToolSpec `json:"function"`
}

// ToolSpec is the function schema inside a Tool.
type ToolSpec struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}
