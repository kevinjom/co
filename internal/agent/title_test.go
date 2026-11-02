package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/kevinjom/co/internal/openai"
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
	session.Restore([]openai.Message{{Role: "user", Content: "old"}})
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
}

func (c *titleChat) ChatStream(
	_ context.Context,
	_ string,
	messages []openai.Message,
	_ []openai.Tool,
	onChunk func(openai.StreamKind, string),
) (openai.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(messages) > 0 && strings.Contains(messages[0].Content, "Reply with a short title") {
		c.titles++
		if c.titleErr != nil {
			return openai.Message{}, c.titleErr
		}
		return openai.Message{Role: "assistant", Content: c.title}, nil
	}
	c.turns++
	if onChunk != nil {
		onChunk(openai.StreamContent, "ok")
	}
	return openai.Message{Role: "assistant", Content: "ok"}, nil
}

type titleStore struct {
	memRecorder
	title string
}

func (s *titleStore) RecordTitle(title string) error {
	s.title = title
	return nil
}
