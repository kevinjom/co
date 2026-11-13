package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kevinjom/co/internal/llm"
)

func writeSSE(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, chunk := range chunks {
		_, _ = io.WriteString(w, "data: "+chunk+"\n\n")
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

func contentChunk(text string) string {
	payload, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{"content": text},
			},
		},
	})
	return string(payload)
}

func toolChunk(index *int, id, name, arguments string) string {
	call := map[string]any{
		"function": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}
	if index != nil {
		call["index"] = *index
	}
	if id != "" {
		call["id"] = id
		call["type"] = "function"
	}
	payload, _ := json.Marshal(map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"tool_calls": []any{call},
				},
			},
		},
	})
	return string(payload)
}

func TestChatStreamSendsDeltas(t *testing.T) {
	var sawStream bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Stream          bool   `json:"stream"`
			ReasoningEffort string `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		sawStream = payload.Stream
		if payload.ReasoningEffort != "low" {
			t.Errorf("reasoning_effort %q", payload.ReasoningEffort)
		}
		writeSSE(w, contentChunk("hel"), contentChunk(""), contentChunk("lo"))
	}))
	defer server.Close()

	var deltas []string
	msg, err := New(server.URL).ChatStream(
		t.Context(),
		"m",
		[]llm.Message{{Role: "user", Content: "hi"}},
		nil,
		func(kind llm.StreamKind, delta string) {
			if kind != llm.StreamContent {
				t.Errorf("kind %d", kind)
			}
			deltas = append(deltas, delta)
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !sawStream {
		t.Fatal("request did not stream")
	}
	if strings.Join(deltas, "") != "hello" || msg.Content != "hello" {
		t.Fatalf("deltas %q content %q", deltas, msg.Content)
	}
	if msg.Role != "assistant" {
		t.Fatalf("role %q", msg.Role)
	}
}

func TestChatStreamJoinsToolFragments(t *testing.T) {
	index := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(
			w,
			toolChunk(&index, "call_1", "list_dir", ""),
			toolChunk(&index, "", "", `{"path":`),
			toolChunk(&index, "", "", `"."}`),
		)
	}))
	defer server.Close()

	msg, err := New(server.URL).ChatStream(t.Context(), "m", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "" {
		t.Fatalf("content %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("calls %+v", msg.ToolCalls)
	}
	call := msg.ToolCalls[0]
	if call.ID != "call_1" || call.Type != "function" || call.Function.Name != "list_dir" {
		t.Fatalf("call %+v", call)
	}
	if call.Function.Arguments["path"] != "." {
		t.Fatalf("arguments %+v", call.Function.Arguments)
	}
}

func TestChatStreamOmitsToolIndex(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(
			w,
			toolChunk(nil, "call_a", "list_dir", `{"path":"."}`),
			toolChunk(nil, "call_b", "grep", `{"pattern":"x"}`),
		)
	}))
	defer server.Close()

	msg, err := New(server.URL).ChatStream(t.Context(), "m", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("calls %+v", msg.ToolCalls)
	}
	if msg.ToolCalls[0].Function.Name != "list_dir" || msg.ToolCalls[1].Function.Name != "grep" {
		t.Fatalf("calls %+v", msg.ToolCalls)
	}
	if msg.ToolCalls[1].Function.Arguments["pattern"] != "x" {
		t.Fatalf("arguments %+v", msg.ToolCalls[1].Function.Arguments)
	}
}

func TestChatStreamRejectsMalformedChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: "+contentChunk("par")+"\n\n")
		_, _ = io.WriteString(w, "data: {not json}\n\n")
	}))
	defer server.Close()

	var deltas []string
	msg, err := New(server.URL).ChatStream(
		t.Context(),
		"m",
		nil,
		nil,
		func(kind llm.StreamKind, delta string) {
			if kind != llm.StreamContent {
				t.Errorf("kind %d", kind)
			}
			deltas = append(deltas, delta)
		},
	)
	if err == nil {
		t.Fatal("expected an error")
	}
	if msg.Content != "par" || strings.Join(deltas, "") != "par" {
		t.Fatalf("content %q deltas %q", msg.Content, deltas)
	}
	if len(msg.ToolCalls) != 0 {
		t.Fatalf("tool calls %+v", msg.ToolCalls)
	}
}

func TestChatStreamHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := New(server.URL).ChatStream(t.Context(), "m", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("err = %v", err)
	}
}

func TestChatStreamCancelKeepsPartialText(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("response writer cannot flush")
			return
		}
		_, _ = io.WriteString(w, "data: "+contentChunk("partial")+"\n\n")
		flusher.Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	msg, err := New(server.URL).ChatStream(ctx, "m", nil, nil, func(llm.StreamKind, string) {
		cancel()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if msg.Content != "partial" || msg.Role != "assistant" {
		t.Fatalf("message %+v", msg)
	}
	if len(msg.ToolCalls) != 0 {
		t.Fatalf("tool calls kept on cancel: %+v", msg.ToolCalls)
	}
	<-started
}

func TestChatStreamSeparatesReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(
			w,
			chunkWith("reasoning", "Think"),
			chunkWith("reasoning", "ing"),
			chunkWith("content", "Hi"),
		)
	}))
	defer server.Close()

	var got []string
	msg, err := New(server.URL).ChatStream(t.Context(), "m", nil, nil, func(kind llm.StreamKind, text string) {
		switch kind {
		case llm.StreamReasoning:
			got = append(got, "r:"+text)
		case llm.StreamContent:
			got = append(got, "c:"+text)
		default:
			t.Errorf("kind %d", kind)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "r:Think r:ing c:Hi" {
		t.Fatalf("chunks %q", got)
	}
	if msg.Content != "Hi" {
		t.Fatalf("content %q", msg.Content)
	}
}

func chunkWith(field, text string) string {
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{field: text}}},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func TestChatStreamEmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	defer server.Close()

	_, err := New(server.URL).ChatStream(t.Context(), "m", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "empty stream") {
		t.Fatalf("err = %v", err)
	}
}

func TestEndpointSendsKeyAndSkipsReasoning(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization %q", r.Header.Get("Authorization"))
		}
		var payload struct {
			ReasoningEffort string `json:"reasoning_effort"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		if payload.ReasoningEffort != "" {
			t.Errorf("reasoning_effort %q", payload.ReasoningEffort)
		}
		writeSSE(w, chunkWith("reasoning", "hidden"), chunkWith("content", "hi"))
	}))
	defer server.Close()

	var sawReasoning bool
	msg, err := NewEndpoint(server.URL, "secret", llm.Compat{}).ChatStream(
		t.Context(),
		"m",
		nil,
		nil,
		func(kind llm.StreamKind, _ string) {
			if kind == llm.StreamReasoning {
				sawReasoning = true
			}
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if sawReasoning || msg.Content != "hi" {
		t.Fatalf("reasoning %v content %q", sawReasoning, msg.Content)
	}
}

func TestChatPathFollowsBase(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		writeSSE(w, contentChunk("ok"))
	}))
	defer server.Close()

	base := server.URL + "/v1beta/openai"
	if _, err := NewEndpoint(base, "secret", llm.Compat{}).ChatStream(t.Context(), "gemini-flash-latest", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got != "/v1beta/openai/chat/completions" {
		t.Fatalf("path %q", got)
	}
}

func signatureChunk(index int, signature string) string {
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{
			"tool_calls": []any{map[string]any{
				"index": index,
				"extra_content": map[string]any{
					"google": map[string]any{"thought_signature": signature},
				},
			}},
		}}},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func TestChatStreamReplaysThoughtSignature(t *testing.T) {
	var replay struct {
		Messages []struct {
			Role      string `json:"role"`
			ToolCalls []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
				ExtraContent *llm.ExtraContent `json:"extra_content"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	var turn int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		turn++
		if turn == 1 {
			first := 0
			second := 1
			writeSSE(w,
				toolChunk(&first, "call_1", "list_dir", `{"path":"."}`),
				signatureChunk(0, "sig-a"),
				toolChunk(&second, "call_2", "grep", `{"pattern":"x"}`),
			)
			return
		}
		if err := json.Unmarshal(body, &replay); err != nil {
			t.Errorf("decode replay: %v", err)
		}
		writeSSE(w, contentChunk("ok"))
	}))
	defer server.Close()

	client := NewEndpoint(server.URL, "secret", llm.Compat{})
	msg, err := client.ChatStream(t.Context(), "gemini-flash-latest", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("calls %+v", msg.ToolCalls)
	}
	if thoughtSignature(msg.ToolCalls[0].ExtraContent) != "sig-a" {
		t.Fatalf("first signature %q", thoughtSignature(msg.ToolCalls[0].ExtraContent))
	}
	if msg.ToolCalls[1].ExtraContent != nil {
		t.Fatalf("second call carried %+v", msg.ToolCalls[1].ExtraContent)
	}

	_, err = client.ChatStream(t.Context(), "gemini-flash-latest", []llm.Message{
		{Role: "user", Content: "look"},
		msg,
		{Role: "tool", Content: "files", ToolCallID: "call_1"},
		{Role: "tool", Content: "hits", ToolCallID: "call_2"},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.Messages) != 4 || replay.Messages[1].Role != "assistant" {
		t.Fatalf("replay %+v", replay.Messages)
	}
	calls := replay.Messages[1].ToolCalls
	if len(calls) != 2 || calls[0].Function.Name != "list_dir" || calls[1].Function.Name != "grep" {
		t.Fatalf("replayed calls %+v", calls)
	}
	if thoughtSignature(calls[0].ExtraContent) != "sig-a" {
		t.Fatalf("replayed signature %q", thoughtSignature(calls[0].ExtraContent))
	}
	if calls[1].ExtraContent != nil {
		t.Fatalf("second replay carried %+v", calls[1].ExtraContent)
	}
}

func TestListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, `{"data":[{"id":"a"},{"id":""},{"id":"b/c"}]}`)
	}))
	defer server.Close()

	ids, err := New(server.URL).List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "a,b/c" {
		t.Fatalf("ids %v", ids)
	}
}

func TestChatStreamRecordsUsage(t *testing.T) {
	var include bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			StreamOptions struct {
				IncludeUsage bool `json:"include_usage"`
			} `json:"stream_options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		include = payload.StreamOptions.IncludeUsage
		writeSSE(
			w,
			contentChunk("hi"),
			`{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":3}}`,
		)
	}))
	defer server.Close()

	msg, err := New(server.URL).ChatStream(t.Context(), "m", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !include {
		t.Fatal("request did not ask for usage")
	}
	if msg.Content != "hi" || msg.Usage == nil || msg.Usage.Input != 12 || msg.Usage.Output != 3 {
		t.Fatalf("message %+v usage %+v", msg, msg.Usage)
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "input") || strings.Contains(string(raw), "prompt_tokens") {
		t.Fatalf("usage leaked into message json %s", raw)
	}
}
