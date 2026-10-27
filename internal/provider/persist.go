package provider

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

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

func setModel(doc *yaml.Node, model string) error {
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
		return fmt.Errorf("writing config: model must be set on a mapping")
	}

	value := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Value: model,
		Style: yaml.DoubleQuotedStyle,
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "model" {
			root.Content[i+1] = value
			return nil
		}
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "model"}
	root.Content = append([]*yaml.Node{key, value}, root.Content...)
	return nil
}
