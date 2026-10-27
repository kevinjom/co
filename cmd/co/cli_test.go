package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/viper"
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
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
			map[string]any{
				"id":   id,
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": arguments,
				},
			},
		}}}},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func prepare(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CO_MODEL", "")
	t.Setenv("OLLAMA_MODEL", "")
	t.Setenv("CO_HOST", "")
	t.Setenv("OLLAMA_HOST", "")
	t.Setenv("CO_LOG_LEVEL", "")
	t.Chdir(t.TempDir())
	viper.Reset()
}

func startCLI(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	cmd, err := newRootCmd()
	if err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(args)
	runErr := cmd.ExecuteContext(context.Background())
	return stdout.String(), stderr.String(), runErr
}

func TestVersionWritesToStdout(t *testing.T) {
	prepare(t)
	stdout, stderr, err := startCLI(t, "", "version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout, "co dev (commit: unknown, built: unknown)\n") {
		t.Fatalf("stdout %q", stdout)
	}
	if !strings.Contains(stdout, "go: ") {
		t.Fatalf("stdout %q", stdout)
	}
	if strings.Contains(stderr, "Usage:") || strings.Contains(stdout, "Usage:") {
		t.Fatalf("usage leaked stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestMissingModelIsUsageError(t *testing.T) {
	prepare(t)
	stdout, stderr, err := startCLI(t, "")
	var exitErr *exitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v", err)
	}
	if exitErr.Code != 2 {
		t.Fatalf("code = %d", exitErr.Code)
	}
	if strings.Contains(stdout, "Usage:") || strings.Contains(stderr, "Usage:") {
		t.Fatalf("usage leaked stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestPipedTurnSeparatesAnswerAndToolTrace(t *testing.T) {
	var mu sync.Mutex
	var sawModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		switch r.URL.Path {
		case "/v1/models/test-model":
			if r.Method != http.MethodGet {
				t.Errorf("method %s", r.Method)
				return
			}
			mu.Lock()
			sawModel = "test-model"
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"test-model"}`)
		case "/v1/chat/completions":
			var payload struct {
				Tools  []any `json:"tools"`
				Stream bool  `json:"stream"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Errorf("decode chat: %v", err)
				return
			}
			if !payload.Stream {
				t.Errorf("chat request did not stream")
			}
			if len(payload.Tools) > 0 {
				writeSSE(w, toolChunk("call_list_dir", "list_dir", `{"path":"."}`))
				return
			}
			writeSSE(w, contentChunk("There are "), contentChunk("3 lines."))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prepare(t)
	t.Setenv("OLLAMA_HOST", "http://127.0.0.1:1")
	t.Setenv("OLLAMA_MODEL", "ignored")

	stdout, stderr, err := startCLI(
		t,
		"how many lines?\n",
		"--host",
		server.URL,
		"--model",
		"test-model",
	)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	gotModel := sawModel
	mu.Unlock()
	if gotModel != "test-model" {
		t.Fatalf("show model = %q", gotModel)
	}
	if stdout != "There are 3 lines.\n" {
		t.Fatalf("stdout %q", stdout)
	}
	if !strings.Contains(stderr, "tool list_dir path=.") {
		t.Fatalf("stderr %q", stderr)
	}
	if strings.Contains(stdout, "tool ") {
		t.Fatalf("tool trace leaked to stdout %q", stdout)
	}
}
