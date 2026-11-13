package tools

import "testing"

func TestDetail(t *testing.T) {
	tests := []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{name: "read file", tool: "read_file", args: map[string]any{"path": "main.go"}, want: "path=main.go"},
		{name: "list dir", tool: "list_dir", args: map[string]any{}, want: "path=."},
		{name: "blank path", tool: "edit_file", args: map[string]any{"path": "  "}, want: "path=."},
		{name: "nil args", tool: "create_file", args: nil, want: "path=."},
		{name: "grep", tool: "grep", args: map[string]any{"pattern": "foo"}, want: `pattern="foo"`},
		{name: "search", tool: "web_search", args: map[string]any{"query": "golang http"}, want: `query="golang http"`},
		{name: "fetch", tool: "web_fetch", args: map[string]any{"url": "https://go.dev"}, want: "https://go.dev"},
		{name: "fetch blank", tool: "web_fetch", args: map[string]any{"url": "  "}, want: "url="},
		{name: "mcp search", tool: "mcp_search", args: map[string]any{"query": "create issue"}, want: "create issue"},
		{name: "mcp call", tool: "mcp_call", args: map[string]any{"server": "github", "tool": "create_issue"}, want: "github create_issue"},
		{name: "bash", tool: "bash", args: map[string]any{"command": "echo hello\necho again"}, want: "echo hello echo again"},
		{
			name: "definition",
			tool: "lsp_definition",
			args: map[string]any{"path": "a.go", "line": float64(4), "column": float64(2)},
			want: "a.go:4:2",
		},
		{name: "symbols", tool: "lsp_symbols", args: map[string]any{"query": "Foo"}, want: `query="Foo"`},
		{name: "symbols path", tool: "lsp_symbols", args: map[string]any{"path": "a.go"}, want: "path=a.go"},
		{name: "diagnostics", tool: "lsp_diagnostics", args: map[string]any{}, want: "workspace"},
		{name: "diagnostics path", tool: "lsp_diagnostics", args: map[string]any{"path": "a.go"}, want: "path=a.go"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Detail(tt.tool, tt.args); got != tt.want {
				t.Fatalf("Detail(%q) = %q, want %q", tt.tool, got, tt.want)
			}
		})
	}
}
