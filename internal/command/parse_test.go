package command

import "testing"

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
		{line: "/connect", kind: KindError},
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
