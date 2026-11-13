package tools

import (
	"context"
	"strings"
	"testing"
)

type fakeMCP struct {
	query  string
	server string
	tool   string
	args   map[string]any
}

func (f *fakeMCP) Search(_ context.Context, query string) (string, error) {
	f.query = query
	return "hits: 0", nil
}

func (f *fakeMCP) Call(_ context.Context, server, tool string, args map[string]any) (string, error) {
	f.server = server
	f.tool = tool
	f.args = args
	return "made", nil
}

func TestMCPTools(t *testing.T) {
	set := New(t.TempDir())
	if mcpSchemaNamed(set, "mcp_search") {
		t.Fatal("mcp tools registered without a hub")
	}
	fake := &fakeMCP{}
	set.EnableMCP(fake)
	set.EnableMCP(fake)
	if !mcpSchemaNamed(set, "mcp_search") || !mcpSchemaNamed(set, "mcp_call") {
		t.Fatal("missing mcp tools")
	}
	if n := strings.Count(schemaList(set), "mcp_search"); n != 1 {
		t.Fatalf("mcp_search count %d", n)
	}

	got, err := set.Run(t.Context(), "mcp_search", map[string]any{"query": "  "})
	if err == nil || !strings.Contains(got, "query is empty") {
		t.Fatalf("got %q err %v", got, err)
	}
	got, err = set.Run(t.Context(), "mcp_search", map[string]any{"query": "create issue"})
	if err != nil || got != "hits: 0" || fake.query != "create issue" {
		t.Fatalf("got %q err %v query %q", got, err, fake.query)
	}
	got, err = set.Run(t.Context(), "mcp_call", map[string]any{
		"server": " github ",
		"tool":   "create_issue",
		"arguments": map[string]any{
			"title": "hi",
		},
	})
	if err != nil || got != "made" || fake.server != "github" || fake.tool != "create_issue" || fake.args["title"] != "hi" {
		t.Fatalf("got %q err %v fake %#v", got, err, fake)
	}
}

func mcpSchemaNamed(set *Set, name string) bool {
	for _, schema := range set.Schemas() {
		if schema.Name == name {
			return true
		}
	}
	return false
}

func schemaList(set *Set) string {
	names := make([]string, 0, len(set.Schemas()))
	for _, schema := range set.Schemas() {
		names = append(names, schema.Name)
	}
	return strings.Join(names, " ")
}
