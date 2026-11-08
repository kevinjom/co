package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kevinjom/co/internal/llm"
)

func TestCleanTitleKeepsTheFirstLine(t *testing.T) {
	got := cleanTitle(" \"Fix the parser.\" \nignore this")
	if got != "Fix the parser" {
		t.Fatalf("title %q", got)
	}
	long := "one two three four five six seven eight"
	if cleanTitle(long) != "one two three four five six" {
		t.Fatalf("title %q", cleanTitle(long))
	}
	if cleanTitle("   ") != "" {
		t.Fatal("blank title")
	}
}

func TestTitleStartsBeforeTheReply(t *testing.T) {
	ready := make(chan struct{})
	chat := &titleChat{
		title: "Fix the parser.",
		onTitle: func() {
			close(ready)
		},
		onTurn: func() {
			select {
			case <-ready:
			case <-time.After(2 * time.Second):
				t.Error("title request did not start")
			}
		},
	}
	session := NewSession(chat, "test-model", t.TempDir(), &titleStore{})
	got, err := session.Turn(t.Context(), "please fix the parser", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("answer %q", got)
	}
}

func TestFirstTurnStoresATitle(t *testing.T) {
	chat := &titleChat{title: "Fix the parser."}
	rec := &titleStore{}
	session := NewSession(chat, "test-model", t.TempDir(), rec)
	got, err := session.Turn(t.Context(), "please fix the parser", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("answer %q", got)
	}
	session.Wait()
	if rec.title != "Fix the parser" || session.Title() != "Fix the parser" {
		t.Fatalf("title %q session %q", rec.title, session.Title())
	}
	if _, err := session.Turn(t.Context(), "next", nil); err != nil {
		t.Fatal(err)
	}
	session.Wait()
	if chat.titles != 1 || chat.turns != 2 {
		t.Fatalf("title calls %d turn calls %d", chat.titles, chat.turns)
	}
}

func TestRestoredSessionIsNotRenamed(t *testing.T) {
	chat := &titleChat{title: "Should not run"}
	rec := &titleStore{}
	session := NewSession(chat, "test-model", t.TempDir(), rec)
	session.Restore([]llm.Message{{Role: "user", Content: "old"}})
	if _, err := session.Turn(t.Context(), "next", nil); err != nil {
		t.Fatal(err)
	}
	session.Wait()
	if chat.titles != 0 || rec.title != "" {
		t.Fatalf("titles %d stored %q", chat.titles, rec.title)
	}
}

func TestTitleFailureLeavesTheTurn(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	chat := &titleChat{titleErr: errors.New("title down")}
	rec := &titleStore{}
	session := NewSession(chat, "test-model", t.TempDir(), rec)
	got, err := session.Turn(t.Context(), "please fix the parser", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("answer %q", got)
	}
	session.Wait()
	if rec.title != "please fix the parser" {
		t.Fatalf("title %q", rec.title)
	}
}

type titleChat struct {
	mu       sync.Mutex
	title    string
	titleErr error
	titles   int
	turns    int
	onTitle  func()
	onTurn   func()
}

func (c *titleChat) ChatStream(
	_ context.Context,
	_ string,
	messages []llm.Message,
	_ []llm.Tool,
	onChunk func(llm.StreamKind, string),
) (llm.Message, error) {
	c.mu.Lock()
	titleReq := len(messages) > 0 && strings.Contains(messages[0].Content, "Reply with a short title")
	if titleReq {
		c.titles++
		err := c.titleErr
		title := c.title
		hold := c.onTitle
		c.mu.Unlock()
		if hold != nil {
			hold()
		}
		if err != nil {
			return llm.Message{}, err
		}
		return llm.Message{Role: "assistant", Content: title}, nil
	}
	c.turns++
	hold := c.onTurn
	c.mu.Unlock()
	if hold != nil {
		hold()
	}
	if onChunk != nil {
		onChunk(llm.StreamContent, "ok")
	}
	return llm.Message{Role: "assistant", Content: "ok"}, nil
}

type titleStore struct {
	memRecorder
	title string
}

func (s *titleStore) RecordTitle(title string) error {
	s.title = title
	return nil
}
