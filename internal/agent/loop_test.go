package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"co/internal/ollama"
)

func writeSSE(w http.ResponseWriter, chunks ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, chunk := range chunks {
		_, _ = io.WriteString(w, "data: "+chunk+"\n\n")
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

func contentChunk(text string) string {
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"content": text}}},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func toolChunk(id, name, arguments string) string {
	call := map[string]any{
		"function": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}
	if id != "" {
		call["id"] = id
		call["type"] = "function"
	}
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{
			"tool_calls": []any{call},
		}}},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

type recordedChat struct {
	Messages []struct {
		Role       string `json:"role"`
		ToolCallID string `json:"tool_call_id"`
		ToolCalls  []struct {
			ID       string `json:"id"`
			Function struct {
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"messages"`
	Tools           []any  `json:"tools"`
	Stream          bool   `json:"stream"`
	ReasoningEffort string `json:"reasoning_effort"`
}

func TestTurnRunsToolThenAnswers(t *testing.T) {
	var mu sync.Mutex
	var calls []recordedChat

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		var call recordedChat
		if err := json.Unmarshal(body, &call); err != nil {
			t.Errorf("decode body: %v", err)
			return
		}
		mu.Lock()
		calls = append(calls, call)
		n := len(calls)
		mu.Unlock()

		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if n == 1 {
			writeSSE(w,
				toolChunk("call_list_dir", "list_dir", `{"pa`),
				toolChunk("", "", `th":"."}`),
			)
			return
		}
		writeSSE(w, contentChunk("two"), contentChunk(" files"))
	}))
	defer server.Close()

	session := NewSession(ollama.New(server.URL), "test-model", t.TempDir())
	var events []Event
	got, err := session.Turn(t.Context(), "what is here?", func(ev Event) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "two files" {
		t.Fatalf("answer %q", got)
	}
	if len(events) != 4 {
		t.Fatalf("events %+v", events)
	}
	start, ok := events[0].(ToolStart)
	if !ok || start.Name != "list_dir" || start.Detail != "path=." || start.ID != "call_list_dir" {
		t.Fatalf("start %+v", events[0])
	}
	done, ok := events[1].(ToolDone)
	if !ok || done.ID != "call_list_dir" || done.Err != nil {
		t.Fatalf("done %+v", events[1])
	}
	first, ok := events[2].(AnswerDelta)
	second, ok2 := events[3].(AnswerDelta)
	if !ok || !ok2 || first.Text+second.Text != "two files" {
		t.Fatalf("deltas %+v %+v", events[2], events[3])
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("calls = %d", len(calls))
	}
	if len(calls[0].Tools) == 0 || !calls[0].Stream {
		t.Fatalf("first call omitted tools or did not stream")
	}
	if len(calls[1].Tools) == 0 || !calls[1].Stream {
		t.Fatalf("second call omitted tools or did not stream")
	}
	for i, call := range calls {
		if call.ReasoningEffort != "low" {
			t.Fatalf("call %d reasoning_effort = %q", i+1, call.ReasoningEffort)
		}
	}
	followUp := calls[1].Messages
	if len(followUp) != 4 {
		t.Fatalf("follow-up messages = %d", len(followUp))
	}
	assistant := followUp[2]
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Function.Arguments != `{"path":"."}` {
		t.Fatalf("assistant tool call %+v", assistant.ToolCalls)
	}
	toolMsg := followUp[3]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != assistant.ToolCalls[0].ID {
		t.Fatalf("tool message %+v", toolMsg)
	}
}

func TestTurnDropsToolsAfterCap(t *testing.T) {
	var mu sync.Mutex
	var calls []recordedChat

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		var call recordedChat
		if err := json.Unmarshal(body, &call); err != nil {
			t.Errorf("decode body: %v", err)
			return
		}
		mu.Lock()
		calls = append(calls, call)
		hasTools := len(call.Tools) > 0
		mu.Unlock()

		if hasTools {
			writeSSE(w, toolChunk("call_list_dir", "list_dir", `{}`))
			return
		}
		writeSSE(w, contentChunk("stopped"))
	}))
	defer server.Close()

	session := NewSession(ollama.New(server.URL), "test-model", t.TempDir())
	got, err := session.Turn(t.Context(), "keep going", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "stopped" {
		t.Fatalf("answer %q", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != maxToolRounds+1 {
		t.Fatalf("calls = %d, want %d", len(calls), maxToolRounds+1)
	}
	for i := range maxToolRounds {
		if len(calls[i].Tools) == 0 {
			t.Fatalf("call %d omitted tools", i+1)
		}
	}
	if len(calls[maxToolRounds].Tools) != 0 {
		t.Fatal("final call included tools")
	}
}

func TestTurnKeepsUserMessageOnModelError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	session := NewSession(ollama.New(server.URL), "test-model", t.TempDir())
	_, err := session.Turn(t.Context(), "hello", nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(session.messages) != 2 {
		t.Fatalf("messages = %d, want system + user", len(session.messages))
	}
	if session.messages[1].Role != "user" || session.messages[1].Content != "hello" {
		t.Fatalf("user message = %+v", session.messages[1])
	}
}

func TestTurnKeepsPartialAnswerOnCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("response writer cannot flush")
			return
		}
		_, _ = io.WriteString(w, "data: "+contentChunk("partial")+"\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	session := NewSession(ollama.New(server.URL), "test-model", t.TempDir())
	got, err := session.Turn(ctx, "hello", func(ev Event) {
		if _, ok := ev.(AnswerDelta); ok {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if got != "partial" {
		t.Fatalf("answer %q", got)
	}
	last := session.messages[len(session.messages)-1]
	if last.Role != "assistant" || last.Content != "partial" || len(last.ToolCalls) != 0 {
		t.Fatalf("last message %+v", last)
	}
}

func TestTurnReportsToolFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var call recordedChat
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		if len(call.Tools) > 0 {
			writeSSE(w, toolChunk("call_read", "read_file", `{"path":"missing.txt"}`))
			return
		}
		writeSSE(w, contentChunk("the file is missing"))
	}))
	defer server.Close()

	session := NewSession(ollama.New(server.URL), "test-model", t.TempDir())
	var done ToolDone
	got, err := session.Turn(t.Context(), "read it", func(ev Event) {
		if event, ok := ev.(ToolDone); ok {
			done = event
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "the file is missing" {
		t.Fatalf("answer %q", got)
	}
	if done.ID != "call_read" || done.Err == nil {
		t.Fatalf("done %+v", done)
	}
	toolMsg := session.messages[len(session.messages)-2]
	if toolMsg.Role != "tool" || !strings.HasPrefix(toolMsg.Content, "error:") {
		t.Fatalf("tool message %+v", toolMsg)
	}
}

func TestTurnStopsWhenContextIsCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("canceled turn called the server")
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	session := NewSession(ollama.New(server.URL), "test-model", t.TempDir())
	_, err := session.Turn(ctx, "hello", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
