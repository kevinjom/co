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
		{line: "/skill", kind: KindSkill},
		{line: "/skill grilling", kind: KindChat, arg: "/skill grilling"},
		{line: "/skill grilling extra", kind: KindChat, arg: "/skill grilling extra"},
		{line: "/skill nope", kind: KindError},
		{line: "/grilling", kind: KindChat, arg: "/grilling"},
		{line: "/grilling now", kind: KindChat, arg: "/grilling now"},
		{line: "/nope", kind: KindError},
	}
	skills := []string{"grilling"}
	for _, tc := range cases {
		got := Parse(tc.line, skills)
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
		got := Parse(name+" x", nil)
		if got.Kind == KindChat {
			t.Fatalf("%s parsed as chat", name)
		}
		if got.Err != nil && strings.Contains(got.Err.Error(), "unknown command") {
			t.Fatalf("%s is unknown", name)
		}
	}
}

func TestComplete(t *testing.T) {
	commands := []string{"/models", "/connect", "/new", "/sessions", "/theme", "/skill"}
	cases := []struct {
		stem   string
		index  int
		value  string
		names  []string
		skills []string
	}{
		{stem: "/m", value: "/models", names: []string{"/models"}},
		{stem: "/c", value: "/connect", names: []string{"/connect"}},
		{stem: "/n", value: "/new", names: []string{"/new"}},
		{stem: "/s", value: "/sessions", names: []string{"/sessions", "/skill"}},
		{stem: "/t", value: "/theme", names: []string{"/theme"}},
		{stem: "/", value: "/models", names: commands},
		{stem: "/", index: 1, value: "/connect", names: commands},
		{stem: "/", index: 2, value: "/new", names: commands},
		{stem: "/", index: 4, value: "/theme", names: commands},
		{stem: "/", index: 5, value: "/skill", names: commands},
		{stem: "/", index: 6, value: "/models", names: commands},
		{stem: "/models", names: []string{"/models"}},
		{stem: "/connect", names: []string{"/connect"}},
		{stem: "/nope"},
		{stem: "models"},
		{stem: "/models ollama/qwen"},
		{stem: ""},
		{
			stem:   "/",
			value:  "/models",
			names:  []string{"/models", "/connect", "/new", "/sessions", "/theme", "/skill", "/grilling", "/notes"},
			skills: []string{"notes", "grilling"},
		},
		{
			stem:   "/g",
			value:  "/grilling",
			names:  []string{"/grilling"},
			skills: []string{"grilling", "notes"},
		},
	}
	for _, tc := range cases {
		got := Complete(tc.stem, tc.index, tc.skills)
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

func TestCompleteOmitsAnEmptySkillsGroup(t *testing.T) {
	got := Complete("/", 0, nil)
	if len(got.Groups) != 1 || got.Groups[0].Heading != "commands" {
		t.Fatalf("groups %+v", got.Groups)
	}
}

func TestCompleteGroupsSkills(t *testing.T) {
	got := Complete("/", 0, []string{"grilling"})
	if len(got.Groups) != 2 || got.Groups[0].Heading != "commands" || got.Groups[1].Heading != "skills" {
		t.Fatalf("groups %+v", got.Groups)
	}
	if len(got.Groups[1].Matches) != 1 || got.Groups[1].Matches[0] != "/grilling" {
		t.Fatalf("skills %+v", got.Groups[1])
	}
}
