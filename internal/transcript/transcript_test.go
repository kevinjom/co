package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"co/internal/ollama"
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

	user := ollama.Message{Role: "user", Content: "hi"}
	assistant := ollama.Message{Role: "assistant", Content: "partial"}
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
			Type        string         `json:"type"`
			V           int            `json:"v"`
			Seq         int            `json:"seq"`
			TS          string         `json:"ts"`
			Interrupted bool           `json:"interrupted"`
			Message     ollama.Message `json:"message"`
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
		Interrupted bool           `json:"interrupted"`
		Message     ollama.Message `json:"message"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &first); err != nil {
		t.Fatal(err)
	}
	if first.Interrupted || first.Message.Role != "user" || first.Message.Content != "hi" {
		t.Fatalf("user %+v", first)
	}
	var second struct {
		Interrupted bool           `json:"interrupted"`
		Message     ollama.Message `json:"message"`
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
	err = w.Record(ollama.Message{Role: "user", Content: "hi"}, false)
	if err == nil {
		t.Fatal("expected an error")
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
