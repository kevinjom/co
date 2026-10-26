package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxResponseBytes = 8 << 20

// Client talks to one Ollama daemon.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a client for baseURL. A trailing slash is ignored.
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		http:    &http.Client{},
	}
}

type showRequest struct {
	Model string `json:"model"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
}

type chatResponse struct {
	Message Message `json:"message"`
}

// Show checks that the daemon is up and the model is present.
func (c *Client) Show(ctx context.Context, model string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	payload := showRequest{Model: model}
	if err := c.post(ctx, "/api/show", payload, nil); err != nil {
		return fmt.Errorf("show model %q: %w", model, err)
	}
	return nil
}

// Chat sends one non-streaming chat request.
// An empty tools slice omits tools from the request.
func (c *Client) Chat(ctx context.Context, model string, messages []Message, tools []Tool) (Message, error) {
	if err := ctx.Err(); err != nil {
		return Message{}, err
	}
	if messages == nil {
		messages = []Message{}
	}

	payload := chatRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
		Stream:   false,
	}

	var decoded chatResponse
	if err := c.post(ctx, "/api/chat", payload, &decoded); err != nil {
		return Message{}, fmt.Errorf("chat: %w", err)
	}

	msg := decoded.Message
	if msg.Role == "" {
		msg.Role = "assistant"
	}
	if msg.ToolCalls == nil {
		msg.ToolCalls = []ToolCall{}
	}
	for i := range msg.ToolCalls {
		if msg.ToolCalls[i].Function.Arguments == nil {
			msg.ToolCalls[i].Function.Arguments = map[string]any{}
		}
	}
	return msg, nil
}

func (c *Client) post(ctx context.Context, path string, payload any, dest any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: status %d: %s", path, resp.StatusCode, bytes.TrimSpace(respBody))
	}
	if dest == nil || len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, dest); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}
