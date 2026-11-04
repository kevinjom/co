package provider

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kevinjom/co/internal/openai"
	"gopkg.in/yaml.v3"
)

// saveModelKey sets the model field and leaves every other key in place.
// A missing file is created with only that field.
func saveModelKey(path, model string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	doc, mode, err := loadYAML(path)
	if err != nil {
		return err
	}
	if err := setModel(doc, model); err != nil {
		return err
	}
	root, err := mappingRoot(doc)
	if err != nil {
		return err
	}
	orderRoot(root)
	return writeDoc(path, doc, mode)
}

// ProviderSpec is one provider block stored in a models file.
// CompatOnCreate is written only when that provider is new.
// Model replaces the model key in the config file. DefaultModel fills it only when empty.
type ProviderSpec struct {
	ID             string
	BaseURL        string
	AuthNone       bool
	Models         []string
	CompatOnCreate *openai.Compat
	Model          string
	DefaultModel   string
}

// WriteProvider upserts one provider in modelsPath.
// The model key is written to configPath. Passing one path for both keeps
// model above providers in that file, which is how --config stays a single file.
func WriteProvider(configPath, modelsPath string, spec ProviderSpec) error {
	if err := validProviderID(spec.ID); err != nil {
		return err
	}
	base := trimBase(spec.BaseURL)
	if base == "" {
		return fmt.Errorf("provider %q: base_url is required", spec.ID)
	}
	if configPath == modelsPath {
		return upsertProviderFile(configPath, spec, base, true)
	}
	if err := upsertProviderFile(modelsPath, spec, base, false); err != nil {
		return err
	}
	return applyModel(configPath, spec)
}

func upsertProviderFile(path string, spec ProviderSpec, base string, withModel bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	doc, mode, err := loadYAML(path)
	if err != nil {
		return err
	}
	root, err := mappingRoot(doc)
	if err != nil {
		return err
	}
	if err := upsertProvider(root, spec, base); err != nil {
		return err
	}
	if withModel {
		if _, err := applyModelNode(doc, spec); err != nil {
			return err
		}
	}
	orderRoot(root)
	return writeDoc(path, doc, mode)
}

// applyModel writes Model, or DefaultModel when the key is empty.
// A DefaultModel that loses to an existing model leaves the file untouched.
func applyModel(path string, spec ProviderSpec) error {
	if spec.Model == "" && spec.DefaultModel == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	doc, mode, err := loadYAML(path)
	if err != nil {
		return err
	}
	root, err := mappingRoot(doc)
	if err != nil {
		return err
	}
	wrote, err := applyModelNode(doc, spec)
	if err != nil || !wrote {
		return err
	}
	orderRoot(root)
	return writeDoc(path, doc, mode)
}

func applyModelNode(doc *yaml.Node, spec ProviderSpec) (bool, error) {
	root, err := mappingRoot(doc)
	if err != nil {
		return false, err
	}
	switch {
	case spec.Model != "":
		if err := setModel(doc, spec.Model); err != nil {
			return false, err
		}
		return true, nil
	case spec.DefaultModel != "" && modelBlank(root):
		if err := setModel(doc, spec.DefaultModel); err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, nil
	}
}

func writeDoc(path string, doc *yaml.Node, mode os.FileMode) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), mode); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	return nil
}

func loadYAML(path string) (*yaml.Node, os.FileMode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &yaml.Node{Kind: yaml.DocumentNode}, 0o644, nil
		}
		return nil, 0, fmt.Errorf("reading config %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("reading config %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, 0, fmt.Errorf("reading config %s: %w", path, err)
	}
	if doc.Kind == 0 {
		doc.Kind = yaml.DocumentNode
	}
	return &doc, info.Mode().Perm(), nil
}

func mappingRoot(doc *yaml.Node) (*yaml.Node, error) {
	root := doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			root = &yaml.Node{Kind: yaml.MappingNode}
			doc.Content = []*yaml.Node{root}
		} else {
			root = doc.Content[0]
		}
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("writing config: root must be a mapping")
	}
	return root, nil
}

func setModel(doc *yaml.Node, model string) error {
	root, err := mappingRoot(doc)
	if err != nil {
		return err
	}
	setNode(root, "model", strNode(model, yaml.DoubleQuotedStyle))
	return nil
}

// savePlainKey sets one unquoted string and leaves every other key in place.
func savePlainKey(path, key, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	doc, mode, err := loadYAML(path)
	if err != nil {
		return err
	}
	root, err := mappingRoot(doc)
	if err != nil {
		return err
	}
	setNode(root, key, strNode(value, 0))
	orderRoot(root)
	return writeDoc(path, doc, mode)
}

func modelBlank(root *yaml.Node) bool {
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "model" {
			continue
		}
		value := root.Content[i+1]
		return value.Kind != yaml.ScalarNode || strings.TrimSpace(value.Value) == ""
	}
	return true
}

// orderRoot puts model first and providers directly under it.
func orderRoot(root *yaml.Node) {
	var modelKey, modelVal, providersKey, providersVal *yaml.Node
	rest := make([]*yaml.Node, 0, len(root.Content))
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, val := root.Content[i], root.Content[i+1]
		switch key.Value {
		case "model":
			modelKey, modelVal = key, val
		case "providers":
			providersKey, providersVal = key, val
		default:
			rest = append(rest, key, val)
		}
	}
	out := make([]*yaml.Node, 0, len(root.Content))
	if modelKey != nil {
		out = append(out, modelKey, modelVal)
	}
	if providersKey != nil {
		out = append(out, providersKey, providersVal)
	}
	out = append(out, rest...)
	root.Content = out
}

func upsertProvider(root *yaml.Node, spec ProviderSpec, base string) error {
	providers, err := ensureMap(root, "providers")
	if err != nil {
		return err
	}
	block, created, err := ensureProvider(providers, spec.ID)
	if err != nil {
		return err
	}
	setNode(block, "base_url", strNode(base, 0))
	if spec.AuthNone {
		setNode(block, "auth", strNode("none", 0))
	}
	setNode(block, "models", seqNode(spec.Models))
	if created && spec.CompatOnCreate != nil {
		setNode(block, "compat", compatNode(spec.CompatOnCreate))
	}
	return nil
}

func ensureMap(parent *yaml.Node, key string) (*yaml.Node, error) {
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value != key {
			continue
		}
		child := parent.Content[i+1]
		if child.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("writing config: %s must be a mapping", key)
		}
		return child, nil
	}
	child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		child,
	)
	return child, nil
}

func ensureProvider(providers *yaml.Node, id string) (*yaml.Node, bool, error) {
	for i := 0; i+1 < len(providers.Content); i += 2 {
		if providers.Content[i].Value != id {
			continue
		}
		block := providers.Content[i+1]
		if block.Kind != yaml.MappingNode {
			return nil, false, fmt.Errorf("writing config: provider %q must be a mapping", id)
		}
		return block, false, nil
	}
	block := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	providers.Content = append(providers.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: id},
		block,
	)
	return block, true, nil
}

func setNode(mapping *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content[i+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		value,
	)
}

func strNode(value string, style yaml.Style) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: value,
		Style: style,
	}
}

func seqNode(items []string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, item := range items {
		node.Content = append(node.Content, strNode(item, yaml.DoubleQuotedStyle))
	}
	return node
}

func compatNode(compat *openai.Compat) *yaml.Node {
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setNode(node, "reasoning_effort", boolNode(compat.ReasoningEffort))
	setNode(node, "reasoning_delta", boolNode(compat.ReasoningDelta))
	return node
}

func boolNode(value bool) *yaml.Node {
	text := "false"
	if value {
		text = "true"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: text}
}
