// Package openai is a client for the OpenAI chat-completions API.
package openai

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
	ID           string        `json:"id,omitempty"`
	Type         string        `json:"type,omitempty"`
	Function     FunctionCall  `json:"function"`
	ExtraContent *ExtraContent `json:"extra_content,omitempty"`
}

// ExtraContent holds provider fields that must be sent back unchanged.
// Gemini requires the thought signature on the tool call that carried it.
type ExtraContent struct {
	Google *GoogleExtra `json:"google,omitempty"`
}

// GoogleExtra is Gemini's addition to an OpenAI tool call.
type GoogleExtra struct {
	ThoughtSignature string `json:"thought_signature,omitempty"`
}

func thoughtSignature(extra *ExtraContent) string {
	if extra == nil || extra.Google == nil {
		return ""
	}
	return extra.Google.ThoughtSignature
}

func withThoughtSignature(signature string) *ExtraContent {
	if signature == "" {
		return nil
	}
	return &ExtraContent{Google: &GoogleExtra{ThoughtSignature: signature}}
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
