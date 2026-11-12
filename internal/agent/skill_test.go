package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kevinjom/co/internal/llm"
	"github.com/kevinjom/co/internal/llm/openai"
	"github.com/kevinjom/co/internal/skill"
)

func TestSession_SetCatalog(t *testing.T) {
	t.Parallel()

	t.Run("nil session receiver does not panic", func(t *testing.T) {
		t.Parallel()
		var s *Session
		cwd := t.TempDir()
		writeAgentSkill(t, cwd, "grilling", "Grill a plan.", "Body")
		cat := skill.Load(cwd, t.TempDir())

		s.SetCatalog(cat)
	})

	t.Run("sets catalog and updates system prompt", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		writeAgentSkill(t, cwd, "grilling", "Grill a plan.", "Body")
		cat := skill.Load(cwd, t.TempDir())

		session := NewSession(openai.New("http://dummy"), "m", cwd, nil)
		session.SetCatalog(cat)

		msgs := session.Messages()
		if len(msgs) == 0 || msgs[0].Role != "system" {
			t.Fatalf("expected system message, got %#v", msgs)
		}
		if !strings.Contains(msgs[0].Content, "grilling: Grill a plan.") {
			t.Errorf("system prompt missing skill summary: %q", msgs[0].Content)
		}
		if !strings.Contains(msgs[0].Content, "When a message names a skill as /name, follow that skill for that turn only.") {
			t.Errorf("system prompt missing instruction: %q", msgs[0].Content)
		}
	})

	t.Run("nil catalog clears skills from prompt", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		writeAgentSkill(t, cwd, "grilling", "Grill a plan.", "Body")
		cat := skill.Load(cwd, t.TempDir())

		session := NewSession(openai.New("http://dummy"), "m", cwd, nil)
		session.SetCatalog(cat)
		session.SetCatalog(nil)

		msgs := session.Messages()
		if len(msgs) == 0 || msgs[0].Role != "system" {
			t.Fatalf("expected system message, got %#v", msgs)
		}
		if strings.Contains(msgs[0].Content, "grilling") {
			t.Errorf("system prompt still contains cleared skill: %q", msgs[0].Content)
		}
		if strings.Contains(msgs[0].Content, "follow that skill for that turn only") {
			t.Errorf("system prompt still contains skill instructions: %q", msgs[0].Content)
		}
	})

	t.Run("refreshPrompt with no system message does not panic or modify messages", func(t *testing.T) {
		t.Parallel()
		cwd := t.TempDir()
		writeAgentSkill(t, cwd, "grilling", "Grill a plan.", "Body")
		cat := skill.Load(cwd, t.TempDir())

		session := &Session{cwd: cwd}
		session.SetCatalog(cat)
		if len(session.Messages()) != 0 {
			t.Errorf("expected 0 messages, got %d", len(session.Messages()))
		}
	})
}

func TestSession_skillsOn(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	writeAgentSkill(t, cwd, "grilling", "Grill a plan.", "Grill body\n")
	writeAgentSkill(t, cwd, "notes", "Take notes.", "Notes body\n")
	cat := skill.Load(cwd, t.TempDir())

	tests := []struct {
		name     string
		catalog  *skill.Catalog
		line     string
		expected []string
	}{
		{
			name:     "nil catalog returns empty",
			catalog:  nil,
			line:     "/grilling please help",
			expected: nil,
		},
		{
			name:     "no skill mention",
			catalog:  cat,
			line:     "regular message without skills",
			expected: nil,
		},
		{
			name:     "single skill mention",
			catalog:  cat,
			line:     "/grilling review my plan",
			expected: []string{"grilling"},
		},
		{
			name:     "slash skill syntax mention",
			catalog:  cat,
			line:     "/skill notes summarize this",
			expected: []string{"notes"},
		},
		{
			name:     "multiple skills preserved in first-seen order",
			catalog:  cat,
			line:     "/notes and then /grilling",
			expected: []string{"notes", "grilling"},
		},
		{
			name:     "duplicate mentions in line only returned once",
			catalog:  cat,
			line:     "/grilling /notes and /grilling again",
			expected: []string{"grilling", "notes"},
		},
		{
			name:     "unknown skill is ignored",
			catalog:  cat,
			line:     "/unknown /grilling /missing",
			expected: []string{"grilling"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Session{catalog: tt.catalog}
			got := s.skillsOn(tt.line)

			if len(got) != len(tt.expected) {
				t.Fatalf("skillsOn(%q) got %d skills, want %d", tt.line, len(got), len(tt.expected))
			}
			for i, exp := range tt.expected {
				if got[i].Name != exp {
					t.Errorf("skillsOn(%q)[%d].Name = %q, want %q", tt.line, i, got[i].Name, exp)
				}
			}
		})
	}
}

func TestSkillsNamed(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	writeAgentSkill(t, cwd, "grilling", "Grill a plan.", "Grill body\n")
	writeAgentSkill(t, cwd, "notes", "Take notes.", "Notes body\n")
	writeAgentSkill(t, cwd, "code-review", "Review code.", "Review body\n")
	cat := skill.Load(cwd, t.TempDir())

	tests := []struct {
		name     string
		catalog  *skill.Catalog
		lines    []string
		expected []string
	}{
		{
			name:     "nil catalog returns empty",
			catalog:  nil,
			lines:    []string{"/grilling"},
			expected: []string{},
		},
		{
			name:     "empty lines",
			catalog:  cat,
			lines:    []string{},
			expected: []string{},
		},
		{
			name:     "blank line",
			catalog:  cat,
			lines:    []string{"   \t  "},
			expected: []string{},
		},
		{
			name:     "no mentions in text",
			catalog:  cat,
			lines:    []string{"hello world, this is a plain message"},
			expected: []string{},
		},
		{
			name:     "single mention",
			catalog:  cat,
			lines:    []string{"/grilling please review"},
			expected: []string{"grilling"},
		},
		{
			name:     "skill with hyphen",
			catalog:  cat,
			lines:    []string{"run /code-review on this PR"},
			expected: []string{"code-review"},
		},
		{
			name:     "slash skill syntax",
			catalog:  cat,
			lines:    []string{"/skill grilling my design"},
			expected: []string{"grilling"},
		},
		{
			name:     "slash skill syntax with unknown skill",
			catalog:  cat,
			lines:    []string{"/skill unknown"},
			expected: []string{},
		},
		{
			name:     "slash skill at end of line without next token",
			catalog:  cat,
			lines:    []string{"use /skill"},
			expected: []string{},
		},
		{
			name:     "multiple distinct mentions in first-seen order",
			catalog:  cat,
			lines:    []string{"first /notes then /grilling"},
			expected: []string{"notes", "grilling"},
		},
		{
			name:     "duplicate mentions in same line",
			catalog:  cat,
			lines:    []string{"/grilling and /grilling again"},
			expected: []string{"grilling"},
		},
		{
			name:     "duplicate mentions across multiple lines",
			catalog:  cat,
			lines:    []string{"line 1: /grilling", "line 2: /notes", "line 3: /grilling"},
			expected: []string{"grilling", "notes"},
		},
		{
			name:     "unknown skill name ignored",
			catalog:  cat,
			lines:    []string{"/nonexistent /grilling /anotherbad"},
			expected: []string{"grilling"},
		},
		{
			name:     "path with slash is not treated as skill token",
			catalog:  cat,
			lines:    []string{"check /usr/local/bin or /notes"},
			expected: []string{"notes"},
		},
		{
			name:     "bare slash is ignored",
			catalog:  cat,
			lines:    []string{"divide 10 / 2 or /notes"},
			expected: []string{"notes"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := skillsNamed(tt.lines, tt.catalog)
			if len(got) != len(tt.expected) {
				t.Fatalf("skillsNamed(%q) = %v (len %d), want %v (len %d)",
					tt.lines, got, len(got), tt.expected, len(tt.expected))
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("skillsNamed(%q)[%d] = %q, want %q", tt.lines, i, got[i], tt.expected[i])
				}
			}
		})
	}
}

func TestSkillToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		field     string
		wantName  string
		wantValid bool
	}{
		{
			name:      "empty string",
			field:     "",
			wantName:  "",
			wantValid: false,
		},
		{
			name:      "no leading slash",
			field:     "grilling",
			wantName:  "",
			wantValid: false,
		},
		{
			name:      "bare slash",
			field:     "/",
			wantName:  "",
			wantValid: false,
		},
		{
			name:      "valid skill token",
			field:     "/grilling",
			wantName:  "grilling",
			wantValid: true,
		},
		{
			name:      "valid skill token with hyphens",
			field:     "/code-review",
			wantName:  "code-review",
			wantValid: true,
		},
		{
			name:      "path containing slash",
			field:     "/path/to/file",
			wantName:  "",
			wantValid: false,
		},
		{
			name:      "double leading slash",
			field:     "//skill",
			wantName:  "",
			wantValid: false,
		},
		{
			name:      "trailing slash",
			field:     "/skill/",
			wantName:  "",
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotName, gotValid := skillToken(tt.field)
			if gotValid != tt.wantValid || gotName != tt.wantName {
				t.Errorf("skillToken(%q) = (%q, %t), want (%q, %t)",
					tt.field, gotName, gotValid, tt.wantName, tt.wantValid)
			}
		})
	}
}

func TestSkillAppliesToOneTurn(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	writeAgentSkill(t, cwd, "grilling", "Grill a plan.", "Ask one question.\n")
	writeAgentSkill(t, cwd, "notes", "Take notes.", "Write them down.\n")
	cat := skill.Load(cwd, t.TempDir())

	var mu sync.Mutex
	var systems []string
	var users [][]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var payload recordedChat
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode: %v", err)
		}
		var turn []string
		var sys string
		for _, msg := range payload.Messages {
			switch msg.Role {
			case "system":
				if strings.Contains(msg.Content, "You are co") {
					sys = msg.Content
				}
			case "user":
				turn = append(turn, msg.Content)
			}
		}

		mu.Lock()
		if sys != "" {
			systems = append(systems, sys)
		}
		users = append(users, turn)
		mu.Unlock()

		writeSSE(w, contentChunk("ok"))
	}))
	defer server.Close()

	session := NewSession(openai.New(server.URL), "m", cwd, nil)
	session.SetCatalog(cat)
	session.EnableLSP(idleLanguage{})
	const typed = "/grilling /notes fix the parser"
	if _, err := session.Turn(t.Context(), typed, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Turn(t.Context(), "what next?", nil); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	sysCount := len(systems)
	userCount := len(users)
	sys0, sys1 := "", ""
	if sysCount == 2 {
		sys0, sys1 = systems[0], systems[1]
	}
	userSnapshot := users
	mu.Unlock()

	if sysCount != 2 || userCount != 2 {
		t.Fatalf("systems %d users %d", sysCount, userCount)
	}
	if sys0 != sys1 {
		t.Fatalf("system prompt changed\nfirst %q\nsecond %q", sys0, sys1)
	}
	if !strings.Contains(sys0, "grilling: Grill a plan.") || !strings.Contains(sys0, "notes: Take notes.") {
		t.Fatalf("catalog %q", sys0)
	}
	if !strings.Contains(sys0, "follow that skill for that turn only") {
		t.Fatalf("prompt %q", sys0)
	}
	if !strings.Contains(sys0, "lsp_definition") {
		t.Fatalf("prompt %q", sys0)
	}
	if strings.Contains(sys0, "Ask one question.") || strings.Contains(sys0, "Write them down.") {
		t.Fatalf("system prompt kept a skill %q", sys0)
	}
	if len(userSnapshot[0]) != 1 || userSnapshot[0][0] != userSnapshot[1][0] {
		t.Fatalf("skill text changed %#v", userSnapshot)
	}
	sent := userSnapshot[0][0]
	if !strings.HasPrefix(sent, typed+"\n\n") {
		t.Fatalf("sent %q", sent)
	}
	grilling := strings.Index(sent, "Skill grilling is active. Its files are in .agents/skills/grilling/.")
	notes := strings.Index(sent, "Skill notes is active. Its files are in .agents/skills/notes/.")
	if grilling < 0 || notes < grilling {
		t.Fatalf("sent %q", sent)
	}
	if !strings.Contains(sent, "Ask one question.") || !strings.Contains(sent, "Write them down.") {
		t.Fatalf("sent %q", sent)
	}
	if len(userSnapshot[1]) != 2 || userSnapshot[1][1] != "what next?" {
		t.Fatalf("second turn %#v", userSnapshot[1])
	}
	stored := session.Messages()
	if stored[1].Content != typed {
		t.Fatalf("user message %q", stored[1].Content)
	}
	if strings.Contains(stored[0].Content, "Ask one question.") {
		t.Fatalf("system prompt kept the skill %q", stored[0].Content)
	}
	if strings.Contains(stored[1].Content, "Ask one question.") {
		t.Fatalf("user message kept the skill %q", stored[1].Content)
	}
}

func TestSkillMentionCanUseTheSkillWord(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	writeAgentSkill(t, cwd, "grilling", "Grill a plan.", "Ask one question.\n")
	cat := skill.Load(cwd, t.TempDir())

	var mu sync.Mutex
	var sent string
	var system string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var payload recordedChat
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode: %v", err)
		}
		mu.Lock()
		for _, msg := range payload.Messages {
			switch msg.Role {
			case "system":
				system = msg.Content
			case "user":
				sent = msg.Content
			}
		}
		mu.Unlock()
		writeSSE(w, contentChunk("ok"))
	}))
	defer server.Close()

	session := NewSession(openai.New(server.URL), "m", cwd, nil)
	session.SetCatalog(cat)
	if _, err := session.Turn(t.Context(), "/skill grilling and /notes", nil); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	gotSent := sent
	gotSystem := system
	mu.Unlock()

	if !strings.HasPrefix(gotSent, "/skill grilling and /notes\n\n") || !strings.Contains(gotSent, "Ask one question.") {
		t.Fatalf("sent %q", gotSent)
	}
	if strings.Contains(gotSystem, "Ask one question.") {
		t.Fatalf("system %q", gotSystem)
	}
}

func TestTurnRecordsTheSkillBesideTheMessage(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	writeAgentSkill(t, cwd, "grilling", "Grill a plan.", "Ask one question.\n")
	cat := skill.Load(cwd, t.TempDir())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeSSE(w, contentChunk("ok"))
	}))
	defer server.Close()

	rec := &memRecorder{}
	session := NewSession(openai.New(server.URL), "m", cwd, rec)
	session.SetCatalog(cat)
	if _, err := session.Turn(t.Context(), "/grilling fix the parser", nil); err != nil {
		t.Fatal(err)
	}
	if len(rec.msgs) < 1 || rec.msgs[0].Content != "/grilling fix the parser" {
		t.Fatalf("stored %+v", rec.msgs)
	}
	if len(rec.skills) != 1 {
		t.Fatalf("skills %#v", rec.skills)
	}
	if !strings.Contains(rec.skills[0], "grilling") || !strings.Contains(rec.skills[0], ".agents/skills/grilling") {
		t.Fatalf("skill %q", rec.skills[0])
	}
	if !strings.Contains(rec.skills[0], "Ask one question.") {
		t.Fatalf("skill %q", rec.skills[0])
	}
}

func TestResumedSkillStaysOnTheUserMessage(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	writeAgentSkill(t, cwd, "grilling", "Grill a plan.", "Ask one question.\n")
	cat := skill.Load(cwd, t.TempDir())

	var mu sync.Mutex
	var sent string
	var system string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		var payload recordedChat
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode: %v", err)
		}
		mu.Lock()
		for _, msg := range payload.Messages {
			switch msg.Role {
			case "system":
				system = msg.Content
			case "user":
				if sent == "" {
					sent = msg.Content
				}
			}
		}
		mu.Unlock()
		writeSSE(w, contentChunk("ok"))
	}))
	defer server.Close()

	session := NewSession(openai.New(server.URL), "m", cwd, nil)
	session.SetCatalog(cat)
	session.Restore([]llm.Message{
		{Role: "user", Content: "/grilling fix the parser"},
		{Role: "assistant", Content: "What should the parser accept?"},
	})
	session.BindSkills([][]skill.Skill{
		{{
			Name: "grilling",
			Dir:  ".agents/skills/grilling",
			Body: "---\nname: grilling\ndescription: Grill a plan.\n---\nStored question.\n",
		}},
		nil,
	})
	if _, err := session.Turn(t.Context(), "what next?", nil); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	gotSent := sent
	gotSystem := system
	mu.Unlock()

	if !strings.Contains(gotSent, "Stored question.") || !strings.Contains(gotSent, "/grilling fix the parser") {
		t.Fatalf("sent %q", gotSent)
	}
	if strings.Contains(gotSent, "Ask one question.") || strings.Contains(gotSystem, "Stored question.") {
		t.Fatalf("sent %q system %q", gotSent, gotSystem)
	}
	stored := session.Messages()
	if stored[1].Content != "/grilling fix the parser" {
		t.Fatalf("user message %q", stored[1].Content)
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
