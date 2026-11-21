package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kevinjom/co/internal/tools"
)

func TestParseAskReply(t *testing.T) {
	t.Parallel()

	options := []string{"postgres", "sqlite", "mysql"}
	tests := []struct {
		name     string
		line     string
		options  []string
		multiple bool
		want     string
		ok       bool
		err      string
	}{
		{name: "empty", line: "  ", options: options},
		{name: "words only", line: "hello", want: "hello", ok: true},
		{name: "one number", line: "2", options: options, want: "sqlite", ok: true},
		{name: "words with options", line: "use redis", options: options, want: "use redis", ok: true},
		{name: "number and words", line: "1 -- and redis", options: options, want: "postgres\n\nand redis", ok: true},
		{name: "typed number", line: "-- 2", options: options, want: "2", ok: true},
		{name: "several", line: "1 3", options: options, multiple: true, want: "postgres\nmysql", ok: true},
		{name: "several and words", line: "1 3 -- nearby", options: options, multiple: true, want: "postgres\nmysql\n\nnearby", ok: true},
		{name: "duplicate numbers", line: "1 1", options: options, multiple: true, want: "postgres", ok: true},
		{name: "dash words stay words", line: "use a -- flag", options: options, want: "use a -- flag", ok: true},
		{name: "two numbers", line: "1 2", options: options, err: "pick one option"},
		{name: "out of range", line: "9", options: options, err: "option 9 is out of range"},
		{name: "words around a dash", line: "nope -- words", options: options, want: "nope -- words", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok, err := parseAskReply(tt.line, tt.options, tt.multiple)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) || ok {
					t.Fatalf("got %q ok %v err %v", got, ok, err)
				}
				return
			}
			if err != nil || ok != tt.ok || got != tt.want {
				t.Fatalf("got %q ok %v err %v", got, ok, err)
			}
		})
	}
}

func TestAskLineRepeatsUntilAnswered(t *testing.T) {
	var out bytes.Buffer
	replies := []string{"", "9", "2 -- nearby"}
	n := 0
	read := func(context.Context) (string, error) {
		if n >= len(replies) {
			return "", errors.New("no more replies")
		}
		line := replies[n]
		n++
		return line, nil
	}
	got, err := askLine(context.Background(), &out, read, tools.Question{
		Text:    "Which database?",
		Options: []string{"postgres", "sqlite"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "sqlite\n\nnearby" {
		t.Fatalf("answer %q", got)
	}
	text := out.String()
	for _, phrase := range []string{"Which database?", "1. postgres", "2. sqlite", "option 9 is out of range", "reply: "} {
		if !strings.Contains(text, phrase) {
			t.Fatalf("prompt missing %q\n%s", phrase, text)
		}
	}
}

func TestLineAskerNeedsATerminal(t *testing.T) {
	t.Parallel()

	asker := lineAsker(ioDiscard{}, func(context.Context) (string, error) {
		t.Fatal("read")
		return "", nil
	}, false)
	_, err := asker(context.Background(), tools.Question{Text: "Pick"})
	if err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatal(err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
