package provider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	ref, err := Parse("openrouter/qwen/qwen3-coder")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Provider != "openrouter" || ref.Model != "qwen/qwen3-coder" || ref.String() != "openrouter/qwen/qwen3-coder" {
		t.Fatalf("ref %+v", ref)
	}

	if _, err := Parse("qwen3"); err == nil {
		t.Fatal("bare name was accepted")
	} else {
		var parseErr *ParseError
		if !errors.As(err, &parseErr) {
			t.Fatalf("err = %v", err)
		}
	}
	if _, err := Parse("ollama/"); err == nil {
		t.Fatal("empty model was accepted")
	}
	if _, err := Parse("/qwen"); err == nil {
		t.Fatal("empty provider was accepted")
	}
}

func TestMergePrecedence(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeYAML(t, HomeConfig(home), `
model: ollama/from-home
log-level: debug
providers:
  ollama:
    base_url: http://home.example:11434
    models:
      - home-model
  openrouter:
    base_url: https://home.example/v1
    api_key_env: HOME_KEY
    models:
      - home/model
  lmstudio:
    base_url: http://127.0.0.1:1234/v1
    auth: none
    models:
      - local
`)
	writeYAML(t, filepath.Join(project, ".co.yaml"), `
model: ollama/from-project
providers:
  ollama:
    base_url: http://project.example:11434
  openrouter:
    base_url: https://openrouter.ai/api/v1
    api_key_env: OPENROUTER_API_KEY
    models:
      - qwen/qwen3-coder
`)

	t.Setenv("OPENROUTER_API_KEY", "from-env")
	t.Setenv("HOME_KEY", "home-secret")
	cat, err := Load(LoadOptions{
		Home:       home,
		ProjectDir: project,
		FlagModel:  "ollama/from-flag",
		Host:       "http://flag.example:11434",
		HostSet:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cat.Selected() != "ollama/from-flag" {
		t.Fatalf("selected %q", cat.Selected())
	}

	ollama, err := cat.Lookup("ollama")
	if err != nil {
		t.Fatal(err)
	}
	if ollama.BaseURL != "http://flag.example:11434" || !ollama.LiveModels || ollama.NeedsKey() {
		t.Fatalf("ollama %+v", ollama)
	}
	if !ollama.Compat.ReasoningEffort || !ollama.Compat.ReasoningDelta {
		t.Fatalf("ollama compat %+v", ollama.Compat)
	}

	remote, err := cat.Lookup("openrouter")
	if err != nil {
		t.Fatal(err)
	}
	if remote.BaseURL != "https://openrouter.ai/api/v1" || len(remote.Models) != 1 || remote.Models[0] != "qwen/qwen3-coder" {
		t.Fatalf("openrouter %+v", remote)
	}
	ep, err := cat.Endpoint(Ref{Provider: "openrouter", Model: "qwen/qwen3-coder"})
	if err != nil {
		t.Fatal(err)
	}
	if ep.APIKey != "from-env" || ep.Probe || ep.Model != "qwen/qwen3-coder" {
		t.Fatalf("endpoint %+v", ep)
	}

	local, err := cat.Lookup("lmstudio")
	if err != nil {
		t.Fatal(err)
	}
	if local.NeedsKey() {
		t.Fatal("lmstudio should not need a key")
	}
}

func TestHomeModelWinsOverProject(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	writeYAML(t, HomeConfig(home), "model: ollama/home\n")
	writeYAML(t, filepath.Join(project, ".co.yaml"), "model: ollama/project\n")
	cat, err := Load(LoadOptions{Home: home, ProjectDir: project})
	if err != nil {
		t.Fatal(err)
	}
	if cat.Selected() != "ollama/home" {
		t.Fatalf("selected %q", cat.Selected())
	}
}

func TestExplicitOllamaModelsReplaceLiveList(t *testing.T) {
	home := t.TempDir()
	writeYAML(t, HomeConfig(home), `
providers:
  ollama:
    models:
      - qwen3:8b
    compat:
      reasoning_effort: false
`)
	cat, err := Load(LoadOptions{Home: home, ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	p, err := cat.Lookup("ollama")
	if err != nil {
		t.Fatal(err)
	}
	if p.LiveModels || len(p.Models) != 1 || p.Models[0] != "qwen3:8b" || p.Compat.ReasoningEffort {
		t.Fatalf("ollama %+v", p)
	}
	if !p.Compat.ReasoningDelta {
		t.Fatal("unset reasoning delta should stay on for ollama")
	}
	if _, err := cat.Endpoint(Ref{Provider: "ollama", Model: "missing"}); err == nil {
		t.Fatal("missing explicit model was accepted")
	}
}

func TestKeyFileLosesToEnv(t *testing.T) {
	home := t.TempDir()
	writeYAML(t, HomeConfig(home), `
providers:
  openrouter:
    base_url: https://openrouter.ai/api/v1
    api_key_env: OPENROUTER_API_KEY
    models:
      - qwen/qwen3-coder
`)
	cat, err := Load(LoadOptions{Home: home, ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ref := Ref{Provider: "openrouter", Model: "qwen/qwen3-coder"}
	if _, err := cat.Endpoint(ref); err == nil {
		t.Fatal("missing key was accepted")
	} else {
		var needs *NeedsKeyError
		if !errors.As(err, &needs) || !strings.Contains(err.Error(), "/connect openrouter") {
			t.Fatalf("err = %v", err)
		}
	}

	if err := cat.SaveKey("openrouter", "stored"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(home, ".co", "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(filepath.Join(home, ".co"))
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", dirInfo.Mode().Perm())
	}

	ep, err := cat.Endpoint(ref)
	if err != nil {
		t.Fatal(err)
	}
	if ep.APIKey != "stored" {
		t.Fatalf("key %q", ep.APIKey)
	}

	t.Setenv("OPENROUTER_API_KEY", "from-env")
	ep, err = cat.Endpoint(ref)
	if err != nil {
		t.Fatal(err)
	}
	if ep.APIKey != "from-env" {
		t.Fatalf("key %q", ep.APIKey)
	}

	if err := cat.SaveKey("ollama", "nope"); err == nil {
		t.Fatal("ollama accepted a key")
	}
}

func TestSaveSelectionPreservesOtherKeys(t *testing.T) {
	home := t.TempDir()
	path := HomeConfig(home)
	writeYAML(t, path, "# keep me\nlog-level: debug\n")
	cat, err := Load(LoadOptions{Home: home, ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.SaveSelection("ollama/qwen3:8b"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "log-level: debug") || !strings.Contains(text, "ollama/qwen3:8b") {
		t.Fatalf("config:\n%s", text)
	}
	if !strings.Contains(text, "keep me") {
		t.Fatalf("comment dropped:\n%s", text)
	}

	reloaded, err := Load(LoadOptions{Home: home, ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Selected() != "ollama/qwen3:8b" {
		t.Fatalf("selected %q", reloaded.Selected())
	}
}

func TestIsolatedConfigDoesNotWriteHome(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(t.TempDir(), "only.yaml")
	writeYAML(t, file, `
model: ollama/from-file
providers:
  lmstudio:
    base_url: http://127.0.0.1:1234/v1
    auth: none
    models:
      - qwen
`)
	writeYAML(t, HomeConfig(home), "model: ollama/from-home\n")
	cat, err := Load(LoadOptions{Home: home, ConfigFile: file})
	if err != nil {
		t.Fatal(err)
	}
	if cat.Selected() != "ollama/from-file" {
		t.Fatalf("selected %q", cat.Selected())
	}
	if _, err := cat.Lookup("lmstudio"); err != nil {
		t.Fatal(err)
	}
	if err := cat.SaveSelection("ollama/changed"); err != nil {
		t.Fatal(err)
	}
	homeData, err := os.ReadFile(HomeConfig(home))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(homeData), "changed") {
		t.Fatalf("home config changed:\n%s", homeData)
	}
}

func writeYAML(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
