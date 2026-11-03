package provider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kevinjom/co/internal/openai"
)

// Endpoint is everything a chat call needs for one selection.
type Endpoint struct {
	Ref     Ref
	BaseURL string
	APIKey  string
	Model   string
	Compat  openai.Compat
}

// NeedsKeyError means a provider requires a key and none is available.
type NeedsKeyError struct {
	Provider string
}

func (e *NeedsKeyError) Error() string {
	return fmt.Sprintf("provider %q has no API key: run /connect %s", e.Provider, e.Provider)
}

// LoadOptions chooses which files and overrides build a catalog.
// ConfigFile replaces ~/.co/config.yaml, ~/.co/models.yaml, and the project file.
// That file may contain the default model, flags, and providers.
type LoadOptions struct {
	Home       string
	ProjectDir string
	ConfigFile string
	FlagModel  string
}

// Catalog is the merged set of providers for one process.
type Catalog struct {
	homeDir    string
	configFile string
	isolated   bool
	flag       string
	home       string
	project    string
	providers  map[string]Provider
	keys       map[string]string
}

// HomeConfig is the user settings file, ~/.co/config.yaml.
// It holds the default model and the supported flags.
func HomeConfig(home string) string {
	return filepath.Join(home, ".co", "config.yaml")
}

// HomeModels is the provider catalog, ~/.co/models.yaml.
func HomeModels(home string) string {
	return filepath.Join(home, ".co", "models.yaml")
}

// Load reads settings, the provider catalog, and credentials.
func Load(opt LoadOptions) (*Catalog, error) {
	cat := &Catalog{
		homeDir:    opt.Home,
		configFile: HomeConfig(opt.Home),
		flag:       strings.TrimSpace(opt.FlagModel),
		keys:       map[string]string{},
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
		homeDoc, _, err := readDoc(cat.configFile)
		if err != nil {
			return nil, err
		}
		modelsDoc, _, err := readDoc(HomeModels(opt.Home))
		if err != nil {
			return nil, err
		}
		projectDoc, _, err := readDoc(filepath.Join(opt.ProjectDir, ".co.yaml"))
		if err != nil {
			return nil, err
		}
		homeProviders, err := providersFrom(modelsDoc)
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

	keys, err := loadKeys(opt.Home)
	if err != nil {
		return nil, err
	}
	cat.keys = keys
	return cat, nil
}

// Selected is the model for this process before /models.
// Precedence is the flag, then ~/.co/config.yaml, then the project or --config file.
// An isolated catalog ignores ~/.co/config.yaml.
func (c *Catalog) Selected() string {
	if c.flag != "" {
		return c.flag
	}
	if !c.isolated && c.home != "" {
		return c.home
	}
	return c.project
}

// Isolated reports that /models must not write ~/.co/config.yaml.
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

// UnknownProviderError means the selection names a provider that is not configured.
type UnknownProviderError struct {
	Provider string
}

func (e *UnknownProviderError) Error() string {
	return fmt.Sprintf("unknown provider %q", e.Provider)
}

// Lookup returns a provider by id.
func (c *Catalog) Lookup(id string) (Provider, error) {
	p, ok := c.providers[id]
	if !ok {
		return Provider{}, &UnknownProviderError{Provider: id}
	}
	return p, nil
}

// Endpoint resolves a selection to a base URL, key, and compat flags.
func (c *Catalog) Endpoint(ref Ref) (Endpoint, error) {
	p, err := c.Lookup(ref.Provider)
	if err != nil {
		return Endpoint{}, err
	}
	if !slices.Contains(p.Models, ref.Model) {
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
	}, nil
}

// Providers returns ids that accept an API key, in sorted order.
func (c *Catalog) Providers() []string {
	ids := []string{}
	for _, id := range c.ordered() {
		if c.providers[id].NeedsKey() {
			ids = append(ids, id)
		}
	}
	return ids
}

// List returns provider/model-id for every configured model.
func (c *Catalog) List(context.Context) ([]string, error) {
	ids := make([]string, 0)
	for _, id := range c.ordered() {
		p := c.providers[id]
		for _, model := range p.Models {
			ids = append(ids, Ref{Provider: p.ID, Model: model}.String())
		}
	}
	return ids, nil
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
	if err := storeKey(c.homeDir, id, key); err != nil {
		return err
	}
	c.keys[id] = key
	return nil
}

// SaveSelection writes the model key in ~/.co/config.yaml.
// An isolated catalog leaves that file alone.
func (c *Catalog) SaveSelection(model string) error {
	if c.isolated {
		return nil
	}
	if err := saveModelKey(c.configFile, model); err != nil {
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
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
