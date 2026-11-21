package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAskUserDeliversTheQuestion(t *testing.T) {
	set := New(t.TempDir())
	var got Question
	set.SetAsker(func(_ context.Context, question Question) (string, error) {
		got = question
		return "postgres\nsqlite\n\nalso redis", nil
	})

	out, err := set.Run(context.Background(), "ask_user", map[string]any{
		"question": " Which database? ",
		"options":  []any{"postgres", "sqlite"},
		"multiple": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != "postgres\nsqlite\n\nalso redis" {
		t.Fatalf("answer %q", out)
	}
	if got.Text != "Which database?" || !got.Multiple || len(got.Options) != 2 || got.Options[0] != "postgres" || got.Options[1] != "sqlite" {
		t.Fatalf("question %+v", got)
	}
	if Detail("ask_user", map[string]any{"question": "Which database?\nplease"}) != "Which database? please" {
		t.Fatal(Detail("ask_user", map[string]any{"question": "Which database?\nplease"}))
	}
}

func TestAskUserRejectsBadCalls(t *testing.T) {
	set := New(t.TempDir())
	set.SetAsker(func(context.Context, Question) (string, error) {
		t.Fatal("asker should not run")
		return "", nil
	})
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "missing question", args: map[string]any{}, want: "question is required"},
		{name: "blank question", args: map[string]any{"question": "  "}, want: "question is required"},
		{name: "options type", args: map[string]any{"question": "Pick", "options": "postgres"}, want: "options: expected an array of strings"},
		{name: "option type", args: map[string]any{"question": "Pick", "options": []any{"postgres", 2}}, want: "options: expected an array of strings"},
		{name: "blank option", args: map[string]any{"question": "Pick", "options": []any{"  "}}, want: "options: each option must be one line"},
		{name: "multiline option", args: map[string]any{"question": "Pick", "options": []any{"a\nb"}}, want: "options: each option must be one line"},
		{name: "repeat", args: map[string]any{"question": "Pick", "options": []any{"postgres", "postgres"}}, want: `options: "postgres" is repeated`},
		{name: "multiple type", args: map[string]any{"question": "Pick", "multiple": "true"}, want: "multiple: expected a boolean"},
		{name: "multiple without options", args: map[string]any{"question": "Pick", "multiple": true}, want: "options are required when multiple is set"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := set.Run(context.Background(), "ask_user", tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(out, tt.want) {
				t.Fatalf("out %q err %v", out, err)
			}
		})
	}
}

func TestAskUserNeedsATerminal(t *testing.T) {
	set := New(t.TempDir())
	out, err := set.Run(context.Background(), "ask_user", map[string]any{"question": "Pick one"})
	if err == nil || !strings.Contains(err.Error(), "terminal") || !strings.Contains(out, "terminal") {
		t.Fatalf("out %q err %v", out, err)
	}
}

func TestAskUserEmptyAnswer(t *testing.T) {
	set := New(t.TempDir())
	set.SetAsker(func(context.Context, Question) (string, error) {
		return "  ", nil
	})
	_, err := set.Run(context.Background(), "ask_user", map[string]any{"question": "Pick one"})
	if err == nil || !strings.Contains(err.Error(), "empty answer") {
		t.Fatal(err)
	}
}

func TestAskUserReturnsAskerError(t *testing.T) {
	set := New(t.TempDir())
	set.SetAsker(func(context.Context, Question) (string, error) {
		return "", errors.New("closed")
	})
	out, err := set.Run(context.Background(), "ask_user", map[string]any{"question": "Pick one"})
	if err == nil || !strings.Contains(err.Error(), "closed") || !strings.Contains(out, "closed") {
		t.Fatalf("out %q err %v", out, err)
	}
}

func TestFormatAnswer(t *testing.T) {
	options := []string{"postgres", "sqlite", "mysql"}
	tests := []struct {
		name   string
		picked []int
		typed  string
		want   string
	}{
		{name: "one option", picked: []int{1}, want: "sqlite"},
		{name: "several", picked: []int{0, 2}, want: "postgres\nmysql"},
		{name: "typed", typed: " redis ", want: "redis"},
		{name: "both", picked: []int{0, 0, 1}, typed: "and redis", want: "postgres\nsqlite\n\nand redis"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FormatAnswer(options, tt.picked, tt.typed)
			if err != nil || got != tt.want {
				t.Fatalf("got %q err %v", got, err)
			}
		})
	}
	if _, err := FormatAnswer(options, nil, "  "); err == nil {
		t.Fatal("empty answer")
	}
	if _, err := FormatAnswer(options, []int{3}, ""); err == nil {
		t.Fatal("out of range")
	}
}
