package transcript

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kevinjom/co/internal/llm"
)

func TestRecordShape(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	w, err := Open(home, dir, "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.path); !os.IsNotExist(err) {
		t.Fatalf("file created before Record: %v", err)
	}

	user := llm.Message{Role: "user", Content: "hi"}
	assistant := llm.Message{Role: "assistant", Content: "partial"}
	if err := w.Record(user, false); err != nil {
		t.Fatal(err)
	}
	if err := w.Record(assistant, true); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(w.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Dir(w.path))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", dirInfo.Mode().Perm())
	}

	lines := readLines(t, w.path)
	if len(lines) != 3 {
		t.Fatalf("lines = %d", len(lines))
	}
	var header struct {
		Type  string `json:"type"`
		V     int    `json:"v"`
		ID    string `json:"id"`
		Cwd   string `json:"cwd"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header.Type != "session" || header.V != 1 || header.Model != "test-model" || header.ID == "" {
		t.Fatalf("header %+v", header)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if header.Cwd != resolved {
		t.Fatalf("cwd %q want %q", header.Cwd, resolved)
	}

	for i, line := range lines[1:] {
		var rec struct {
			Type        string      `json:"type"`
			V           int         `json:"v"`
			Seq         int         `json:"seq"`
			TS          string      `json:"ts"`
			Interrupted bool        `json:"interrupted"`
			Message     llm.Message `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		if rec.Type != "message" || rec.V != 1 || rec.Seq != i+1 || rec.TS == "" {
			t.Fatalf("record %+v", rec)
		}
		if !json.Valid([]byte(line)) {
			t.Fatalf("invalid json %s", line)
		}
	}

	var first struct {
		Interrupted bool        `json:"interrupted"`
		Message     llm.Message `json:"message"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Interrupted || first.Message.Role != "user" || first.Message.Content != "hi" {
		t.Fatalf("user %+v", first)
	}
	var second struct {
		Interrupted bool        `json:"interrupted"`
		Message     llm.Message `json:"message"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &second); err != nil {
		t.Fatal(err)
	}
	if !second.Interrupted || second.Message.Content != "partial" || len(second.Message.ToolCalls) != 0 {
		t.Fatalf("assistant %+v", second)
	}
}

func TestProjectNameReplacesSeparators(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := Open(home, dir, "m")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(resolved, string(os.PathSeparator), "-")
	if !strings.Contains(w.path, filepath.Join("projects", want, "sessions")) {
		t.Fatalf("path %s want project %s", w.path, want)
	}
	if strings.Contains(want, string(os.PathSeparator)) {
		t.Fatalf("project name kept a separator: %s", want)
	}
}

func TestProjectNameFollowsSymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "real")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	real, err := Open(home, target, "m")
	if err != nil {
		t.Fatal(err)
	}
	via, err := Open(home, link, "m")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(real.path) != filepath.Dir(via.path) {
		t.Fatalf("real %s link %s", real.path, via.path)
	}
}

func TestRecordFailsWhenParentIsAFile(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".co"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	w, err := Open(home, t.TempDir(), "m")
	if err != nil {
		t.Fatal(err)
	}
	err = w.Record(llm.Message{Role: "user", Content: "hi"}, false)
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestRecordModelLeavesHeader(t *testing.T) {
	home := t.TempDir()
	w, err := Open(home, t.TempDir(), "ollama/first")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Record(llm.Message{Role: "user", Content: "hi"}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.RecordModel("openrouter/qwen/qwen3-coder"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	lines := readLines(t, w.path)
	if len(lines) != 3 {
		t.Fatalf("lines = %d", len(lines))
	}
	var header struct {
		Type  string `json:"type"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header.Type != "session" || header.Model != "ollama/first" {
		t.Fatalf("header %+v", header)
	}
	var change struct {
		Type  string `json:"type"`
		V     int    `json:"v"`
		Seq   int    `json:"seq"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &change); err != nil {
		t.Fatal(err)
	}
	if change.Type != "model" || change.V != 1 || change.Seq != 2 || change.Model != "openrouter/qwen/qwen3-coder" {
		t.Fatalf("change %+v", change)
	}
}

func TestResumeAppendsWithoutRewritingTheHeader(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	w, err := Open(home, dir, "ollama/first")
	if err != nil {
		t.Fatal(err)
	}
	call := llm.Message{
		Role: "assistant",
		ToolCalls: []llm.ToolCall{{
			ID:   "call_1",
			Type: "function",
			Function: llm.FunctionCall{
				Name:      "list_dir",
				Arguments: llm.Arguments{"path": "."},
			},
		}},
	}
	if err := w.Record(llm.Message{Role: "user", Content: "hi"}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.Record(call, false); err != nil {
		t.Fatal(err)
	}
	if err := w.RecordModel("openrouter/qwen/qwen3-coder"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	loaded, err := Resume(home, dir, w.ID()+".jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Model != "openrouter/qwen/qwen3-coder" {
		t.Fatalf("model %q", loaded.Model)
	}
	if loaded.Writer.model != "ollama/first" {
		t.Fatalf("header model %q", loaded.Writer.model)
	}
	if len(loaded.Messages) != 2 || loaded.Messages[1].ToolCalls[0].Function.Name != "list_dir" {
		t.Fatalf("messages %+v", loaded.Messages)
	}
	path, _ := loaded.Messages[1].ToolCalls[0].Function.Arguments["path"].(string)
	if path != "." {
		t.Fatalf("path %q", path)
	}
	if err := loaded.Writer.Record(llm.Message{Role: "user", Content: "next"}, false); err != nil {
		t.Fatal(err)
	}
	if err := loaded.Writer.Close(); err != nil {
		t.Fatal(err)
	}

	lines := readLines(t, w.path)
	if len(lines) != 5 {
		t.Fatalf("lines = %d", len(lines))
	}
	var headers int
	for _, line := range lines {
		var kind struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &kind); err != nil {
			t.Fatal(err)
		}
		if kind.Type == "session" {
			headers++
		}
	}
	if headers != 1 {
		t.Fatalf("headers = %d", headers)
	}
	var last struct {
		Seq     int `json:"seq"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(lines[4]), &last); err != nil {
		t.Fatal(err)
	}
	if last.Seq != 4 || last.Message.Content != "next" {
		t.Fatalf("last %+v", last)
	}
}

func TestResumeDropsATornTail(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	w, err := Open(home, dir, "ollama/first")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Record(llm.Message{Role: "user", Content: "hi"}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(w.path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"type":"message","broken`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	loaded, err := Resume(home, dir, w.ID())
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.Writer.Record(llm.Message{Role: "user", Content: "next"}, false); err != nil {
		t.Fatal(err)
	}
	if err := loaded.Writer.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(w.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("broken")) {
		t.Fatalf("torn tail kept: %s", data)
	}
	lines := readLines(t, w.path)
	if len(lines) != 3 {
		t.Fatalf("lines = %d file %s", len(lines), data)
	}
}

func TestResumeLeavesACorruptLine(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	w, err := Open(home, dir, "ollama/first")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Record(llm.Message{Role: "user", Content: "hi"}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.Record(llm.Message{Role: "assistant", Content: "ok"}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, w.path)
	lines[1] = "not-json"
	corrupt := []byte(strings.Join(lines, "\n") + "\n")
	if err := os.WriteFile(w.path, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resume(home, dir, w.ID()); err == nil {
		t.Fatal("expected an error")
	}
	got, err := os.ReadFile(w.path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, corrupt) {
		t.Fatalf("file changed: %s", got)
	}
}

func TestResumeLatestUsesTheNewestFile(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	older, err := Open(home, dir, "ollama/first")
	if err != nil {
		t.Fatal(err)
	}
	if err := older.Record(llm.Message{Role: "user", Content: "old"}, false); err != nil {
		t.Fatal(err)
	}
	if err := older.Close(); err != nil {
		t.Fatal(err)
	}
	newer, err := Open(home, dir, "ollama/first")
	if err != nil {
		t.Fatal(err)
	}
	if err := newer.Record(llm.Message{Role: "user", Content: "new"}, false); err != nil {
		t.Fatal(err)
	}
	if err := newer.Close(); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(older.path, past, past); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer.path, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}

	loaded, err := Resume(home, dir, "latest")
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Writer.Close()
	if loaded.Writer.ID() != newer.ID() {
		t.Fatalf("id %s want %s", loaded.Writer.ID(), newer.ID())
	}
	if len(loaded.Messages) != 1 || loaded.Messages[0].Content != "new" {
		t.Fatalf("messages %+v", loaded.Messages)
	}
}

func TestResumeRejectsBadInput(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	assertInput := func(id, want string) {
		t.Helper()
		_, err := Resume(home, dir, id)
		var input *InputError
		if !errors.As(err, &input) || !strings.Contains(err.Error(), want) {
			t.Fatalf("Resume %q err = %v", id, err)
		}
	}
	assertInput("latest", "no session")
	assertInput("../secret", "invalid session")
	assertInput("missing", "not found")
}

func TestResumeRejectsADifferentDirectory(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	w, err := Open(home, dir, "ollama/first")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Record(llm.Message{Role: "user", Content: "hi"}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, w.path)
	var header map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatal(err)
	}
	header["cwd"] = "/somewhere/else"
	raw, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	body := append(append(raw, '\n'), []byte(strings.Join(lines[1:], "\n")+"\n")...)
	if err := os.WriteFile(w.path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Resume(home, dir, w.ID())
	var input *InputError
	if !errors.As(err, &input) || !strings.Contains(err.Error(), "belongs to") {
		t.Fatalf("err = %v", err)
	}
}

func TestResumeAttachesSkillToItsMessage(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	w, err := Open(home, dir, "test-model")
	if err != nil {
		t.Fatal(err)
	}
	const typed = "/grilling fix the parser"
	const body = "---\nname: grilling\ndescription: Grill a plan.\n---\nAsk one question.\n"
	if err := w.Record(llm.Message{Role: "user", Content: typed}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.RecordSkillUse("grilling", ".agents/skills/grilling", body); err != nil {
		t.Fatal(err)
	}
	if err := w.Record(llm.Message{Role: "assistant", Content: "What should the parser accept?"}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	loaded, err := Resume(home, dir, w.ID())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Writer.Close() })
	if len(loaded.Messages) != 2 || loaded.Messages[0].Content != typed {
		t.Fatalf("messages %+v", loaded.Messages)
	}
	if len(loaded.Skills) != 0 {
		t.Fatalf("names %v", loaded.Skills)
	}
	if len(loaded.MessageSkills) != 2 || len(loaded.MessageSkills[1]) != 0 {
		t.Fatalf("skills %#v", loaded.MessageSkills)
	}
	got := loaded.MessageSkills[0]
	if len(got) != 1 || got[0].Name != "grilling" || got[0].Dir != ".agents/skills/grilling" || got[0].Body != body {
		t.Fatalf("skill %+v", got)
	}

	lines := readLines(t, w.path)
	var skillLine struct {
		At   int    `json:"at"`
		Name string `json:"name"`
		Dir  string `json:"dir"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &skillLine); err != nil {
		t.Fatal(err)
	}
	if skillLine.At != 1 || skillLine.Name != "grilling" || skillLine.Body != body {
		t.Fatalf("record %+v", skillLine)
	}
	if skillLine.Dir != ".agents/skills/grilling" {
		t.Fatalf("dir %q", skillLine.Dir)
	}
}

func TestRecordSkillUseNeedsAMessage(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	w, err := Open(home, dir, "test-model")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	err = w.RecordSkillUse("grilling", "", "Ask one question.\n")
	if err == nil || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("err = %v", err)
	}
}

func TestResumeRejectsASkillWithoutItsMessage(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	w, err := Open(home, dir, "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Record(llm.Message{Role: "user", Content: "hi"}, false); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	extra := []byte(strings.Join([]string{
		`{"type":"skill","v":1,"seq":2,"ts":"2026-11-12T00:00:00Z",`,
		`"at":9,"name":"grilling","body":"Ask.\n"}`,
	}, "") + "\n")
	raw, err := os.ReadFile(w.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(w.path, append(raw, extra...), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Resume(home, dir, w.ID())
	var input *InputError
	if !errors.As(err, &input) || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("err = %v", err)
	}
}

func TestResumeKeepsSkillNames(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	w, err := Open(home, dir, "test-model")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.RecordSkill("grilling"); err != nil {
		t.Fatal(err)
	}
	if err := w.RecordSkill("notes"); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	loaded, err := Resume(home, dir, w.ID())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loaded.Writer.Close() })
	if len(loaded.Skills) != 2 || loaded.Skills[0] != "grilling" || loaded.Skills[1] != "notes" {
		t.Fatalf("skills %v", loaded.Skills)
	}
	if len(loaded.Messages) != 0 {
		t.Fatalf("messages %v", loaded.Messages)
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("missing trailing newline: %q", data)
	}
	text := strings.TrimSuffix(string(data), "\n")
	return strings.Split(text, "\n")
}
