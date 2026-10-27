package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"co/internal/openai"
)

// Endpoint is everything a chat call needs for one selection.
type Endpoint struct {
	Ref     Ref
	BaseURL string
	APIKey  string
	Model   string
	Compat  openai.Compat
	Probe   bool
}

// NeedsKeyError means a provider requires a key and none is available.
type NeedsKeyError struct {
	Provider string
}

func (e *NeedsKeyError) Error() string {
	return fmt.Sprintf("provider %q has no API key: run /connect %s", e.Provider, e.Provider)
}

// LoadOptions chooses which files and overrides build a catalog.
// ConfigFile replaces the home and project files. Host, when set, overrides
// only the ollama base URL.
type LoadOptions struct {
	Home       string
	ProjectDir string
	ConfigFile string
	FlagModel  string
	Host       string
	HostSet    bool
}

// Catalog is the merged set of providers for one process.
type Catalog struct {
	homeFile  string
	isolated  bool
	flag      string
	home      string
	project   string
	providers map[string]Provider
	keys      map[string]string
}

// Load reads config, credentials, and the built-in ollama provider.
func Load(opt LoadOptions) (*Catalog, error) {
	if opt.HostSet && strings.TrimSpace(opt.Host) == "" {
		return nil, fmt.Errorf("host is required: set --host or OLLAMA_HOST")
	}

	cat := &Catalog{
		homeFile: filepath.Join(opt.Home, ".co.yaml"),
		flag:     strings.TrimSpace(opt.FlagModel),
		keys:     map[string]string{},
	}

	var layers []map[string]Provider
	if opt.ConfigFile != "" {
		cat.isolated = true
		doc, found, err := readDoc(opt.ConfigFile)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("reading config %s: file not found", opt.ConfigFile)
		}
		parsed, err := providersFrom(doc)
		if err != nil {
			return nil, err
		}
		layers = append(layers, parsed)
		cat.project = strings.TrimSpace(doc.Model)
	} else {
		homeDoc, _, err := readDoc(cat.homeFile)
		if err != nil {
			return nil, err
		}
		projectDoc, _, err := readDoc(filepath.Join(opt.ProjectDir, ".co.yaml"))
		if err != nil {
			return nil, err
		}
		homeProviders, err := providersFrom(homeDoc)
		if err != nil {
			return nil, err
		}
		projectProviders, err := providersFrom(projectDoc)
		if err != nil {
			return nil, err
		}
		layers = append(layers, homeProviders, projectProviders)
		cat.home = strings.TrimSpace(homeDoc.Model)
		cat.project = strings.TrimSpace(projectDoc.Model)
	}

	cat.providers = mergeProviders(layers...)
	if opt.HostSet {
		p := cat.providers["ollama"]
		p.BaseURL = trimBase(opt.Host)
		cat.providers["ollama"] = p
	}

	keys, err := loadKeys(opt.Home)
	if err != nil {
		return nil, err
	}
	cat.keys = keys
	return cat, nil
}

// Selected is the model for this process before /models.
// Precedence is the flag, then ~/.co.yaml, then the project or --config file.
// An isolated catalog ignores ~/.co.yaml.
func (c *Catalog) Selected() string {
	if c.flag != "" {
		return c.flag
	}
	if !c.isolated && c.home != "" {
		return c.home
	}
	return c.project
}

// Isolated reports that /models must not write ~/.co.yaml.
func (c *Catalog) Isolated() bool {
	return c.isolated
}

// CanConnect reports whether a key can be stored for id.
func (c *Catalog) CanConnect(id string) error {
	p, err := c.Lookup(id)
	if err != nil {
		return err
	}
	if !p.NeedsKey() {
		return fmt.Errorf("provider %q does not take an API key", id)
	}
	return nil
}

// Lookup returns a provider by id.
func (c *Catalog) Lookup(id string) (Provider, error) {
	p, ok := c.providers[id]
	if !ok {
		return Provider{}, fmt.Errorf("unknown provider %q", id)
	}
	return p, nil
}

// Endpoint resolves a selection to a base URL, key, and compat flags.
// Ollama models are probed by the caller. Remote models are not.
func (c *Catalog) Endpoint(ref Ref) (Endpoint, error) {
	p, err := c.Lookup(ref.Provider)
	if err != nil {
		return Endpoint{}, err
	}
	if !p.LiveModels && !slices.Contains(p.Models, ref.Model) {
		return Endpoint{}, fmt.Errorf("unknown model %q", ref.String())
	}
	key, err := c.apiKey(p)
	if err != nil {
		return Endpoint{}, err
	}
	return Endpoint{
		Ref:     ref,
		BaseURL: p.BaseURL,
		APIKey:  key,
		Model:   ref.Model,
		Compat:  p.Compat,
		Probe:   p.ID == "ollama",
	}, nil
}

// List returns provider/model-id for every selectable model.
// A failed Ollama listing is returned with whatever other providers offered,
// and alone when Ollama was the only source.
func (c *Catalog) List(ctx context.Context) ([]string, error) {
	ids := make([]string, 0)
	var liveErr error
	for _, id := range c.ordered() {
		p := c.providers[id]
		models := p.Models
		if p.LiveModels {
			listed, err := listLive(ctx, p)
			if err != nil {
				liveErr = err
				continue
			}
			models = listed
		}
		for _, model := range models {
			ids = append(ids, Ref{Provider: p.ID, Model: model}.String())
		}
	}
	if liveErr != nil && len(ids) == 0 {
		return nil, liveErr
	}
	return ids, liveErr
}

// SaveKey stores a key for a provider that accepts one.
func (c *Catalog) SaveKey(id, key string) error {
	p, err := c.Lookup(id)
	if err != nil {
		return err
	}
	if !p.NeedsKey() {
		return fmt.Errorf("provider %q does not take an API key", id)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("api key is empty")
	}
	home := filepath.Dir(c.homeFile)
	if err := storeKey(home, id, key); err != nil {
		return err
	}
	c.keys[id] = key
	return nil
}

// SaveSelection writes the model key in ~/.co.yaml.
// An isolated catalog leaves that file alone.
func (c *Catalog) SaveSelection(model string) error {
	if c.isolated {
		return nil
	}
	if err := saveModelKey(c.homeFile, model); err != nil {
		return err
	}
	c.home = model
	return nil
}

func (c *Catalog) apiKey(p Provider) (string, error) {
	if !p.NeedsKey() {
		return "", nil
	}
	if p.APIKeyEnv != "" {
		if value := os.Getenv(p.APIKeyEnv); value != "" {
			return value, nil
		}
	}
	if key := c.keys[p.ID]; key != "" {
		return key, nil
	}
	return "", &NeedsKeyError{Provider: p.ID}
}

func (c *Catalog) ordered() []string {
	ids := make([]string, 0, len(c.providers))
	for id := range c.providers {
		if id != "ollama" {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	if _, ok := c.providers["ollama"]; ok {
		return append([]string{"ollama"}, ids...)
	}
	return ids
}

func listLive(ctx context.Context, p Provider) ([]string, error) {
	client := openai.NewEndpoint(p.BaseURL, "", p.Compat)
	ids, err := client.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing ollama models: %w", err)
	}
	return ids, nil
}
