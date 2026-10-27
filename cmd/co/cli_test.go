package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	t.Setenv("CO_NO_HISTORY", "")
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
	var chats int
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
			mu.Lock()
			chats++
			n := chats
			mu.Unlock()
			if n == 1 {
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
	assertSessionRoles(t, "user", "assistant", "tool", "assistant")
}

func TestNoHistoryWritesNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/test-model":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"test-model"}`)
		case "/v1/chat/completions":
			writeSSE(w, contentChunk("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prepare(t)
	stdout, _, err := startCLI(
		t,
		"hi\n",
		"--host",
		server.URL,
		"--model",
		"test-model",
		"--no-history",
	)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "ok\n" {
		t.Fatalf("stdout %q", stdout)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(home, ".co", "projects", "*", "sessions", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("session files %v", matches)
	}
}

func assertSessionRoles(t *testing.T, roles ...string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(home, ".co", "projects", "*", "sessions", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("session files %v", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) != len(roles)+1 {
		t.Fatalf("lines = %d, file %s", len(lines), data)
	}
	var header struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header.Type != "session" {
		t.Fatalf("header %s", lines[0])
	}
	for i, role := range roles {
		var rec struct {
			Seq     int `json:"seq"`
			Message struct {
				Role string `json:"role"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(lines[i+1]), &rec); err != nil {
			t.Fatal(err)
		}
		if rec.Seq != i+1 || rec.Message.Role != role {
			t.Fatalf("line %d seq %d role %q", i+1, rec.Seq, rec.Message.Role)
		}
	}
}
