package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxResponseBytes = 8 << 20

// defaultReasoningEffort is sent as reasoning_effort on every chat request.
const defaultReasoningEffort = "low"

// Client talks to one Ollama daemon through its OpenAI-compatible API.
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

type chatRequest struct {
	Model           string    `json:"model"`
	Messages        []Message `json:"messages"`
	Tools           []Tool    `json:"tools,omitempty"`
	Stream          bool      `json:"stream"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
}

// apiMessage decodes a choice. Content is null when the model only calls tools.
type apiMessage struct {
	Role      string     `json:"role"`
	Content   *string    `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls"`
}

// Show checks that the server is up and the model is present.
func (c *Client) Show(ctx context.Context, model string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	path := "/v1/models/" + url.PathEscape(model)
	if err := c.do(ctx, http.MethodGet, path, nil, nil); err != nil {
		return fmt.Errorf("model %q: %w", model, err)
	}
	return nil
}

func (m apiMessage) message() Message {
	content := ""
	if m.Content != nil {
		content = *m.Content
	}
	msg := Message{
		Role:      m.Role,
		Content:   content,
		ToolCalls: m.ToolCalls,
	}
	if msg.Role == "" {
		msg.Role = "assistant"
	}
	if msg.ToolCalls == nil {
		msg.ToolCalls = []ToolCall{}
	}
	for i := range msg.ToolCalls {
		if msg.ToolCalls[i].ID == "" {
			msg.ToolCalls[i].ID = fmt.Sprintf("call_%d", i+1)
		}
		if msg.ToolCalls[i].Type == "" {
			msg.ToolCalls[i].Type = "function"
		}
		if msg.ToolCalls[i].Function.Arguments == nil {
			msg.ToolCalls[i].Function.Arguments = Arguments{}
		}
	}
	return msg
}

func (c *Client) do(ctx context.Context, method, path string, payload any, dest any) error {
	resp, err := c.send(ctx, method, path, payload)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if dest == nil || len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, dest); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

// send performs one request. A non-200 response is an error and the body is closed.
// On success the caller closes the body.
func (c *Client) send(ctx context.Context, method, path string, payload any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encoding request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return nil, fmt.Errorf("reading response: %w", err)
		}
		return nil, fmt.Errorf("%s: status %d: %s", path, resp.StatusCode, bytes.TrimSpace(respBody))
	}
	return resp, nil
}
