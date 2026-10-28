package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"co/internal/openai"
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

	session := NewSession(openai.New(server.URL), "test-model", t.TempDir(), nil)
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

	session := NewSession(openai.New(server.URL), "test-model", t.TempDir(), nil)
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

	session := NewSession(openai.New(server.URL), "test-model", t.TempDir(), nil)
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

	session := NewSession(openai.New(server.URL), "test-model", t.TempDir(), nil)
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
	var mu sync.Mutex
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			writeSSE(w, toolChunk("call_read", "read_file", `{"path":"missing.txt"}`))
			return
		}
		writeSSE(w, contentChunk("the file is missing"))
	}))
	defer server.Close()

	session := NewSession(openai.New(server.URL), "test-model", t.TempDir(), nil)
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

type memRecorder struct {
	msgs        []openai.Message
	interrupted []bool
	fail        error
	calls       int
}

func (m *memRecorder) Record(msg openai.Message, interrupted bool) error {
	m.calls++
	if m.fail != nil {
		return m.fail
	}
	m.msgs = append(m.msgs, msg)
	m.interrupted = append(m.interrupted, interrupted)
	return nil
}

func TestTurnRecordsCompletedMessages(t *testing.T) {
	var mu sync.Mutex
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			writeSSE(w, toolChunk("call_list_dir", "list_dir", `{"path":"."}`))
			return
		}
		writeSSE(w, contentChunk("two"), contentChunk(" files"))
	}))
	defer server.Close()

	rec := &memRecorder{}
	session := NewSession(openai.New(server.URL), "test-model", t.TempDir(), rec)
	if _, err := session.Turn(t.Context(), "what is here?", nil); err != nil {
		t.Fatal(err)
	}
	if len(rec.msgs) != 4 {
		t.Fatalf("records = %d, want user, assistant, tool, assistant", len(rec.msgs))
	}
	want := []string{"user", "assistant", "tool", "assistant"}
	for i, role := range want {
		if rec.msgs[i].Role != role || rec.interrupted[i] {
			t.Fatalf("record %d role %q interrupted %v", i, rec.msgs[i].Role, rec.interrupted[i])
		}
	}
	if len(rec.msgs[1].ToolCalls) != 1 || rec.msgs[1].ToolCalls[0].Function.Name != "list_dir" {
		t.Fatalf("tool call %+v", rec.msgs[1].ToolCalls)
	}
	if rec.msgs[2].ToolCallID != "call_list_dir" {
		t.Fatalf("tool result %+v", rec.msgs[2])
	}
	if rec.msgs[3].Content != "two files" {
		t.Fatalf("answer %q", rec.msgs[3].Content)
	}
}

func TestTurnDisablesHistoryAfterWriteError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, contentChunk("ok"))
	}))
	defer server.Close()

	rec := &memRecorder{fail: errors.New("disk full")}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	session := NewSession(openai.New(server.URL), "test-model", t.TempDir(), rec)
	got, err := session.Turn(t.Context(), "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("answer %q", got)
	}
	if rec.calls != 1 {
		t.Fatalf("record calls = %d, want the user message only", rec.calls)
	}
	if session.recorder != nil {
		t.Fatal("recorder still enabled")
	}
}

func TestTurnRecordsInterruptedPartial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Errorf("response writer cannot flush")
			return
		}
		_, _ = io.WriteString(w, "data: "+contentChunk("partial")+"\n\n")
		_, _ = io.WriteString(w, "data: "+toolChunk("call_x", "list_dir", `{"path":"."}`)+"\n\n")
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	rec := &memRecorder{}
	session := NewSession(openai.New(server.URL), "test-model", t.TempDir(), rec)
	_, err := session.Turn(ctx, "hello", func(ev Event) {
		if _, ok := ev.(AnswerDelta); ok {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if len(rec.msgs) != 2 {
		t.Fatalf("records = %d", len(rec.msgs))
	}
	if rec.interrupted[0] || rec.msgs[0].Role != "user" {
		t.Fatalf("user %+v interrupted %v", rec.msgs[0], rec.interrupted[0])
	}
	got := rec.msgs[1]
	if !rec.interrupted[1] || got.Role != "assistant" || got.Content != "partial" || len(got.ToolCalls) != 0 {
		t.Fatalf("assistant %+v interrupted %v", got, rec.interrupted[1])
	}
}

func TestTurnStreamsReasoningSeparatelyFromAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, fieldChunk("reasoning", "Think"), fieldChunk("content", "Hi"))
	}))
	defer server.Close()

	var got []string
	session := NewSession(openai.New(server.URL), "test-model", t.TempDir(), nil)
	answer, err := session.Turn(t.Context(), "hi", func(ev Event) {
		switch ev := ev.(type) {
		case ReasoningDelta:
			got = append(got, "r:"+ev.Text)
		case AnswerDelta:
			got = append(got, "c:"+ev.Text)
		default:
			t.Fatalf("event %T", ev)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, " ") != "r:Think c:Hi" || answer != "Hi" {
		t.Fatalf("events %q answer %q", got, answer)
	}
	last := session.messages[len(session.messages)-1]
	if last.Content != "Hi" {
		t.Fatalf("stored %q", last.Content)
	}
}

func TestTurnKeepsReasoningWhenTheAnswerIsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, fieldChunk("reasoning", "the "), fieldChunk("reasoning", "answer"))
	}))
	defer server.Close()

	session := NewSession(openai.New(server.URL), "test-model", t.TempDir(), nil)
	answer, err := session.Turn(t.Context(), "explain", nil)
	if err != nil {
		t.Fatal(err)
	}
	if answer != "the answer" {
		t.Fatalf("answer %q", answer)
	}
	last := session.messages[len(session.messages)-1]
	if last.Role != "assistant" || last.Content != "the answer" || len(last.ToolCalls) != 0 {
		t.Fatalf("stored %+v", last)
	}
}

func fieldChunk(field, text string) string {
	payload, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{field: text}}},
	})
	if err != nil {
		panic(err)
	}
	return string(payload)
}

func TestTurnStopsWhenContextIsCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("canceled turn called the server")
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	session := NewSession(openai.New(server.URL), "test-model", t.TempDir(), nil)
	_, err := session.Turn(ctx, "hello", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

type scriptChat struct {
	model string
	seen  [][]openai.Message
}

func (s *scriptChat) ChatStream(
	_ context.Context,
	model string,
	messages []openai.Message,
	_ []openai.Tool,
	onChunk func(openai.StreamKind, string),
) (openai.Message, error) {
	s.model = model
	copied := append([]openai.Message{}, messages...)
	s.seen = append(s.seen, copied)
	if onChunk != nil {
		onChunk(openai.StreamContent, "ok")
	}
	return openai.Message{Role: "assistant", Content: "ok"}, nil
}

func TestSetModelKeepsMessages(t *testing.T) {
	first := &scriptChat{}
	session := NewSession(first, "one", t.TempDir(), nil)
	if _, err := session.Turn(t.Context(), "hello", nil); err != nil {
		t.Fatal(err)
	}
	second := &scriptChat{}
	session.SetModel(second, "two")
	if _, err := session.Turn(t.Context(), "next", nil); err != nil {
		t.Fatal(err)
	}
	if first.model != "one" || second.model != "two" {
		t.Fatalf("models %q %q", first.model, second.model)
	}
	if len(second.seen) != 1 || len(second.seen[0]) != 4 {
		t.Fatalf("follow-up history %+v", second.seen)
	}
	roles := make([]string, len(second.seen[0]))
	for i, msg := range second.seen[0] {
		roles[i] = msg.Role
	}
	if strings.Join(roles, ",") != "system,user,assistant,user" {
		t.Fatalf("roles %v", roles)
	}
	if second.seen[0][1].Content != "hello" || second.seen[0][3].Content != "next" {
		t.Fatalf("messages %+v", second.seen[0])
	}
}

func TestRestoreIsSentOnTheNextTurn(t *testing.T) {
	rec := &memRecorder{}
	chat := &scriptChat{}
	session := NewSession(chat, "test-model", t.TempDir(), rec)
	session.Restore([]openai.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	})
	if rec.calls != 0 {
		t.Fatalf("restore recorded %d messages", rec.calls)
	}
	if _, err := session.Turn(t.Context(), "next", nil); err != nil {
		t.Fatal(err)
	}
	if len(chat.seen) != 1 || len(chat.seen[0]) != 4 {
		t.Fatalf("history %+v", chat.seen)
	}
	if chat.seen[0][1].Content != "hello" || chat.seen[0][2].Content != "hi" || chat.seen[0][3].Content != "next" {
		t.Fatalf("messages %+v", chat.seen[0])
	}
	if rec.calls != 2 {
		t.Fatalf("recorded %d", rec.calls)
	}
}
