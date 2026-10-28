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

	"co/internal/provider"

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
	t.Setenv("CO_RESUME", "")
	t.Chdir(t.TempDir())
	viper.Reset()
}

func startCLI(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()
	viper.Reset()
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
		"ollama/test-model",
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
	if strings.Contains(stderr, "resume with:") {
		t.Fatalf("eof printed a resume hint: %q", stderr)
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
	stdout, stderr, err := startCLI(
		t,
		"hi\nexit\n",
		"--host",
		server.URL,
		"--model",
		"ollama/test-model",
		"--no-history",
	)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "ok\n" {
		t.Fatalf("stdout %q", stdout)
	}
	if strings.Contains(stderr, "resume with:") {
		t.Fatalf("resume hint %q", stderr)
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

func TestBareModelIsUsageError(t *testing.T) {
	prepare(t)
	_, _, err := startCLI(t, "", "--model", "test-model")
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("err = %v", err)
	}
}

func TestSlashModelsIsNotATurn(t *testing.T) {
	var mu sync.Mutex
	var chats int
	var chatModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"test-model"}]}`)
		case r.URL.Path == "/v1/models/test-model":
			_, _ = io.WriteString(w, `{"id":"test-model"}`)
		case r.URL.Path == "/v1/models/missing":
			http.NotFound(w, r)
		case r.URL.Path == "/v1/chat/completions":
			var payload struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode: %v", err)
				return
			}
			mu.Lock()
			chats++
			chatModel = payload.Model
			mu.Unlock()
			writeSSE(w, contentChunk("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prepare(t)
	stdout, stderr, err := startCLI(
		t,
		"/models\n/nope\n/models ollama/missing\nhi\n",
		"--host",
		server.URL,
		"--model",
		"ollama/test-model",
	)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "ok\n" {
		t.Fatalf("stdout %q", stdout)
	}
	if !strings.Contains(stderr, "ollama/test-model") || !strings.Contains(stderr, "unknown command") {
		t.Fatalf("stderr %q", stderr)
	}
	if !strings.Contains(stderr, "missing") {
		t.Fatalf("stderr %q", stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if chats != 1 || chatModel != "test-model" {
		t.Fatalf("chats=%d model=%q", chats, chatModel)
	}
}

func TestSlashModelsSwitchesAndPersists(t *testing.T) {
	var mu sync.Mutex
	var chatModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/models/test-model" || r.URL.Path == "/v1/models/other":
			_, _ = io.WriteString(w, `{"id":"ok"}`)
		case r.URL.Path == "/v1/chat/completions":
			var payload struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode: %v", err)
				return
			}
			mu.Lock()
			chatModel = payload.Model
			mu.Unlock()
			writeSSE(w, contentChunk("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prepare(t)
	stdout, stderr, err := startCLI(
		t,
		"/models ollama/other\nhi\n",
		"--host",
		server.URL,
		"--model",
		"ollama/test-model",
	)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "ok\n" {
		t.Fatalf("stdout %q", stdout)
	}
	if !strings.Contains(stderr, "model ollama/other") {
		t.Fatalf("stderr %q", stderr)
	}
	mu.Lock()
	if chatModel != "other" {
		t.Fatalf("chat model %q", chatModel)
	}
	mu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(provider.HomeConfig(home))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ollama/other") {
		t.Fatalf("config %s", data)
	}

	matches, err := filepath.Glob(filepath.Join(home, ".co", "projects", "*", "sessions", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("session files %v", matches)
	}
	session, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(session)
	if !strings.Contains(text, `"type":"session"`) || !strings.Contains(text, `"model":"ollama/test-model"`) {
		t.Fatalf("header missing startup model: %s", text)
	}
	if !strings.Contains(text, `"type":"model"`) || !strings.Contains(text, `"model":"ollama/other"`) {
		t.Fatalf("model record missing: %s", text)
	}
}

func TestExitPrintsResumeAndContinueKeepsTheSession(t *testing.T) {
	var mu sync.Mutex
	var chats []struct {
		Model    string
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models/test-model":
			_, _ = io.WriteString(w, `{"id":"test-model"}`)
		case "/v1/chat/completions":
			var body struct {
				Model    string `json:"model"`
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode: %v", err)
				return
			}
			mu.Lock()
			chats = append(chats, struct {
				Model    string
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				}
			}{Model: body.Model, Messages: body.Messages})
			mu.Unlock()
			writeSSE(w, contentChunk("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prepare(t)
	_, stderr, err := startCLI(
		t,
		"hi\nexit\n",
		"--host",
		server.URL,
		"--model",
		"ollama/test-model",
	)
	if err != nil {
		t.Fatal(err)
	}
	id := resumeHint(t, stderr)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provider.HomeConfig(home), []byte("model: ollama/other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = startCLI(
		t,
		"again\nexit\n",
		"--host",
		server.URL,
		"--resume",
		"latest",
	)
	if err != nil {
		t.Fatal(err)
	}
	if resumeHint(t, stderr) != id {
		t.Fatalf("second hint %q want %s", stderr, id)
	}

	matches, err := filepath.Glob(filepath.Join(home, ".co", "projects", "*", "sessions", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || filepath.Base(matches[0]) != id+".jsonl" {
		t.Fatalf("session files %v id %s", matches, id)
	}
	text, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(text), `"type":"session"`) != 1 || !strings.Contains(string(text), "again") {
		t.Fatalf("transcript %s", text)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(chats) != 2 {
		t.Fatalf("chats = %d", len(chats))
	}
	if chats[1].Model != "test-model" {
		t.Fatalf("resumed model %q", chats[1].Model)
	}
	var sawHi, sawAgain bool
	for _, msg := range chats[1].Messages {
		if msg.Role == "user" && msg.Content == "hi" {
			sawHi = true
		}
		if msg.Role == "user" && msg.Content == "again" {
			sawAgain = true
		}
	}
	if !sawHi || !sawAgain {
		t.Fatalf("messages %+v", chats[1].Messages)
	}
}

func TestResumeMissingSessionIsUsageError(t *testing.T) {
	prepare(t)
	_, stderr, err := startCLI(t, "", "--resume", "missing")
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(stderr, "Usage:") {
		t.Fatalf("usage leaked %q", stderr)
	}
}

func resumeHint(t *testing.T, stderr string) string {
	t.Helper()
	const prefix = "resume with: co --resume "
	var id string
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, prefix) {
			id = strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	if id == "" {
		t.Fatalf("resume hint missing in %q", stderr)
	}
	return id
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
