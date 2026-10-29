package provider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"co/internal/openai"
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
    auth: none
    models:
      - home-model
    compat:
      reasoning_effort: true
      reasoning_delta: true
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
    auth: none
    models:
      - project-model
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
	if ollama.BaseURL != "http://project.example:11434" || ollama.NeedsKey() {
		t.Fatalf("ollama %+v", ollama)
	}
	if len(ollama.Models) != 1 || ollama.Models[0] != "project-model" {
		t.Fatalf("ollama %+v", ollama)
	}
	if ollama.Compat.ReasoningEffort || ollama.Compat.ReasoningDelta {
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
	if ep.APIKey != "from-env" || ep.Model != "qwen/qwen3-coder" {
		t.Fatalf("endpoint %+v", ep)
	}

	local, err := cat.Lookup("lmstudio")
	if err != nil {
		t.Fatal(err)
	}
	if local.NeedsKey() {
		t.Fatal("lmstudio should not need a key")
	}
	if err := cat.SaveKey("lmstudio", "nope"); err == nil {
		t.Fatal("auth none accepted a key")
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

func TestProviderRequiresBaseURL(t *testing.T) {
	home := t.TempDir()
	writeYAML(t, HomeConfig(home), `
providers:
  ollama:
    auth: none
    models:
      - qwen3:8b
`)
	if _, err := Load(LoadOptions{Home: home, ProjectDir: t.TempDir()}); err == nil {
		t.Fatal("missing base_url was accepted")
	}
}

func TestConfiguredModelIsRequired(t *testing.T) {
	home := t.TempDir()
	writeYAML(t, HomeConfig(home), `
providers:
  ollama:
    base_url: http://127.0.0.1:11434
    auth: none
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
	if p.NeedsKey() || len(p.Models) != 1 || p.Models[0] != "qwen3:8b" {
		t.Fatalf("ollama %+v", p)
	}
	if p.Compat.ReasoningEffort || p.Compat.ReasoningDelta {
		t.Fatalf("ollama %+v", p)
	}
	if _, err := cat.Endpoint(Ref{Provider: "ollama", Model: "missing"}); err == nil {
		t.Fatal("missing model was accepted")
	}
	ep, err := cat.Endpoint(Ref{Provider: "ollama", Model: "qwen3:8b"})
	if err != nil {
		t.Fatal(err)
	}
	if ep.Model != "qwen3:8b" || ep.APIKey != "" {
		t.Fatalf("endpoint %+v", ep)
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
		t.Fatal("unknown provider accepted a key")
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

func TestProvidersFollowModel(t *testing.T) {
	home := t.TempDir()
	path := HomeConfig(home)
	writeYAML(t, path, `
log-level: debug
providers:
  ollama:
    base_url: http://127.0.0.1:11434
    auth: none
    models:
      - qwen3:8b
`)
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
	modelAt := strings.Index(text, "model:")
	providersAt := strings.Index(text, "providers:")
	logAt := strings.Index(text, "log-level:")
	if modelAt < 0 || providersAt < modelAt || logAt < providersAt {
		t.Fatalf("config:\n%s", text)
	}
}

func TestWriteProviderCreatesAndRefreshes(t *testing.T) {
	home := t.TempDir()
	path := HomeConfig(home)
	err := WriteProvider(path, ProviderSpec{
		ID:       "ollama",
		BaseURL:  "http://127.0.0.1:11434/",
		AuthNone: true,
		Models:   []string{"b", "a"},
		CompatOnCreate: &openai.Compat{
			ReasoningEffort: true,
			ReasoningDelta:  true,
		},
		DefaultModel: "ollama/a",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	modelAt := strings.Index(text, "model:")
	providersAt := strings.Index(text, "providers:")
	if modelAt < 0 || providersAt < modelAt || !strings.Contains(text, `model: "ollama/a"`) {
		t.Fatalf("config:\n%s", text)
	}
	if !strings.Contains(text, "reasoning_effort: true") || !strings.Contains(text, "reasoning_delta: true") {
		t.Fatalf("config:\n%s", text)
	}

	writeYAML(t, path, `
model: "openrouter/keep"
log-level: debug
providers:
  ollama:
    base_url: http://old.example:11434
    auth: none
    api_key_env: UNUSED
    models:
      - old
    compat:
      reasoning_effort: false
      reasoning_delta: false
`)
	err = WriteProvider(path, ProviderSpec{
		ID:       "ollama",
		BaseURL:  "http://127.0.0.1:11434",
		AuthNone: true,
		Models:   []string{"qwen3:8b"},
		CompatOnCreate: &openai.Compat{
			ReasoningEffort: true,
			ReasoningDelta:  true,
		},
		DefaultModel: "ollama/qwen3:8b",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text = string(data)
	modelAt = strings.Index(text, "model:")
	providersAt = strings.Index(text, "providers:")
	logAt := strings.Index(text, "log-level:")
	if modelAt < 0 || providersAt < modelAt || logAt < providersAt {
		t.Fatalf("config:\n%s", text)
	}
	if !strings.Contains(text, `model: "openrouter/keep"`) || !strings.Contains(text, "http://127.0.0.1:11434") {
		t.Fatalf("config:\n%s", text)
	}
	if strings.Contains(text, "old.example") || strings.Contains(text, "reasoning_effort: true") {
		t.Fatalf("config:\n%s", text)
	}
	if !strings.Contains(text, "api_key_env: UNUSED") {
		t.Fatalf("config:\n%s", text)
	}
	if !strings.Contains(text, "log-level: debug") || !strings.Contains(text, `"qwen3:8b"`) {
		t.Fatalf("config:\n%s", text)
	}

	err = WriteProvider(path, ProviderSpec{
		ID:       "ollama",
		BaseURL:  "http://127.0.0.1:11434",
		AuthNone: true,
		Models:   []string{"qwen3:8b"},
		Model:    "ollama/qwen3:8b",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `model: "ollama/qwen3:8b"`) {
		t.Fatalf("config:\n%s", data)
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
