// Package openai is a client for an OpenAI-compatible chat-completions API.
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/kevinjom/co/internal/llm"
)

const maxResponseBytes = 8 << 20

// defaultReasoningEffort is sent as reasoning_effort on every chat request.
const defaultReasoningEffort = "low"

// Client talks to one OpenAI-compatible chat endpoint.
type Client struct {
	baseURL string
	apiKey  string
	compat  llm.Compat
	http    *http.Client
}

var _ llm.Chatter = (*Client)(nil)

// New returns a client for baseURL with no API key.
// A trailing slash is ignored.
// Reasoning effort and the reasoning delta are enabled.
func New(baseURL string) *Client {
	return NewEndpoint(baseURL, "", llm.Compat{
		ReasoningEffort: true,
		ReasoningDelta:  true,
	})
}

// NewEndpoint returns a client for one provider endpoint.
func NewEndpoint(baseURL, apiKey string, compat llm.Compat) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiKey:  apiKey,
		compat:  compat,
		http:    &http.Client{},
	}
}

type chatRequest struct {
	Model           string        `json:"model"`
	Messages        []llm.Message `json:"messages"`
	Tools           []llm.Tool    `json:"tools,omitempty"`
	Stream          bool          `json:"stream"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty"`
}

// apiMessage decodes a choice. Content is null when the model only calls tools.
type apiMessage struct {
	Role      string         `json:"role"`
	Content   *string        `json:"content"`
	ToolCalls []llm.ToolCall `json:"tool_calls"`
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

type modelList struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// List returns model ids from GET /v1/models.
func (c *Client) List(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var dest modelList
	if err := c.do(ctx, http.MethodGet, "/v1/models", nil, &dest); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(dest.Data))
	for _, item := range dest.Data {
		if item.ID == "" {
			continue
		}
		ids = append(ids, item.ID)
	}
	return ids, nil
}

func (m apiMessage) message() llm.Message {
	content := ""
	if m.Content != nil {
		content = *m.Content
	}
	msg := llm.Message{
		Role:      m.Role,
		Content:   content,
		ToolCalls: m.ToolCalls,
	}
	if msg.Role == "" {
		msg.Role = "assistant"
	}
	if msg.ToolCalls == nil {
		msg.ToolCalls = []llm.ToolCall{}
	}
	for i := range msg.ToolCalls {
		if msg.ToolCalls[i].ID == "" {
			msg.ToolCalls[i].ID = fmt.Sprintf("call_%d", i+1)
		}
		if msg.ToolCalls[i].Type == "" {
			msg.ToolCalls[i].Type = "function"
		}
		if msg.ToolCalls[i].Function.Arguments == nil {
			msg.ToolCalls[i].Function.Arguments = llm.Arguments{}
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

// chatCompletionsPath is /v1/chat/completions on a server root such as an Ollama host.
// A base that already ends in /v1 or /openai, including OpenRouter and Gemini's
// OpenAI-compatible endpoint, only needs /chat/completions.
func chatCompletionsPath(baseURL string) string {
	if strings.HasSuffix(baseURL, "/v1") || strings.HasSuffix(baseURL, "/openai") {
		return "/chat/completions"
	}
	return "/v1/chat/completions"
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
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
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
