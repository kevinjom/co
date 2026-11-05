package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevinjom/co/internal/openai"
	"github.com/kevinjom/co/internal/skill"
)

func TestSkillAppliesToOneTurn(t *testing.T) {
	cwd := t.TempDir()
	writeAgentSkill(t, cwd, "grilling", "Grill a plan.", "Ask one question.\n")
	writeAgentSkill(t, cwd, "notes", "Take notes.", "Write them down.\n")
	cat := skill.Load(cwd, t.TempDir())

	var prompts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode: %v", err)
		}
		for _, msg := range payload.Messages {
			if msg.Role == "system" && strings.Contains(msg.Content, "You are co") {
				prompts = append(prompts, msg.Content)
			}
		}
		writeSSE(w, contentChunk("ok"))
	}))
	defer server.Close()

	session := NewSession(openai.New(server.URL), "m", cwd, nil)
	session.SetCatalog(cat)
	session.EnableLSP(idleLanguage{})
	if _, err := session.Turn(t.Context(), "/grilling fix the parser", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Turn(t.Context(), "what next?", nil); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 {
		t.Fatalf("prompts %d", len(prompts))
	}
	first := prompts[0]
	if !strings.Contains(first, "grilling: Grill a plan.") || !strings.Contains(first, "notes: Take notes.") {
		t.Fatalf("catalog %q", first)
	}
	if !strings.Contains(first, "follow that skill for that turn only") {
		t.Fatalf("prompt %q", first)
	}
	if !strings.Contains(first, "Ask one question.") || strings.Contains(first, "Write them down.") {
		t.Fatalf("first turn %q", first)
	}
	if !strings.Contains(first, "Skill grilling is active. Its files are in .agents/skills/grilling/.") {
		t.Fatalf("path %q", first)
	}
	if !strings.Contains(first, "lsp_definition") {
		t.Fatalf("lsp %q", first)
	}
	second := prompts[1]
	if strings.Contains(second, "Ask one question.") || strings.Contains(second, "Write them down.") {
		t.Fatalf("second turn %q", second)
	}
	if !strings.Contains(second, "lsp_definition") || !strings.Contains(second, "grilling: Grill a plan.") {
		t.Fatalf("second catalog %q", second)
	}
	stored := session.Messages()
	if stored[1].Content != "/grilling fix the parser" {
		t.Fatalf("user message %q", stored[1].Content)
	}
	if strings.Contains(stored[0].Content, "Ask one question.") {
		t.Fatalf("system prompt kept the skill %q", stored[0].Content)
	}
}

func TestSkillMentionCanUseTheSkillWord(t *testing.T) {
	cwd := t.TempDir()
	writeAgentSkill(t, cwd, "grilling", "Grill a plan.", "Ask one question.\n")
	cat := skill.Load(cwd, t.TempDir())
	var prompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode: %v", err)
		}
		for _, msg := range payload.Messages {
			if msg.Role == "system" && strings.Contains(msg.Content, "You are co") {
				prompt = msg.Content
			}
		}
		writeSSE(w, contentChunk("ok"))
	}))
	defer server.Close()

	session := NewSession(openai.New(server.URL), "m", cwd, nil)
	session.SetCatalog(cat)
	if _, err := session.Turn(t.Context(), "/skill grilling and /notes", nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Ask one question.") {
		t.Fatalf("prompt %q", prompt)
	}
}

func writeAgentSkill(t *testing.T, cwd, name, description, body string) {
	t.Helper()
	dir := filepath.Join(cwd, ".agents", "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	text := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
