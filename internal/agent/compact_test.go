package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevinjom/co/internal/llm"
	"github.com/kevinjom/co/internal/skill"
	"github.com/kevinjom/co/internal/transcript"
)

func TestCompactReplacesContextAndKeepsHistory(t *testing.T) {
	chat := &replayChat{replies: []llm.Message{
		{Role: "assistant", Content: "noted"},
		{Role: "assistant", Content: "Keep the file list task."},
		{Role: "assistant", Content: "still going"},
		{Role: "assistant", Content: "Now they want to continue."},
	}}
	home := t.TempDir()
	dir := t.TempDir()
	session := NewSession(chat, "m", dir, nil)
	if _, err := session.Turn(t.Context(), "list files", nil); err != nil {
		t.Fatal(err)
	}
	summary, err := session.Compact(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if summary != "Keep the file list task." {
		t.Fatalf("summary %q", summary)
	}
	assertMessages(t, session.Messages(), []llm.Message{
		{Role: "user", Content: "list files"},
		{Role: "assistant", Content: "noted"},
		{Role: SummaryRole, Content: "Keep the file list task."},
	})
	if strings.Contains(joined(session.Messages()), compactInstruction) {
		t.Fatal("summary request was stored")
	}
	first := chat.got[1]
	if !first.toolsNil {
		t.Fatal("summary request included tools")
	}
	if !strings.Contains(joined(first.messages), "list files") || !strings.Contains(joined(first.messages), compactInstruction) {
		t.Fatalf("summary request %+v", first.messages)
	}
	if first.messages[len(first.messages)-1].Content != compactInstruction {
		t.Fatalf("instruction %+v", first.messages[len(first.messages)-1])
	}

	if _, err := session.Turn(t.Context(), "continue", nil); err != nil {
		t.Fatal(err)
	}
	follow := chat.got[2]
	if strings.Contains(joined(follow.messages), "list files") || strings.Contains(joined(follow.messages), "noted") {
		t.Fatalf("follow-up sent the summarized turns %+v", follow.messages)
	}
	if !strings.Contains(joined(follow.messages), summaryPrefix+"Keep the file list task.") {
		t.Fatalf("follow-up missed the summary %+v", follow.messages)
	}
	if follow.messages[len(follow.messages)-1].Content != "continue" {
		t.Fatalf("follow-up tail %+v", follow.messages[len(follow.messages)-1])
	}

	if _, err := session.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	again := chat.got[3]
	if strings.Contains(joined(again.messages), "list files") || strings.Contains(joined(again.messages), "noted") {
		t.Fatalf("second summary saw the original turns %+v", again.messages)
	}
	if !strings.Contains(joined(again.messages), "Keep the file list task.") || !strings.Contains(joined(again.messages), "continue") {
		t.Fatalf("second summary missed the current context %+v", again.messages)
	}
	if _, err := session.Compact(t.Context()); err == nil || !strings.Contains(err.Error(), "nothing to compact") {
		t.Fatalf("third compact: %v", err)
	}
	if len(chat.got) != 4 {
		t.Fatalf("calls %d", len(chat.got))
	}

	kept := append([]llm.Message{}, session.Messages()...)
	writer, err := transcript.Open(home, dir, "m")
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range kept[1:] {
		if err := writer.Record(msg, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, err := transcript.Resume(home, dir, writer.ID())
	if err != nil {
		t.Fatal(err)
	}
	if joined(loaded.Messages) != joined(kept[1:]) {
		t.Fatalf("resumed %+v\nstored %+v", loaded.Messages, kept[1:])
	}
	next := NewSession(chat, "m", dir, nil)
	next.Restore(loaded.Messages)
	chat.replies = []llm.Message{{Role: "assistant", Content: "ok"}}
	if _, err := next.Turn(t.Context(), "after resume", nil); err != nil {
		t.Fatal(err)
	}
	resumed := joined(chat.got[len(chat.got)-1].messages)
	for _, gone := range []string{"list files", "noted", "Keep the file list task.", "still going"} {
		if strings.Contains(resumed, gone) {
			t.Fatalf("resumed request still has %q\n%s", gone, resumed)
		}
	}
	if !strings.Contains(resumed, "Now they want to continue.") || !strings.Contains(resumed, "after resume") {
		t.Fatalf("resumed request %s", resumed)
	}
}

func TestCompactRefusesAnEmptySession(t *testing.T) {
	chat := &replayChat{}
	session := NewSession(chat, "m", t.TempDir(), nil)
	if _, err := session.Compact(t.Context()); err == nil || !strings.Contains(err.Error(), "nothing to compact") {
		t.Fatalf("err %v", err)
	}
	if len(chat.got) != 0 || len(session.Messages()) != 1 {
		t.Fatalf("calls %d messages %d", len(chat.got), len(session.Messages()))
	}
}

func TestCompactRefusesAnOpenToolRound(t *testing.T) {
	chat := &replayChat{}
	session := NewSession(chat, "m", t.TempDir(), nil)
	session.Restore([]llm.Message{
		{Role: "user", Content: "go"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID:       "c1",
			Function: llm.FunctionCall{Name: "list_dir"},
		}}},
	})
	if _, err := session.Compact(t.Context()); err == nil || !strings.Contains(err.Error(), "wait for the turn to finish") {
		t.Fatalf("err %v", err)
	}
	if len(chat.got) != 0 {
		t.Fatalf("calls %d", len(chat.got))
	}
}

func TestCompactDropsAFailedSummary(t *testing.T) {
	chat := &replayChat{err: errors.New("offline")}
	session := NewSession(chat, "m", t.TempDir(), nil)
	session.Restore([]llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	})
	before := len(session.Messages())
	if _, err := session.Compact(t.Context()); err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("err %v", err)
	}
	if len(session.Messages()) != before {
		t.Fatalf("messages %d", len(session.Messages()))
	}
}

func TestCompactDropsAnEmptySummary(t *testing.T) {
	chat := &replayChat{replies: []llm.Message{{Role: "assistant", Content: "  \n"}}}
	session := NewSession(chat, "m", t.TempDir(), nil)
	session.Restore([]llm.Message{{Role: "user", Content: "hi"}})
	if _, err := session.Compact(t.Context()); err == nil || !strings.Contains(err.Error(), "empty summary") {
		t.Fatalf("err %v", err)
	}
	if session.Messages()[len(session.Messages())-1].Role != "user" {
		t.Fatalf("messages %+v", session.Messages())
	}
}

func TestCompactKeepsAReasoningSummary(t *testing.T) {
	chat := &replayChat{replies: []llm.Message{{Role: "assistant", Content: "from reasoning"}}, reasoning: true}
	session := NewSession(chat, "m", t.TempDir(), nil)
	session.Restore([]llm.Message{{Role: "user", Content: "hi"}})
	got, err := session.Compact(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got != "from reasoning" {
		t.Fatalf("summary %q", got)
	}
}

func TestCompactIgnoresACanceledSummary(t *testing.T) {
	chat := &replayChat{}
	session := NewSession(chat, "m", t.TempDir(), nil)
	session.Restore([]llm.Message{{Role: "user", Content: "hi"}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := session.Compact(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if session.Messages()[len(session.Messages())-1].Role != "user" {
		t.Fatalf("messages %+v", session.Messages())
	}
}

func TestCompactCountsSummaryUsage(t *testing.T) {
	chat := &replayChat{replies: []llm.Message{{
		Role:    "assistant",
		Content: "sum",
		Usage:   &llm.Usage{Input: 40, Output: 5},
	}}}
	session := NewSession(chat, "m", t.TempDir(), nil)
	session.Restore([]llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello", Usage: &llm.Usage{Input: 10, Output: 2}},
	})
	if _, err := session.Compact(t.Context()); err != nil {
		t.Fatal(err)
	}
	const want = "user turns: 1\nassistant answers: 1\ntool calls: 0\ntokens: 50 in, 7 out\ncontext: 40 in"
	if got := session.Stats().Text(); got != want {
		t.Fatalf("stats\n%s", got)
	}
}

func TestCompactKeepsSkillsAfterTheSummary(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, ".agents", "skills", "grilling")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: grilling\ndescription: Grill.\n---\nAsk why.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	catalog := skill.Load(dir, t.TempDir())
	sk, ok := catalog.Get("grilling")
	if !ok {
		t.Fatal("skill")
	}
	chat := &replayChat{replies: []llm.Message{{Role: "assistant", Content: "ok"}}}
	session := NewSession(chat, "m", dir, nil)
	session.SetCatalog(catalog)
	session.Restore([]llm.Message{
		{Role: "user", Content: "ancient question"},
		{Role: SummaryRole, Content: "done with that part"},
		{Role: "user", Content: "/grilling go"},
	})
	session.BindSkills([][]skill.Skill{nil, nil, {sk}})
	if _, err := session.Turn(t.Context(), "next", nil); err != nil {
		t.Fatal(err)
	}
	req := joined(chat.got[0].messages)
	if strings.Contains(req, "ancient question") || !strings.Contains(req, "Ask why.") || !strings.Contains(req, "done with that part") {
		t.Fatalf("request %s", req)
	}
}

func assertMessages(t *testing.T, got []llm.Message, want []llm.Message) {
	t.Helper()
	if len(got) != len(want)+1 || got[0].Role != "system" {
		t.Fatalf("messages %+v", got)
	}
	for i, msg := range want {
		if got[i+1].Role != msg.Role || got[i+1].Content != msg.Content {
			t.Fatalf("message %d %+v", i, got[i+1])
		}
	}
}

func joined(messages []llm.Message) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		parts = append(parts, msg.Role+"\n"+msg.Content)
	}
	return strings.Join(parts, "\n")
}

type replayCall struct {
	messages []llm.Message
	toolsNil bool
}

type replayChat struct {
	replies   []llm.Message
	got       []replayCall
	err       error
	reasoning bool
}

func (c *replayChat) ChatStream(
	ctx context.Context,
	_ string,
	messages []llm.Message,
	tools []llm.Tool,
	onChunk func(llm.StreamKind, string),
) (llm.Message, error) {
	if err := ctx.Err(); err != nil {
		return llm.Message{}, err
	}
	saved := make([]llm.Message, len(messages))
	copy(saved, messages)
	c.got = append(c.got, replayCall{messages: saved, toolsNil: tools == nil})
	if c.err != nil {
		return llm.Message{}, c.err
	}
	if len(c.replies) == 0 {
		return llm.Message{}, errors.New("no reply")
	}
	msg := c.replies[0]
	c.replies = c.replies[1:]
	if c.reasoning {
		if onChunk != nil {
			onChunk(llm.StreamReasoning, msg.Content)
		}
		msg.Content = ""
		return msg, nil
	}
	if onChunk != nil && msg.Content != "" {
		onChunk(llm.StreamContent, msg.Content)
	}
	return msg, nil
}
