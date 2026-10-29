package provider

import (
	"fmt"
	"os"
	"strings"

	"co/internal/openai"
	"gopkg.in/yaml.v3"
)

// Provider is one configured endpoint plus the models it offers.
type Provider struct {
	ID        string
	BaseURL   string
	APIKeyEnv string
	AuthNone  bool
	Models    []string
	Compat    openai.Compat
}

// NeedsKey reports whether a selection requires an API key.
// Auth none is the only provider that does not.
func (p Provider) NeedsKey() bool {
	return !p.AuthNone
}

type fileDoc struct {
	Model     string                  `yaml:"model"`
	Providers map[string]providerFile `yaml:"providers"`
}

type providerFile struct {
	BaseURL   string      `yaml:"base_url"`
	APIKeyEnv string      `yaml:"api_key_env"`
	Auth      string      `yaml:"auth"`
	Models    *[]string   `yaml:"models"`
	Compat    *compatFile `yaml:"compat"`
}

type compatFile struct {
	ReasoningEffort *bool `yaml:"reasoning_effort"`
	ReasoningDelta  *bool `yaml:"reasoning_delta"`
}

func readDoc(path string) (fileDoc, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fileDoc{}, false, nil
		}
		return fileDoc{}, false, fmt.Errorf("reading config %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return fileDoc{}, true, nil
	}
	var doc fileDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fileDoc{}, false, fmt.Errorf("reading config %s: %w", path, err)
	}
	return doc, true, nil
}

func providersFrom(doc fileDoc) (map[string]Provider, error) {
	out := map[string]Provider{}
	for id, raw := range doc.Providers {
		parsed, err := interpret(id, raw)
		if err != nil {
			return nil, err
		}
		out[id] = parsed
	}
	return out, nil
}

func interpret(id string, raw providerFile) (Provider, error) {
	if err := validProviderID(id); err != nil {
		return Provider{}, err
	}
	authNone, err := parseAuth(id, raw.Auth)
	if err != nil {
		return Provider{}, err
	}
	models, err := cleanModels(id, raw.Models)
	if err != nil {
		return Provider{}, err
	}
	if strings.TrimSpace(raw.BaseURL) == "" {
		return Provider{}, fmt.Errorf("provider %q: base_url is required", id)
	}
	p := Provider{
		ID:        id,
		BaseURL:   trimBase(raw.BaseURL),
		APIKeyEnv: strings.TrimSpace(raw.APIKeyEnv),
		AuthNone:  authNone,
		Models:    models,
	}
	applyCompat(&p, raw.Compat)
	return p, nil
}

func validProviderID(id string) error {
	if id == "" || strings.ContainsAny(id, "/ \t\r\n") {
		return fmt.Errorf("invalid provider id %q", id)
	}
	return nil
}

func parseAuth(id, raw string) (bool, error) {
	switch raw {
	case "", "none":
		return raw == "none", nil
	default:
		return false, fmt.Errorf("provider %q: auth %q is not valid", id, raw)
	}
}

func cleanModels(id string, raw *[]string) ([]string, error) {
	if raw == nil {
		return []string{}, nil
	}
	out := make([]string, 0, len(*raw))
	seen := map[string]bool{}
	for _, model := range *raw {
		model = strings.TrimSpace(model)
		if model == "" {
			return nil, fmt.Errorf("provider %q: model id is empty", id)
		}
		if seen[model] {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out, nil
}

func applyCompat(p *Provider, raw *compatFile) {
	if raw == nil {
		return
	}
	if raw.ReasoningEffort != nil {
		p.Compat.ReasoningEffort = *raw.ReasoningEffort
	}
	if raw.ReasoningDelta != nil {
		p.Compat.ReasoningDelta = *raw.ReasoningDelta
	}
}

func trimBase(raw string) string {
	return strings.TrimRight(strings.TrimSpace(raw), "/")
}

func mergeProviders(layers ...map[string]Provider) map[string]Provider {
	out := map[string]Provider{}
	for _, layer := range layers {
		for id, p := range layer {
			out[id] = p
		}
	}
	return out
}
