package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestListProjectFilesSkipsIgnoredPaths(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "*.log\nvendor/\n!keep.log\n")
	write("keep.go", "package p\n")
	write("keep.log", "keep\n")
	write("drop.log", "drop\n")
	write("vendor/secret.go", "package secret\n")
	write(".git/config", "[core]\n")
	write("nested/.gitignore", "skip.txt\n")
	write("nested/skip.txt", "nope\n")
	write("nested/keep.txt", "yes\n")

	got, err := listProjectFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		".gitignore",
		"keep.go",
		"keep.log",
		"nested/.gitignore",
		"nested/keep.txt",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("files:\n%s", strings.Join(got, "\n"))
	}
}

func TestListProjectFilesRejectsAFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := listProjectFiles(path); err == nil {
		t.Fatal("expected an error")
	}
}

func TestRankFilesUsesFuzzyMatch(t *testing.T) {
	paths := []string{"readme.md", "internal/note.go", "internal/tui/markdown.go"}
	hits := rankFiles(paths, "")
	if len(hits) != len(paths) || hits[0].path != "readme.md" || hits[2].path != "internal/tui/markdown.go" {
		t.Fatalf("order %#v", hits)
	}

	hits = rankFiles(paths, "note")
	if len(hits) != 1 || hits[0].path != "internal/note.go" {
		t.Fatalf("match %#v", hits)
	}
	var marked []byte
	for _, index := range hits[0].indexes {
		marked = append(marked, hits[0].path[index])
	}
	if string(marked) != "note" {
		t.Fatalf("highlight %q indexes %v", marked, hits[0].indexes)
	}

	hits = rankFiles(paths, "zzz")
	if len(hits) != 0 {
		t.Fatalf("unexpected %#v", hits)
	}
}

func TestMentionAtCursor(t *testing.T) {
	end := len([]rune("see @re"))
	ment, ok := mentionAt("see @re now", 0, end)
	if !ok || ment.query != "re" {
		t.Fatalf("ok %v query %q", ok, ment.query)
	}
	next, caret := applyMention("see @re now", ment, "readme.md")
	if next != "see @readme.md now" {
		t.Fatalf("inserted %q", next)
	}
	if _, still := mentionAt(next, 0, caret); still {
		t.Fatal("picker reopened")
	}

	if _, ok := mentionAt("user@host", 0, len("user@host")); ok {
		t.Fatal("address opened a picker")
	}
	ment, ok = mentionAt("@", 0, 1)
	if !ok || ment.query != "" {
		t.Fatalf("bare @ ok %v query %q", ok, ment.query)
	}
	ment, ok = mentionAt("top\n@c", 1, 2)
	if !ok || ment.query != "c" || ment.at != len([]rune("top\n")) {
		t.Fatalf("second line %#v ok %v", ment, ok)
	}
}

func TestSetCaretLandsOnTheRequestedRune(t *testing.T) {
	m := newTestModel(t)
	m.input.Focus()
	value := "one\nsee @file two"
	caret := len([]rune("one\nsee @file "))
	setCaret(&m.input, value, caret)
	if m.input.Value() != value {
		t.Fatalf("value %q", m.input.Value())
	}
	if m.input.Line() != 1 || m.input.Column() != len([]rune("see @file ")) {
		t.Fatalf("line %d col %d", m.input.Line(), m.input.Column())
	}
	if _, ok := m.mention(); ok {
		t.Fatal("caret reopened the mention")
	}
}

func TestAtInsertsAFuzzyFile(t *testing.T) {
	m := projectModel(t)
	m = typeText(t, m, "look at @note")
	plain := ansi.Strip(m.View().Content)
	if !strings.Contains(plain, "> internal/note.go") || strings.Contains(plain, "readme.md") {
		t.Fatalf("list %q", plain)
	}
	if !strings.Contains(plain, "tab insert") || !strings.Contains(plain, "esc close") {
		t.Fatalf("help %q", plain)
	}
	if h := lipgloss.Height(m.View().Content); h != 24 {
		t.Fatalf("height %d\n%s", h, plain)
	}

	updated, _ := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if got.tab().busy || got.input.Value() != "look at @internal/note.go " {
		t.Fatalf("busy %v value %q", got.tab().busy, got.input.Value())
	}
	plain = ansi.Strip(got.View().Content)
	if strings.Contains(plain, "> internal/note.go") || strings.Contains(plain, "tab insert") {
		t.Fatalf("list lingered %q", plain)
	}
}

func TestAtMovesAndInsertsWithTab(t *testing.T) {
	m := projectModel(t)
	m = typeText(t, m, "@")
	plain := ansi.Strip(m.View().Content)
	if !strings.Contains(plain, "> .gitignore") || !strings.Contains(plain, "  internal/note.go") || !strings.Contains(plain, "  readme.md") {
		t.Fatalf("list %q", plain)
	}
	if strings.Contains(plain, "vendor") || strings.Contains(plain, "drop.log") {
		t.Fatalf("ignored path listed %q", plain)
	}

	updated, _ := m.Update(press(tea.KeyDown, 0))
	m = updated.(*model)
	plain = ansi.Strip(m.View().Content)
	if !strings.Contains(plain, "> internal/note.go") || !strings.Contains(plain, "  .gitignore") {
		t.Fatalf("moved %q", plain)
	}
	updated, _ = m.Update(press(tea.KeyUp, 0))
	m = updated.(*model)
	updated, _ = m.Update(press(tea.KeyDown, 0))
	m = updated.(*model)
	updated, _ = m.Update(press(tea.KeyTab, 0))
	got := updated.(*model)
	if got.tab().busy || got.input.Value() != "@internal/note.go " {
		t.Fatalf("busy %v value %q", got.tab().busy, got.input.Value())
	}
}

func TestAtEscClosesTheList(t *testing.T) {
	m := projectModel(t)
	m = typeText(t, m, "@note")
	updated, _ := m.Update(press(tea.KeyEsc, 0))
	got := updated.(*model)
	if got.input.Value() != "@note" {
		t.Fatalf("value %q", got.input.Value())
	}
	plain := ansi.Strip(got.View().Content)
	if strings.Contains(plain, "internal/note.go") || strings.Contains(plain, "tab insert") {
		t.Fatalf("list lingered %q", plain)
	}
	if !strings.Contains(plain, "enter send") {
		t.Fatalf("help %q", plain)
	}
}

func TestAtInsideAWordDoesNotOpen(t *testing.T) {
	m := projectModel(t)
	m = typeText(t, m, "mail user@host")
	plain := ansi.Strip(m.View().Content)
	if strings.Contains(plain, "internal/note.go") || strings.Contains(plain, "tab insert") || strings.Contains(plain, "indexing files") {
		t.Fatalf("picker opened %q", plain)
	}
}

func TestAtEnterWaitsWhileIndexing(t *testing.T) {
	m := newTestModel(t)
	if err := os.WriteFile(filepath.Join(m.cwd, "readme.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = typeText(t, m, "@")
	plain := ansi.Strip(m.View().Content)
	if !strings.Contains(plain, "indexing files") {
		t.Fatalf("view %q", plain)
	}
	updated, _ := m.Update(press(tea.KeyEnter, 0))
	got := updated.(*model)
	if got.tab().busy || got.input.Value() != "@" {
		t.Fatalf("busy %v value %q", got.tab().busy, got.input.Value())
	}

	updated, _ = got.Update(filesLoadedMsg{root: m.cwd, paths: []string{"readme.md"}})
	got = updated.(*model)
	plain = ansi.Strip(got.View().Content)
	if !strings.Contains(plain, "> readme.md") {
		t.Fatalf("loaded %q", plain)
	}
}

func TestLoadFilesIndexesOnce(t *testing.T) {
	m := newTestModel(t)
	if err := os.WriteFile(filepath.Join(m.cwd, "readme.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := m.loadFiles()
	if cmd == nil {
		t.Fatal("expected a load")
	}
	if m.loadFiles() != nil {
		t.Fatal("second load")
	}
	msg, ok := cmd().(filesLoadedMsg)
	if !ok || msg.err != nil || len(msg.paths) != 1 || msg.paths[0] != "readme.md" {
		t.Fatalf("msg %#v ok %v", msg, ok)
	}
	updated, next := m.Update(msg)
	got := updated.(*model)
	if next != nil || !got.filesReady || len(got.files) != 1 {
		t.Fatalf("ready %v files %#v next %v", got.filesReady, got.files, next != nil)
	}
	updated, _ = got.Update(filesLoadedMsg{root: "elsewhere", paths: []string{"other.md"}})
	got = updated.(*model)
	if len(got.files) != 1 || got.files[0] != "readme.md" {
		t.Fatalf("files %#v", got.files)
	}
}

func TestPasteOpensTheFilePicker(t *testing.T) {
	m := projectModel(t)
	m.input.Focus()
	updated, _ := m.Update(tea.PasteMsg{Content: "see @note"})
	got := updated.(*model)
	plain := ansi.Strip(got.View().Content)
	if got.input.Value() != "see @note" || !strings.Contains(plain, "> internal/note.go") {
		t.Fatalf("value %q view %q", got.input.Value(), plain)
	}
}

func projectModel(t *testing.T) *model {
	t.Helper()
	m := newTestModel(t)
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(m.cwd, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".gitignore", "*.log\nvendor/\n")
	write("readme.md", "hi\n")
	write("internal/note.go", "package note\n")
	write("drop.log", "nope\n")
	write("vendor/secret.go", "package secret\n")
	return readyFiles(t, m)
}

func readyFiles(t *testing.T, m *model) *model {
	t.Helper()
	cmd := m.loadFiles()
	if cmd == nil {
		t.Fatal("load")
	}
	msg, ok := cmd().(filesLoadedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("msg %#v ok %v", msg, ok)
	}
	updated, _ := m.Update(msg)
	return updated.(*model)
}

func typeText(t *testing.T, m *model, text string) *model {
	t.Helper()
	m.input.Focus()
	for _, r := range text {
		updated, _ := m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		next, ok := updated.(*model)
		if !ok {
			t.Fatalf("model %T", updated)
		}
		m = next
	}
	return m
}
