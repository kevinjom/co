package skill

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsAValidSkill(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	body := "Ask one question at a time.\n"
	writeSkill(t, filepath.Join(cwd, ".agents", "skills", "grilling"), "grilling", "Grill a plan.", body)

	cat := Load(cwd, home)
	got, ok := cat.Get("grilling")
	if !ok {
		t.Fatal("missing grilling")
	}
	if got.Dir != ".agents/skills/grilling" {
		t.Fatalf("dir %q", got.Dir)
	}
	if !strings.Contains(got.Body, "name: grilling") || !strings.Contains(got.Body, body) {
		t.Fatalf("body %q", got.Body)
	}
	if names := cat.Names(); len(names) != 1 || names[0] != "grilling" {
		t.Fatalf("names %v", names)
	}
}

func TestLoadProjectOverridesHome(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, ".co", "skills", "grilling"), "grilling", "Home copy.", "home\n")
	writeSkill(t, filepath.Join(cwd, ".agents", "skills", "grilling"), "grilling", "Project copy.", "project\n")

	got, ok := Load(cwd, home).Get("grilling")
	if !ok {
		t.Fatal("missing grilling")
	}
	if got.Description != "Project copy." || got.Dir == "" || !strings.Contains(got.Body, "project") {
		t.Fatalf("skill %+v", got)
	}
}

func TestLoadKeepsAHomeSkill(t *testing.T) {
	cwd := t.TempDir()
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, ".co", "skills", "notes"), "notes", "Take notes.", "write them down\n")

	got, ok := Load(cwd, home).Get("notes")
	if !ok {
		t.Fatal("missing notes")
	}
	if got.Dir != "" {
		t.Fatalf("dir %q", got.Dir)
	}
}

func TestLoadMissingRootsAreEmpty(t *testing.T) {
	cat := Load(t.TempDir(), t.TempDir())
	if len(cat.List()) != 0 {
		t.Fatalf("skills %v", cat.List())
	}
}

func TestLoadSkipsInvalidSkills(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cwd := t.TempDir()
	root := filepath.Join(cwd, ".agents", "skills")
	writeSkill(t, filepath.Join(root, "Bad"), "Bad", "Uppercase.", "no\n")
	writeSkill(t, filepath.Join(root, "alpha"), "beta", "Wrong directory.", "no\n")
	writeSkill(t, filepath.Join(root, "-bad"), "-bad", "Leading hyphen.", "no\n")
	writeSkill(t, filepath.Join(root, "double--dash"), "double--dash", "Doubled hyphen.", "no\n")
	writeSkill(t, filepath.Join(root, "skill"), "skill", "Shadows a command.", "no\n")
	writeRaw(t, filepath.Join(root, "plain"), "SKILL.md", "no frontmatter\n")
	long := strings.Repeat("a", maxDescriptionLen+1)
	writeSkill(t, filepath.Join(root, "long"), "long", long, "no\n")
	writeSkill(t, filepath.Join(root, "blank"), "blank", "   ", "no\n")
	writeSkill(t, filepath.Join(root, "ok-skill"), "ok-skill", "Kept.", "yes\n")

	nested := strings.Join([]string{
		"---",
		"name: nested",
		"description: Nested metadata.",
		"metadata:",
		"  author: samber",
		"  openclaw:",
		"    emoji: x",
		"---",
		"body",
		"",
	}, "\n")
	writeRaw(t, filepath.Join(root, "nested"), "SKILL.md", nested)

	cat := Load(cwd, t.TempDir())
	names := cat.Names()
	if len(names) != 2 || names[0] != "nested" || names[1] != "ok-skill" {
		t.Fatalf("names %v", names)
	}
	log := buf.String()
	for _, name := range []string{"Bad", "alpha", "-bad", "double--dash", "skill", "plain", "long", "blank"} {
		if !strings.Contains(log, name) {
			t.Fatalf("log missing %s: %s", name, log)
		}
	}
}

func writeSkill(t *testing.T, dir, name, description, body string) {
	t.Helper()
	text := "---\nname: " + name + "\ndescription: " + description + "\n---\n" + body
	writeRaw(t, dir, "SKILL.md", text)
}

func writeRaw(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
