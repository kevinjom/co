package provider

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kevinjom/co/internal/openai"
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
no-history: true
clear-scrollback: true
`)
	writeYAML(t, HomeModels(home), `
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
	writeYAML(t, HomeModels(home), `
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
	writeYAML(t, HomeModels(home), `
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

func TestProvidersSkipsAuthNone(t *testing.T) {
	home := t.TempDir()
	writeYAML(t, HomeModels(home), `
providers:
  ollama:
    base_url: http://127.0.0.1:11434
    auth: none
    models:
      - qwen3:8b
  gemini:
    base_url: https://generativelanguage.googleapis.com/v1beta/openai
    api_key_env: GEMINI_API_KEY
    models:
      - gemini-2.5-flash
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
	got := strings.Join(cat.Providers(), ",")
	if got != "gemini,openrouter" {
		t.Fatalf("providers %q", got)
	}
}

func TestKeyFileLosesToEnv(t *testing.T) {
	home := t.TempDir()
	writeYAML(t, HomeModels(home), `
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
	configPath := HomeConfig(home)
	modelsPath := HomeModels(home)
	writeYAML(t, configPath, "log-level: debug\n")
	models := `
providers:
  ollama:
    base_url: http://127.0.0.1:11434
    auth: none
    models:
      - qwen3:8b
`
	writeYAML(t, modelsPath, models)
	cat, err := Load(LoadOptions{Home: home, ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := cat.SaveSelection("ollama/qwen3:8b"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	modelAt := strings.Index(text, "model:")
	logAt := strings.Index(text, "log-level:")
	if modelAt < 0 || logAt < modelAt || strings.Contains(text, "providers:") {
		t.Fatalf("config:\n%s", text)
	}
	left, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(left) != models {
		t.Fatalf("models.yaml changed:\n%s", left)
	}
}

func TestWriteProviderCreatesAndRefreshes(t *testing.T) {
	home := t.TempDir()
	configPath := HomeConfig(home)
	modelsPath := HomeModels(home)
	err := WriteProvider(configPath, modelsPath, ProviderSpec{
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
	configText := readTestFile(t, configPath)
	if !strings.Contains(configText, `model: "ollama/a"`) || strings.Contains(configText, "providers:") {
		t.Fatalf("config:\n%s", configText)
	}
	modelsText := readTestFile(t, modelsPath)
	if strings.Contains(modelsText, "model:") || !strings.Contains(modelsText, "providers:") {
		t.Fatalf("models:\n%s", modelsText)
	}
	if !strings.Contains(modelsText, "reasoning_effort: true") || !strings.Contains(modelsText, "reasoning_delta: true") {
		t.Fatalf("models:\n%s", modelsText)
	}

	writeYAML(t, configPath, `
model: "openrouter/keep"
log-level: debug
no-history: true
`)
	writeYAML(t, modelsPath, `
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
	err = WriteProvider(configPath, modelsPath, ProviderSpec{
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
	configText = readTestFile(t, configPath)
	modelAt := strings.Index(configText, "model:")
	logAt := strings.Index(configText, "log-level:")
	if modelAt < 0 || logAt < modelAt || !strings.Contains(configText, `model: "openrouter/keep"`) {
		t.Fatalf("config:\n%s", configText)
	}
	if !strings.Contains(configText, "log-level: debug") || !strings.Contains(configText, "no-history: true") {
		t.Fatalf("config:\n%s", configText)
	}
	if strings.Contains(configText, "providers:") || strings.Contains(configText, "old.example") {
		t.Fatalf("config:\n%s", configText)
	}
	modelsText = readTestFile(t, modelsPath)
	if strings.Contains(modelsText, "model:") || strings.Contains(modelsText, "old.example") || strings.Contains(modelsText, "reasoning_effort: true") {
		t.Fatalf("models:\n%s", modelsText)
	}
	if !strings.Contains(modelsText, "http://127.0.0.1:11434") || !strings.Contains(modelsText, "api_key_env: UNUSED") {
		t.Fatalf("models:\n%s", modelsText)
	}
	if !strings.Contains(modelsText, `"qwen3:8b"`) {
		t.Fatalf("models:\n%s", modelsText)
	}

	err = WriteProvider(configPath, modelsPath, ProviderSpec{
		ID:       "ollama",
		BaseURL:  "http://127.0.0.1:11434",
		AuthNone: true,
		Models:   []string{"qwen3:8b"},
		Model:    "ollama/qwen3:8b",
	})
	if err != nil {
		t.Fatal(err)
	}
	configText = readTestFile(t, configPath)
	if !strings.Contains(configText, `model: "ollama/qwen3:8b"`) || !strings.Contains(configText, "log-level: debug") {
		t.Fatalf("config:\n%s", configText)
	}
}

func TestWriteProviderCombinedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "only.yaml")
	err := WriteProvider(path, path, ProviderSpec{
		ID:           "ollama",
		BaseURL:      "http://127.0.0.1:11434/",
		AuthNone:     true,
		Models:       []string{"a"},
		DefaultModel: "ollama/a",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := readTestFile(t, path)
	modelAt := strings.Index(text, "model:")
	providersAt := strings.Index(text, "providers:")
	if modelAt < 0 || providersAt < modelAt || !strings.Contains(text, `model: "ollama/a"`) {
		t.Fatalf("config:\n%s", text)
	}
}

func TestConfigProvidersAreNotTheCatalog(t *testing.T) {
	home := t.TempDir()
	writeYAML(t, HomeConfig(home), `
model: ollama/from-config
providers:
  lmstudio:
    base_url: http://config.example:1234/v1
    auth: none
    models:
      - local
`)
	writeYAML(t, HomeModels(home), `
providers:
  ollama:
    base_url: http://models.example:11434
    auth: none
    models:
      - from-models
`)
	cat, err := Load(LoadOptions{Home: home, ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if cat.Selected() != "ollama/from-config" {
		t.Fatalf("selected %q", cat.Selected())
	}
	ollama, err := cat.Lookup("ollama")
	if err != nil {
		t.Fatal(err)
	}
	if ollama.BaseURL != "http://models.example:11434" || len(ollama.Models) != 1 || ollama.Models[0] != "from-models" {
		t.Fatalf("ollama %+v", ollama)
	}
	if _, err := cat.Lookup("lmstudio"); err == nil {
		t.Fatal("provider from config.yaml was loaded")
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

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
