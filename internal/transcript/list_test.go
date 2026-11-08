package transcript

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kevinjom/co/internal/llm"
)

func TestListReturnsNewestFirst(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	older := writeSession(t, home, dir, "ollama/first", "  old\nprompt  ")
	newer, err := Open(home, dir, "ollama/first")
	if err != nil {
		t.Fatal(err)
	}
	if err := newer.Record(llm.Message{Role: "user", Content: "new prompt"}, false); err != nil {
		t.Fatal(err)
	}
	if err := newer.RecordModel("openrouter/qwen/qwen3-coder"); err != nil {
		t.Fatal(err)
	}
	if err := newer.Close(); err != nil {
		t.Fatal(err)
	}
	past := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := past.Add(time.Hour)
	if err := os.Chtimes(older.path, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer.path, now, now); err != nil {
		t.Fatal(err)
	}

	got, err := List(home, dir, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("sessions = %d", len(got))
	}
	if got[0].ID != newer.ID() || got[0].Model != "openrouter/qwen/qwen3-coder" || got[0].Title != "new prompt" {
		t.Fatalf("newest %+v", got[0])
	}
	if !got[0].Updated.Equal(now) {
		t.Fatalf("updated %s", got[0].Updated)
	}
	if got[1].ID != older.ID() || got[1].Title != "old" || got[1].Model != "ollama/first" {
		t.Fatalf("older %+v", got[1])
	}

	loaded, err := Resume(home, dir, "latest")
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Writer.Close()
	if loaded.Writer.ID() != got[0].ID {
		t.Fatalf("latest %s list %s", loaded.Writer.ID(), got[0].ID)
	}
}

func TestListHonorsLimit(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	var newest string
	for i := range 3 {
		w := writeSession(t, home, dir, "m", "prompt")
		when := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(w.path, when, when); err != nil {
			t.Fatal(err)
		}
		newest = w.ID()
	}
	got, err := List(home, dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != newest {
		t.Fatalf("sessions %+v", got)
	}
}

func TestListBreaksModTimeTiesByID(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	first := writeSession(t, home, dir, "m", "one")
	second := writeSession(t, home, dir, "m", "two")
	when := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	low := filepath.Join(filepath.Dir(first.path), "a.jsonl")
	high := filepath.Join(filepath.Dir(first.path), "b.jsonl")
	if err := os.Rename(first.path, low); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(second.path, high); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(low, when, when); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(high, when, when); err != nil {
		t.Fatal(err)
	}

	got, err := List(home, dir, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("sessions %+v", got)
	}
}

func TestListSkipsACorruptFile(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	good := writeSession(t, home, dir, "m", "keep me")
	dirPath := filepath.Dir(good.path)
	if err := os.WriteFile(filepath.Join(dirPath, "notes.txt"), []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dirPath, "subdir.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "bad id.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, "broken.jsonl"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	prev := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	got, err := List(home, dir, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != good.ID() || got[0].Title != "keep me" {
		t.Fatalf("sessions %+v", got)
	}
	if !strings.Contains(logs.String(), "broken") {
		t.Fatalf("log %q", logs.String())
	}
}

func TestListUsesTheFirstLineWhenThereIsNoTitle(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	prompt := "one two three four five six seven eight nine ten eleven twelve"
	writeSession(t, home, dir, "m", prompt+"\nmore words")
	got, err := List(home, dir, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "one two three four five six seven eight nine ten" {
		t.Fatalf("title %q", got[0].Title)
	}
}

func TestListPrefersTheStoredTitle(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	w, err := Open(home, dir, "m")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Record(llm.Message{Role: "user", Content: "please fix the parser and add tests"}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.RecordTitle("Fix the parser"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := List(home, dir, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Fix the parser" {
		t.Fatalf("title %+v", got)
	}

	loaded, err := Resume(home, dir, w.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Writer.Close()
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "please fix the parser and add tests" {
		t.Fatalf("messages %+v", loaded.Messages)
	}
}

func TestListEmptyDirectory(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	got, err := List(home, dir, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("sessions %+v", got)
	}

	other := t.TempDir()
	writeSession(t, home, other, "m", "elsewhere")
	got, err = List(home, dir, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("sessions %+v", got)
	}
}

func TestListRejectsBadArguments(t *testing.T) {
	dir := t.TempDir()
	if _, err := List("  ", dir, 20); err == nil {
		t.Fatal("empty home")
	}
	if _, err := List(t.TempDir(), dir, 0); err == nil {
		t.Fatal("zero limit")
	}
}

func writeSession(t *testing.T, home, dir, model, prompt string) *Writer {
	t.Helper()
	w, err := Open(home, dir, model)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Record(llm.Message{Role: "user", Content: prompt}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.Record(llm.Message{Role: "assistant", Content: "reply"}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return w
}
