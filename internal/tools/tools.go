// Package tools exposes the model's tools.
// Filesystem tools are jailed to one directory. The bash tool can read the rest of the
// disk and write anywhere.
// Web tools search and fetch public pages.
package tools

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const maxLineBytes = 1024 * 1024

// Schema is one tool the model is allowed to call.
type Schema struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type runner func(ctx context.Context, args map[string]any) (string, error)

type tool struct {
	schema Schema
	run    runner
}

// searcher is how file contents are searched.
// rg is preferred, then ag. The grep tool is registered only when both are missing.
type searcher string

const (
	searchRg   searcher = "rg"
	searchAg   searcher = "ag"
	searchGrep searcher = "grep"
)

// Set is the tool surface for one working directory.
type Set struct {
	root        string
	lang        Language
	mcp         MCP
	search      searcher
	tools       []tool
	bashTimeout time.Duration
}

// New returns the tools for root.
// The grep tool is omitted when rg or ag is on PATH.
func New(root string) *Set {
	return newSet(root, detectSearch(commandOnPath))
}

func newSet(root string, search searcher) *Set {
	switch search {
	case searchRg, searchAg, searchGrep:
	default:
		panic(fmt.Sprintf("unhandled search %q", search))
	}
	set := &Set{
		root:   root,
		search: search,
	}
	set.tools = []tool{
		{schema: listDirSchema(), run: set.listDir},
		{schema: readFileSchema(), run: set.readFile},
	}
	if search == searchGrep {
		set.tools = append(set.tools, tool{schema: grepSchema(), run: set.grep})
	}
	set.tools = append(set.tools,
		tool{schema: createFileSchema(), run: set.createFile},
		tool{schema: editFileSchema(), run: set.editFile},
		tool{schema: bashSchema(), run: set.bash},
		tool{schema: webSearchSchema(), run: set.webSearch},
		tool{schema: webFetchSchema(), run: set.webFetch},
	)
	return set
}

// Search is the file search the model should use: "rg", "ag", or "grep".
func (s *Set) Search() string {
	return string(s.search)
}

func detectSearch(exists func(string) bool) searcher {
	if exists("rg") {
		return searchRg
	}
	if exists("ag") {
		return searchAg
	}
	return searchGrep
}

func commandOnPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Schemas returns the tool definitions in call order.
func (s *Set) Schemas() []Schema {
	out := make([]Schema, 0, len(s.tools))
	for _, tool := range s.tools {
		out = append(out, tool.schema)
	}
	return out
}

// Run executes a tool.
// Failures are returned as text so the model can recover, and as an error so the UI can mark them.
func (s *Set) Run(ctx context.Context, name string, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return fmt.Sprintf("error: %s", err), err
	}
	if args == nil {
		args = map[string]any{}
	}

	for _, tool := range s.tools {
		if tool.schema.Name != name {
			continue
		}
		out, err := tool.run(ctx, args)
		if err != nil {
			return fmt.Sprintf("error: %s", err), err
		}
		return out, nil
	}
	err := fmt.Errorf("unknown tool %q", name)
	return fmt.Sprintf("error: %s", err), err
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringProp(description string) map[string]any {
	return map[string]any{
		"type":        "string",
		"description": description,
	}
}

func intProp(description string) map[string]any {
	return map[string]any{
		"type":        "integer",
		"description": description,
	}
}

func optionalPath(args map[string]any) (string, error) {
	path, ok, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	if !ok || strings.TrimSpace(path) == "" {
		return ".", nil
	}
	return path, nil
}

func requiredString(args map[string]any, key string) (string, bool, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return "", false, nil
	}
	text, ok := raw.(string)
	if !ok {
		return "", false, fmt.Errorf("%s: expected a string", key)
	}
	return text, true, nil
}

func intArg(args map[string]any, key string, fallback int) (int, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return fallback, nil
	}

	switch n := raw.(type) {
	case float64:
		if n != math.Trunc(n) {
			return 0, fmt.Errorf("%s: expected an integer", key)
		}
		return int(n), nil
	case int:
		return n, nil
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(n))
		if err != nil {
			return 0, fmt.Errorf("%s: %w", key, err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("%s: expected an integer", key)
	}
}
