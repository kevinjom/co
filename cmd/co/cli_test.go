package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kevinjom/co/internal/provider"

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
	t.Setenv("OLLAMA_HOST", "")
	t.Setenv("CO_LOG_LEVEL", "")
	t.Setenv("CO_NO_HISTORY", "")
	t.Setenv("CO_CLEAR_SCROLLBACK", "")
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

func TestClearScrollbackDefaultsOff(t *testing.T) {
	prepare(t)
	cmd, err := newRootCmd()
	if err != nil {
		t.Fatal(err)
	}
	flag := cmd.Flags().Lookup("clear-scrollback")
	if flag == nil || flag.DefValue != "false" {
		t.Fatalf("flag %+v", flag)
	}

	if _, _, err := startCLI(t, "", "version"); err != nil {
		t.Fatal(err)
	}
	if viper.GetBool("clear-scrollback") {
		t.Fatal("clear-scrollback defaulted on")
	}

	t.Setenv("CO_CLEAR_SCROLLBACK", "true")
	if _, _, err := startCLI(t, "", "version"); err != nil {
		t.Fatal(err)
	}
	if !viper.GetBool("clear-scrollback") {
		t.Fatal("CO_CLEAR_SCROLLBACK was ignored")
	}
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
	var chats int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		switch r.URL.Path {
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
	writeOllamaConfig(t, server.URL, "test-model")

	stdout, stderr, err := startCLI(
		t,
		"how many lines?\n",
		"--model",
		"ollama/test-model",
	)
	if err != nil {
		t.Fatal(err)
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
		case "/v1/chat/completions":
			writeSSE(w, contentChunk("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prepare(t)
	writeOllamaConfig(t, server.URL, "test-model")
	stdout, stderr, err := startCLI(
		t,
		"hi\nexit\n",
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

func TestMissingOllamaProviderNamesTheSetupCommand(t *testing.T) {
	prepare(t)
	_, stderr, err := startCLI(t, "", "--model", "ollama/qwen3.5:4b")
	if err == nil || !strings.Contains(err.Error(), `unknown provider "ollama": run co ollama`) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(stderr, "Usage:") {
		t.Fatalf("usage leaked %q", stderr)
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
		switch r.URL.Path {
		case "/v1/chat/completions":
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
	writeOllamaConfig(t, server.URL, "test-model")
	stdout, stderr, err := startCLI(
		t,
		"/models\n/nope\n/models ollama/missing\nhi\n",
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

func TestConnectListsKeyProviders(t *testing.T) {
	prepare(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	body := `
model: ollama/test-model
providers:
  ollama:
    base_url: http://127.0.0.1:11434
    auth: none
    models:
      - test-model
  gemini:
    base_url: https://generativelanguage.googleapis.com/v1beta/openai
    api_key_env: GEMINI_API_KEY
    models:
      - gemini-2.5-flash
  openrouter:
    base_url: https://openrouter.ai/api/v1
    api_key_env: OPENROUTER_API_KEY
    models:
      - qwen/qwen3-coder
`
	path := provider.HomeConfig(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := startCLI(t, "/connect\nexit\n", "--no-history")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "" {
		t.Fatalf("stdout %q", stdout)
	}
	if !strings.Contains(stderr, "gemini\n") || !strings.Contains(stderr, "openrouter\n") {
		t.Fatalf("stderr %q", stderr)
	}
	for _, line := range strings.Split(stderr, "\n") {
		if line == "ollama" {
			t.Fatalf("auth none was listed: %q", stderr)
		}
	}
}

func TestSlashModelsSwitchesAndPersists(t *testing.T) {
	var mu sync.Mutex
	var chatModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chat/completions":
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
	writeOllamaConfig(t, server.URL, "test-model", "other")
	stdout, stderr, err := startCLI(
		t,
		"/models ollama/other\nhi\n",
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
	writeOllamaConfig(t, server.URL, "test-model")
	_, stderr, err := startCLI(
		t,
		"hi\nexit\n",
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
	body := fmt.Sprintf(`model: "ollama/other"
providers:
  ollama:
    base_url: %q
    auth: none
    models:
      - test-model
`, server.URL)
	if err := os.WriteFile(provider.HomeConfig(home), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stderr, err = startCLI(
		t,
		"again\nexit\n",
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

func TestOllamaUsesTheRunningModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			_, _ = io.WriteString(w, `{"models":[{"name":"b"}]}`)
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"b"},{"id":"a"},{"id":"a"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prepare(t)
	useInstalledOllama(t)
	stdout, stderr, err := startCLI(t, "", "ollama", "--host", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "model: ollama/b\n" {
		t.Fatalf("stdout %q stderr %q", stdout, stderr)
	}
	if strings.Contains(stderr, "Which model?") {
		t.Fatalf("asked despite a running model: %q", stderr)
	}
	text := readHomeConfig(t)
	modelAt := strings.Index(text, "model:")
	providersAt := strings.Index(text, "providers:")
	if modelAt < 0 || providersAt < modelAt || !strings.Contains(text, `model: "ollama/b"`) {
		t.Fatalf("config:\n%s", text)
	}
	if !strings.Contains(text, server.URL) || !strings.Contains(text, "auth: none") {
		t.Fatalf("config:\n%s", text)
	}
	if !strings.Contains(text, "reasoning_effort: true") || !strings.Contains(text, "reasoning_delta: true") {
		t.Fatalf("config:\n%s", text)
	}
	a := strings.Index(text, `"a"`)
	b := strings.Index(text, `"b"`)
	if a < 0 || b < a {
		t.Fatalf("config:\n%s", text)
	}
}

func TestOllamaRefreshKeepsCompatAndUsesRunningModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			_, _ = io.WriteString(w, `{"models":[{"name":"qwen3:8b"}]}`)
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"qwen3:8b"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prepare(t)
	useInstalledOllama(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	path := provider.HomeConfig(home)
	body := `model: "openrouter/keep"
log-level: debug
providers:
  ollama:
    base_url: http://old.example:11434
    auth: none
    models:
      - old
    compat:
      reasoning_effort: false
      reasoning_delta: false
`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := startCLI(t, "", "ollama", "--host", server.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "model: ollama/qwen3:8b\n" {
		t.Fatalf("stdout %q", stdout)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	modelAt := strings.Index(text, "model:")
	providersAt := strings.Index(text, "providers:")
	logAt := strings.Index(text, "log-level:")
	if modelAt < 0 || providersAt < modelAt || logAt < providersAt {
		t.Fatalf("config:\n%s", text)
	}
	if !strings.Contains(text, `model: "ollama/qwen3:8b"`) || strings.Contains(text, "old.example") {
		t.Fatalf("config:\n%s", text)
	}
	if strings.Contains(text, "reasoning_effort: true") || !strings.Contains(text, server.URL) {
		t.Fatalf("config:\n%s", text)
	}
	if !strings.Contains(text, `"qwen3:8b"`) || !strings.Contains(text, "log-level: debug") {
		t.Fatalf("config:\n%s", text)
	}
}

func TestOllamaNotInstalledWritesNothing(t *testing.T) {
	prepare(t)
	look := ollamaLookPath
	t.Cleanup(func() { ollamaLookPath = look })
	ollamaLookPath = func(string) (string, error) { return "", errors.New("not found") }

	_, _, err := startCLI(t, "", "ollama")
	if err == nil || !strings.Contains(err.Error(), "ollama is not installed") {
		t.Fatalf("err = %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(provider.HomeConfig(home)); !os.IsNotExist(statErr) {
		t.Fatal("config was written")
	}
}

func TestOllamaAsksWhenNothingIsLoaded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			_, _ = io.WriteString(w, `{"models":[]}`)
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"b"},{"id":"a"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prepare(t)
	useInstalledOllama(t)
	stdout, stderr, err := startCLI(t, "2\n", "ollama", "--host", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "model: ollama/b\n" {
		t.Fatalf("stdout %q stderr %q", stdout, stderr)
	}
	if !strings.Contains(stderr, "1. a") || !strings.Contains(stderr, "2. b") {
		t.Fatalf("stderr %q", stderr)
	}
	if !strings.Contains(stderr, "Which model?") {
		t.Fatalf("stderr %q", stderr)
	}
	if !strings.Contains(readHomeConfig(t), `model: "ollama/b"`) {
		t.Fatalf("config:\n%s", readHomeConfig(t))
	}
}

func TestOllamaStartsDaemonThenAsks(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	host := "http://" + addr

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			_, _ = io.WriteString(w, `{"models":[]}`)
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"a"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer func() {
		if server.Listener != nil {
			server.Close()
		}
	}()

	prepare(t)
	useInstalledOllama(t)
	var started int
	ollamaStart = func(got string) error {
		started++
		if got != host {
			t.Errorf("host %q", got)
		}
		listen, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		server.Listener = listen
		server.Start()
		return nil
	}

	stdout, stderr, err := startCLI(t, "a\n", "ollama", "--host", host)
	if err != nil {
		t.Fatal(err)
	}
	if started != 1 {
		t.Fatalf("started %d", started)
	}
	if stdout != "model: ollama/a\n" {
		t.Fatalf("stdout %q stderr %q", stdout, stderr)
	}
	if !strings.Contains(stderr, "Which model?") {
		t.Fatalf("stderr %q", stderr)
	}
}

func TestOllamaHostFromEnv(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ps":
			_, _ = io.WriteString(w, `{"models":[{"name":"local"}]}`)
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"local"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	prepare(t)
	useInstalledOllama(t)
	t.Setenv("OLLAMA_HOST", server.URL)
	if _, _, err := startCLI(t, "", "ollama"); err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(provider.HomeConfig(home))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), server.URL) || !strings.Contains(string(data), `model: "ollama/local"`) {
		t.Fatalf("config:\n%s", data)
	}
}

func TestOllamaEmptyHostIsUsageError(t *testing.T) {
	prepare(t)
	useInstalledOllama(t)
	_, _, err := startCLI(t, "", "ollama", "--host", "")
	var exitErr *exitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("err = %v", err)
	}
}

func useInstalledOllama(t *testing.T) {
	t.Helper()
	look := ollamaLookPath
	start := ollamaStart
	t.Cleanup(func() {
		ollamaLookPath = look
		ollamaStart = start
	})
	ollamaLookPath = func(string) (string, error) { return "ollama", nil }
	ollamaStart = func(string) error {
		t.Fatal("ollama serve was started")
		return nil
	}
}

func readHomeConfig(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(provider.HomeConfig(home))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeOllamaConfig(t *testing.T, baseURL string, models ...string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	if len(models) > 0 {
		fmt.Fprintf(&body, "model: %q\n", "ollama/"+models[0])
	}
	fmt.Fprintf(&body, "providers:\n  ollama:\n    base_url: %q\n    auth: none\n    models:\n", baseURL)
	for _, model := range models {
		fmt.Fprintf(&body, "      - %q\n", model)
	}
	path := provider.HomeConfig(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
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
