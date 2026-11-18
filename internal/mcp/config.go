package mcp

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// HomePath is the user MCP file, ~/.co/mcp.yaml.
// A missing file is left missing. --config does not change this path.
func HomePath(home string) string {
	return filepath.Join(home, ".co", "mcp.yaml")
}

// Server is one Streamable HTTP endpoint.
// Header values are sent exactly as written.
// ClientID, when set, is the OAuth client used instead of dynamic registration.
type Server struct {
	Name         string
	URL          string
	Headers      map[string]string
	ClientID     string
	ClientSecret string
}

// Load reads ~/.co/mcp.yaml.
// A missing or empty file returns no servers. Broken YAML, a server without
// a url, or a name outside letters, digits, underscore, and hyphen is an error.
// Servers are returned in file order.
func Load(home string) ([]Server, error) {
	data, err := os.ReadFile(HomePath(home))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading mcp.yaml: %w", err)
	}
	return parse(data)
}

func parse(data []byte) ([]Server, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("reading mcp.yaml: %w", err)
	}
	root, empty := documentRoot(&doc)
	if empty {
		return nil, nil
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("reading mcp.yaml: expected a mapping")
	}
	if len(root.Content)%2 != 0 {
		return nil, fmt.Errorf("reading mcp.yaml: expected a mapping")
	}

	seen := map[string]bool{}
	servers := make([]Server, 0, len(root.Content)/2)
	for i := 0; i < len(root.Content); i += 2 {
		name := root.Content[i].Value
		if !validName(name) {
			return nil, fmt.Errorf("reading mcp.yaml: invalid server name %q", name)
		}
		if seen[name] {
			return nil, fmt.Errorf("reading mcp.yaml: duplicate server %q", name)
		}
		seen[name] = true
		server, err := decodeServer(name, root.Content[i+1])
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, nil
}

func documentRoot(doc *yaml.Node) (*yaml.Node, bool) {
	if doc == nil || doc.Kind == 0 {
		return nil, true
	}
	root := doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil, true
		}
		root = doc.Content[0]
	}
	if root.Kind == yaml.ScalarNode && (root.Tag == "!!null" || root.Value == "" || root.Value == "null") {
		return nil, true
	}
	return root, false
}

func decodeServer(name string, node *yaml.Node) (Server, error) {
	if node.Kind != yaml.MappingNode || len(node.Content)%2 != 0 {
		return Server{}, fmt.Errorf("reading mcp.yaml: server %q: expected a mapping", name)
	}
	var (
		rawURL       string
		seenURL      bool
		headers      map[string]string
		clientID     string
		seenClient   bool
		clientSecret string
	)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]
		switch key {
		case "url":
			decoded, err := scalarString(name, "url", val)
			if err != nil {
				return Server{}, err
			}
			rawURL = decoded
			seenURL = true
		case "headers":
			decoded, err := decodeHeaders(name, val)
			if err != nil {
				return Server{}, err
			}
			headers = decoded
		case "client_id":
			decoded, err := scalarString(name, "client_id", val)
			if err != nil {
				return Server{}, err
			}
			clientID = decoded
			seenClient = true
		case "client_secret":
			decoded, err := scalarString(name, "client_secret", val)
			if err != nil {
				return Server{}, err
			}
			clientSecret = decoded
		default:
			return Server{}, fmt.Errorf("reading mcp.yaml: server %q: unknown field %q", name, key)
		}
	}
	if !seenURL || strings.TrimSpace(rawURL) == "" {
		return Server{}, fmt.Errorf("reading mcp.yaml: server %q: url is required", name)
	}
	if seenClient && strings.TrimSpace(clientID) == "" {
		return Server{}, fmt.Errorf("reading mcp.yaml: server %q: client_id is empty", name)
	}
	if clientSecret != "" && clientID == "" {
		return Server{}, fmt.Errorf("reading mcp.yaml: server %q: client_secret requires client_id", name)
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return Server{}, fmt.Errorf("reading mcp.yaml: server %q: url must be http or https", name)
	}
	return Server{
		Name:         name,
		URL:          rawURL,
		Headers:      headers,
		ClientID:     clientID,
		ClientSecret: clientSecret,
	}, nil
}

func scalarString(name, field string, val *yaml.Node) (string, error) {
	switch {
	case val.Kind != yaml.ScalarNode, val.Tag == "!!null", val.Tag == "!!int", val.Tag == "!!float", val.Tag == "!!bool":
		return "", fmt.Errorf("reading mcp.yaml: server %q: %s must be a string", name, field)
	default:
		return val.Value, nil
	}
}

func decodeHeaders(name string, node *yaml.Node) (map[string]string, error) {
	if node.Kind == yaml.ScalarNode && (node.Tag == "!!null" || node.Value == "null" || node.Value == "") {
		return nil, nil
	}
	if node.Kind != yaml.MappingNode || len(node.Content)%2 != 0 {
		return nil, fmt.Errorf("reading mcp.yaml: server %q: headers must be a mapping", name)
	}
	if len(node.Content) == 0 {
		return nil, nil
	}
	headers := make(map[string]string, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		val := node.Content[i+1]
		if strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("reading mcp.yaml: server %q: header name is empty", name)
		}
		if val.Kind != yaml.ScalarNode || val.Tag == "!!null" {
			return nil, fmt.Errorf("reading mcp.yaml: server %q: header %q must be a string", name, key)
		}
		headers[key] = val.Value
	}
	return headers, nil
}

func validName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}
