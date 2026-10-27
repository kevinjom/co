package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		[]Message{{Role: "user", Content: "hi"}},
		nil,
		func(delta string) { deltas = append(deltas, delta) },
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
		func(delta string) { deltas = append(deltas, delta) },
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

	msg, err := New(server.URL).ChatStream(ctx, "m", nil, nil, func(string) {
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
