package command

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	cases := []struct {
		line string
		kind Kind
		arg  string
	}{
		{line: "hello", kind: KindChat, arg: "hello"},
		{line: "exit", kind: KindQuit},
		{line: "quit", kind: KindQuit},
		{line: "/models", kind: KindModels},
		{line: "/models openrouter/qwen/qwen3-coder", kind: KindModels, arg: "openrouter/qwen/qwen3-coder"},
		{line: "/connect openrouter", kind: KindConnect, arg: "openrouter"},
		{line: "/connect", kind: KindConnect},
		{line: "/connect open router", kind: KindError},
		{line: "/new", kind: KindNew},
		{line: "/new extra", kind: KindError},
		{line: "/sessions", kind: KindSessions},
		{line: "/sessions 2", kind: KindError},
		{line: "/theme", kind: KindTheme},
		{line: "/theme rose", kind: KindTheme, arg: "rose"},
		{line: "/theme rose extra", kind: KindError},
		{line: "/nope", kind: KindError},
	}
	for _, tc := range cases {
		got := Parse(tc.line)
		if got.Kind != tc.kind || got.Arg != tc.arg {
			t.Fatalf("parse %q: kind %d arg %q err %v", tc.line, got.Kind, got.Arg, got.Err)
		}
		if tc.kind == KindError && got.Err == nil {
			t.Fatalf("parse %q: missing error", tc.line)
		}
	}
}

func TestSlashCommandsAreKnown(t *testing.T) {
	for _, name := range slashCommands() {
		got := Parse(name + " x")
		if got.Kind == KindChat {
			t.Fatalf("%s parsed as chat", name)
		}
		if got.Err != nil && strings.Contains(got.Err.Error(), "unknown command") {
			t.Fatalf("%s is unknown", name)
		}
	}
}

func TestComplete(t *testing.T) {
	cases := []struct {
		stem  string
		index int
		value string
		names []string
	}{
		{stem: "/m", value: "/models", names: []string{"/models"}},
		{stem: "/c", value: "/connect", names: []string{"/connect"}},
		{stem: "/n", value: "/new", names: []string{"/new"}},
		{stem: "/s", value: "/sessions", names: []string{"/sessions"}},
		{stem: "/t", value: "/theme", names: []string{"/theme"}},
		{stem: "/", value: "/models", names: []string{"/models", "/connect", "/new", "/sessions", "/theme"}},
		{stem: "/", index: 1, value: "/connect", names: []string{"/models", "/connect", "/new", "/sessions", "/theme"}},
		{stem: "/", index: 2, value: "/new", names: []string{"/models", "/connect", "/new", "/sessions", "/theme"}},
		{stem: "/", index: 4, value: "/theme", names: []string{"/models", "/connect", "/new", "/sessions", "/theme"}},
		{stem: "/", index: 5, value: "/models", names: []string{"/models", "/connect", "/new", "/sessions", "/theme"}},
		{stem: "/models", names: []string{"/models"}},
		{stem: "/connect", names: []string{"/connect"}},
		{stem: "/nope"},
		{stem: "models"},
		{stem: "/models ollama/qwen"},
		{stem: ""},
	}
	for _, tc := range cases {
		got := Complete(tc.stem, tc.index)
		if got.Value != tc.value || len(got.Matches) != len(tc.names) {
			t.Fatalf("complete %q #%d: value %q matches %v", tc.stem, tc.index, got.Value, got.Matches)
		}
		for i, name := range tc.names {
			if got.Matches[i] != name {
				t.Fatalf("complete %q #%d: matches %v", tc.stem, tc.index, got.Matches)
			}
		}
	}
}
