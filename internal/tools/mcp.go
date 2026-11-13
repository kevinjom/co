package tools

import (
	"context"
	"fmt"
	"strings"
)

// MCP searches and calls tools on configured MCP servers.
type MCP interface {
	Search(ctx context.Context, query string) (string, error)
	Call(ctx context.Context, server, tool string, args map[string]any) (string, error)
}

// EnableMCP adds mcp_search and mcp_call.
// A nil service, or a second call, does nothing.
func (s *Set) EnableMCP(cat MCP) {
	if cat == nil || s.mcp != nil {
		return
	}
	s.mcp = cat
	s.tools = append(s.tools,
		tool{schema: mcpSearchSchema(), run: s.mcpSearch},
		tool{schema: mcpCallSchema(), run: s.mcpCall},
	)
}

func init() {
	registerDetail("mcp_search", mcpSearchDetail)
	registerDetail("mcp_call", mcpCallDetail)
}

func mcpSearchDetail(args map[string]any) string {
	query, _ := args["query"].(string)
	return strings.Join(strings.Fields(query), " ")
}

func mcpCallDetail(args map[string]any) string {
	server, _ := args["server"].(string)
	name, _ := args["tool"].(string)
	return strings.TrimSpace(server + " " + name)
}

func mcpSearchSchema() Schema {
	return Schema{
		Name:        "mcp_search",
		Description: "Search configured MCP servers for tools. Pass words from the task. Results include each tool's input schema.",
		Parameters: objectSchema(
			map[string]any{
				"query": stringProp("Words to match against tool names and descriptions."),
			},
			[]string{"query"},
		),
	}
}

func mcpCallSchema() Schema {
	return Schema{
		Name:        "mcp_call",
		Description: "Call one MCP tool. Use the server, tool, and arguments from an mcp_search result.",
		Parameters: objectSchema(
			map[string]any{
				"server": stringProp("Server name from mcp_search."),
				"tool":   stringProp("Tool name from mcp_search."),
				"arguments": map[string]any{
					"type":        "object",
					"description": "Arguments for the tool, taken from its input schema.",
				},
			},
			[]string{"server", "tool"},
		),
	}
}

func (s *Set) mcpSearch(ctx context.Context, args map[string]any) (string, error) {
	if err := s.requireMCP(); err != nil {
		return "", err
	}
	query, ok, err := requiredString(args, "query")
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is empty")
	}
	return s.mcp.Search(ctx, query)
}

func (s *Set) mcpCall(ctx context.Context, args map[string]any) (string, error) {
	if err := s.requireMCP(); err != nil {
		return "", err
	}
	server, ok, err := requiredString(args, "server")
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(server) == "" {
		return "", fmt.Errorf("server is required")
	}
	name, ok, err := requiredString(args, "tool")
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("tool is required")
	}
	arguments, err := objectArg(args, "arguments")
	if err != nil {
		return "", err
	}
	return s.mcp.Call(ctx, strings.TrimSpace(server), strings.TrimSpace(name), arguments)
}

func (s *Set) requireMCP() error {
	if s.mcp == nil {
		return fmt.Errorf("mcp is not available")
	}
	return nil
}

func objectArg(args map[string]any, key string) (map[string]any, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return map[string]any{}, nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected an object", key)
	}
	return obj, nil
}
