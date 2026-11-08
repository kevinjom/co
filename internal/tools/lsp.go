package tools

import (
	"context"
	"fmt"
	"strings"
)

// Language is a read-only language server.
// Paths passed in are absolute and already inside the working directory.
// An empty path on Symbols or Diagnostics means the whole workspace.
type Language interface {
	Definition(ctx context.Context, path string, line, column int) (string, error)
	References(ctx context.Context, path string, line, column int) (string, error)
	Hover(ctx context.Context, path string, line, column int) (string, error)
	Symbols(ctx context.Context, path, query string) (string, error)
	Diagnostics(ctx context.Context, path string) (string, error)
}

// EnableLSP adds the language-server tools.
// A nil service, or a second call, does nothing.
func (s *Set) EnableLSP(lang Language) {
	if lang == nil || s.lang != nil {
		return
	}
	s.lang = lang
	s.tools = append(s.tools,
		tool{schema: lspDefinitionSchema(), run: s.lspDefinition},
		tool{schema: lspReferencesSchema(), run: s.lspReferences},
		tool{schema: lspHoverSchema(), run: s.lspHover},
		tool{schema: lspSymbolsSchema(), run: s.lspSymbols},
		tool{schema: lspDiagnosticsSchema(), run: s.lspDiagnostics},
	)
}

func init() {
	registerDetail("lsp_definition", positionDetail)
	registerDetail("lsp_references", positionDetail)
	registerDetail("lsp_hover", positionDetail)
	registerDetail("lsp_symbols", symbolsDetail)
	registerDetail("lsp_diagnostics", diagnosticsDetail)
}

func positionDetail(args map[string]any) string {
	return fmt.Sprintf("%s:%d:%d", detailPath(args), detailInt(args["line"]), detailInt(args["column"]))
}

func symbolsDetail(args map[string]any) string {
	if query, _ := args["query"].(string); strings.TrimSpace(query) != "" {
		return fmt.Sprintf("query=%q", query)
	}
	return "path=" + detailPath(args)
}

func diagnosticsDetail(args map[string]any) string {
	path, _ := args["path"].(string)
	if strings.TrimSpace(path) == "" {
		return "workspace"
	}
	return "path=" + path
}

func lspDefinitionSchema() Schema {
	return Schema{
		Name:        "lsp_definition",
		Description: "Go to the definition of the symbol at a line and column. Line and column start at 1. Prefer this over grep for a declaration.",
		Parameters:  objectSchema(positionProps(), []string{"path", "line", "column"}),
	}
}

func lspReferencesSchema() Schema {
	return Schema{
		Name:        "lsp_references",
		Description: "Find references to the symbol at a line and column, including the declaration. Line and column start at 1.",
		Parameters:  objectSchema(positionProps(), []string{"path", "line", "column"}),
	}
}

func lspHoverSchema() Schema {
	return Schema{
		Name:        "lsp_hover",
		Description: "Show the language server's hover text for the symbol at a line and column. Line and column start at 1. Column counts Unicode code points.",
		Parameters:  objectSchema(positionProps(), []string{"path", "line", "column"}),
	}
}

func lspSymbolsSchema() Schema {
	return Schema{
		Name:        "lsp_symbols",
		Description: "List symbols. Set path for one file, or query to search the workspace. Set one of them, not both.",
		Parameters: objectSchema(
			map[string]any{
				"path":  stringProp("File inside the working directory. Set this or query, not both."),
				"query": stringProp("Workspace symbol query. Set this or path, not both."),
			},
			[]string{},
		),
	}
}

func lspDiagnosticsSchema() Schema {
	return Schema{
		Name:        "lsp_diagnostics",
		Description: "Diagnostics from the language server. Set path for one file, or omit it for the workspace.",
		Parameters: objectSchema(
			map[string]any{
				"path": stringProp("File inside the working directory. Omit for the whole workspace."),
			},
			[]string{},
		),
	}
}

func positionProps() map[string]any {
	return map[string]any{
		"path":   stringProp("File inside the working directory."),
		"line":   intProp("Line number, starting at 1."),
		"column": intProp("Column number, starting at 1, counted in Unicode code points."),
	}
}

func (s *Set) lspDefinition(ctx context.Context, args map[string]any) (string, error) {
	if err := s.needLang(); err != nil {
		return "", err
	}
	path, line, column, err := positionArgs(args)
	if err != nil {
		return "", err
	}
	full, err := resolve(s.root, path)
	if err != nil {
		return "", err
	}
	return s.lang.Definition(ctx, full, line, column)
}

func (s *Set) lspReferences(ctx context.Context, args map[string]any) (string, error) {
	if err := s.needLang(); err != nil {
		return "", err
	}
	path, line, column, err := positionArgs(args)
	if err != nil {
		return "", err
	}
	full, err := resolve(s.root, path)
	if err != nil {
		return "", err
	}
	return s.lang.References(ctx, full, line, column)
}

func (s *Set) lspHover(ctx context.Context, args map[string]any) (string, error) {
	if err := s.needLang(); err != nil {
		return "", err
	}
	path, line, column, err := positionArgs(args)
	if err != nil {
		return "", err
	}
	full, err := resolve(s.root, path)
	if err != nil {
		return "", err
	}
	return s.lang.Hover(ctx, full, line, column)
}

func (s *Set) lspSymbols(ctx context.Context, args map[string]any) (string, error) {
	if err := s.needLang(); err != nil {
		return "", err
	}
	path, hasPath, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	query, hasQuery, err := requiredString(args, "query")
	if err != nil {
		return "", err
	}
	pathSet := hasPath && strings.TrimSpace(path) != ""
	querySet := hasQuery && strings.TrimSpace(query) != ""
	if pathSet == querySet {
		return "", fmt.Errorf("set path or query")
	}
	if querySet {
		return s.lang.Symbols(ctx, "", query)
	}
	full, err := resolve(s.root, path)
	if err != nil {
		return "", err
	}
	return s.lang.Symbols(ctx, full, "")
}

func (s *Set) lspDiagnostics(ctx context.Context, args map[string]any) (string, error) {
	if err := s.needLang(); err != nil {
		return "", err
	}
	path, hasPath, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	if !hasPath || strings.TrimSpace(path) == "" {
		return s.lang.Diagnostics(ctx, "")
	}
	full, err := resolve(s.root, path)
	if err != nil {
		return "", err
	}
	return s.lang.Diagnostics(ctx, full)
}

func (s *Set) needLang() error {
	if s.lang == nil {
		return fmt.Errorf("language server is not available")
	}
	return nil
}

func positionArgs(args map[string]any) (string, int, int, error) {
	path, ok, err := requiredString(args, "path")
	if err != nil {
		return "", 0, 0, err
	}
	if !ok || strings.TrimSpace(path) == "" {
		return "", 0, 0, fmt.Errorf("path is required")
	}
	line, err := intArg(args, "line", 0)
	if err != nil {
		return "", 0, 0, err
	}
	column, err := intArg(args, "column", 0)
	if err != nil {
		return "", 0, 0, err
	}
	if line < 1 || column < 1 {
		return "", 0, 0, fmt.Errorf("line and column start at 1")
	}
	return path, line, column, nil
}
