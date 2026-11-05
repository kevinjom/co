package command

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevinjom/co/internal/openai"
	"github.com/kevinjom/co/internal/provider"
)

func TestOpenKeepsSessionsIndependent(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	settings := "model: ollama/alpha\n"
	models := `
providers:
  ollama:
    base_url: http://127.0.0.1:11434
    auth: none
    models:
      - alpha
      - beta
`
	if err := os.MkdirAll(filepath.Join(home, ".co"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provider.HomeConfig(home), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provider.HomeModels(home), []byte(models), 0o600); err != nil {
		t.Fatal(err)
	}
	cat, err := provider.Load(provider.LoadOptions{Home: home, ProjectDir: cwd})
	if err != nil {
		t.Fatal(err)
	}
	app := New(
		cat,
		home,
		cwd,
		false,
		0,
	)
	ctx := context.Background()
	if _, err := app.Open(ctx); err == nil {
		t.Fatal("opened a session before a model was selected")
	}
	if err := app.Activate(ctx, "ollama/alpha"); err != nil {
		t.Fatal(err)
	}
	first := app.Session()
	second, err := app.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || second == nil || first == second {
		t.Fatal("expected two sessions")
	}
	if err := app.Select(ctx, "ollama/beta", second); err != nil {
		t.Fatal(err)
	}
	if first.Model() != "alpha" || second.Model() != "beta" {
		t.Fatalf("models %q %q", first.Model(), second.Model())
	}

	msg := openai.Message{Role: "user", Content: "hello"}
	h0 := app.running[0].history
	h1 := app.running[1].history
	if err := h0.Record(msg, false); err != nil {
		t.Fatal(err)
	}
	if err := h1.Record(msg, false); err != nil {
		t.Fatal(err)
	}
	note := app.ExitNote()
	if h0.ID() == h1.ID() || !strings.Contains(note, h0.ID()) || !strings.Contains(note, h1.ID()) {
		t.Fatalf("note %q", note)
	}
	app.Close()
	if err := h0.Record(msg, false); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("first writer: %v", err)
	}
	if err := h1.Record(msg, false); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("second writer: %v", err)
	}
}

func TestResumeDoesNotKeepASkill(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".co"), 0o700); err != nil {
		t.Fatal(err)
	}
	settings := "model: ollama/alpha\n"
	models := `
providers:
  ollama:
    base_url: http://127.0.0.1:11434
    auth: none
    models:
      - alpha
`
	if err := os.WriteFile(provider.HomeConfig(home), []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(provider.HomeModels(home), []byte(models), 0o600); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(cwd, ".agents", "skills", "grilling")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: grilling\ndescription: Grill a plan.\n---\nAsk one question.\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	newApp := func() *App {
		t.Helper()
		cat, err := provider.Load(provider.LoadOptions{Home: home, ProjectDir: cwd})
		if err != nil {
			t.Fatal(err)
		}
		return New(cat, home, cwd, false, 0)
	}

	ctx := context.Background()
	app := newApp()
	if err := app.Activate(ctx, "ollama/alpha"); err != nil {
		t.Fatal(err)
	}
	if err := app.running[0].history.RecordSkill("grilling"); err != nil {
		t.Fatal(err)
	}
	id := app.running[0].history.ID()
	app.Close()

	app = newApp()
	if err := app.Resume(ctx, id, ""); err != nil {
		t.Fatal(err)
	}
	prompt := app.Session().Messages()[0].Content
	if strings.Contains(prompt, "Ask one question.") {
		t.Fatalf("resumed prompt kept the skill %q", prompt)
	}
	app.Close()
}
