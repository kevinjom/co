package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"co/internal/ollama"
)

func listDirToolCall(arguments string) string {
	return `{
  "message": {
    "role": "assistant",
    "content": "",
    "tool_calls": [{
      "function": {
        "name": "list_dir",
        "arguments": ` + arguments + `
      }
    }]
  }
}`
}

type recordedChat struct {
	Tools  []any `json:"tools"`
	Stream bool  `json:"stream"`
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

		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			_, _ = io.WriteString(w, listDirToolCall(`{"path":"."}`))
			return
		}
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"two files"}}`)
	}))
	defer server.Close()

	session := NewSession(ollama.New(server.URL), "test-model", t.TempDir())
	var traces []string
	got, err := session.Turn(t.Context(), "what is here?", func(line string) {
		traces = append(traces, line)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "two files" {
		t.Fatalf("answer %q", got)
	}
	if len(traces) != 1 || traces[0] != "tool list_dir path=." {
		t.Fatalf("traces %q", traces)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("calls = %d", len(calls))
	}
	if len(calls[0].Tools) == 0 || calls[0].Stream {
		t.Fatalf("first call omitted tools or streamed")
	}
	if len(calls[1].Tools) == 0 || calls[1].Stream {
		t.Fatalf("second call omitted tools or streamed")
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

		w.Header().Set("Content-Type", "application/json")
		if hasTools {
			_, _ = io.WriteString(w, listDirToolCall(`{}`))
			return
		}
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"stopped"}}`)
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
