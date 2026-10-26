// Package ollama is a small client for Ollama's native chat API.
package ollama

// Message is one item in an Ollama chat history.
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Thinking  string     `json:"thinking,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"`
}

// ToolCall is a function the model wants the harness to run.
type ToolCall struct {
	Function FunctionCall `json:"function"`
}

// FunctionCall is the name and arguments of one tool call.
// Ollama sends arguments as a JSON object, not a string.
type FunctionCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
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
