// Package tools exposes read-only filesystem tools jailed to one directory.
package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	defaultReadLines = 200
	maxReadBytes     = 32 * 1024
	maxGrepMatches   = 50
	maxLineBytes     = 1024 * 1024
)

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

// Set is the read-only tool surface for one working directory.
type Set struct {
	root  string
	tools []tool
}

// New returns the tools that may read root.
func New(root string) *Set {
	set := &Set{
		root:  root,
		tools: []tool{},
	}
	set.tools = []tool{
		{schema: listDirSchema(), run: set.listDir},
		{schema: readFileSchema(), run: set.readFile},
		{schema: grepSchema(), run: set.grep},
	}
	return set
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

func listDirSchema() Schema {
	return Schema{
		Name:        "list_dir",
		Description: "List one directory level. Each entry is marked file or dir. Path defaults to the working directory.",
		Parameters: objectSchema(
			map[string]any{
				"path": stringProp("Directory to list, relative to the working directory. Defaults to ."),
			},
			[]string{},
		),
	}
}

func readFileSchema() Schema {
	return Schema{
		Name:        "read_file",
		Description: "Read a window of a text file. The result starts with the total line count.",
		Parameters: objectSchema(
			map[string]any{
				"path":   stringProp("File to read, relative to the working directory."),
				"offset": intProp("First line to return, counting from 1. Defaults to 1."),
				"limit":  intProp("Maximum lines to return. Defaults to 200."),
			},
			[]string{"path"},
		),
	}
}

func grepSchema() Schema {
	return Schema{
		Name:        "grep",
		Description: "Search file contents with a regular expression. Skips .git. Returns at most 50 path:line:text matches.",
		Parameters: objectSchema(
			map[string]any{
				"pattern": stringProp("Go regular expression to search for."),
				"path":    stringProp("File or directory to search, relative to the working directory. Defaults to ."),
			},
			[]string{"pattern"},
		),
	}
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

func (s *Set) listDir(_ context.Context, args map[string]any) (string, error) {
	path, err := optionalPath(args)
	if err != nil {
		return "", err
	}
	full, err := resolve(s.root, path)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("path %q is not a directory", path)
	}

	entries, err := os.ReadDir(full)
	if err != nil {
		return "", fmt.Errorf("listing %q: %w", path, err)
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	var b strings.Builder
	fmt.Fprintf(&b, "entries: %d\n", len(entries))
	for _, entry := range entries {
		kind := "file"
		if entry.IsDir() {
			kind = "dir"
		}
		fmt.Fprintf(&b, "%s %s\n", kind, entry.Name())
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func (s *Set) readFile(_ context.Context, args map[string]any) (string, error) {
	path, ok, err := requiredString(args, "path")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("path is required")
	}

	offset, err := intArg(args, "offset", 1)
	if err != nil {
		return "", err
	}
	limit, err := intArg(args, "limit", defaultReadLines)
	if err != nil {
		return "", err
	}
	if offset < 1 {
		offset = 1
	}
	if limit < 1 {
		limit = defaultReadLines
	}

	full, err := resolve(s.root, path)
	if err != nil {
		return "", err
	}
	return readFileWindow(full, offset, limit)
}

func readFileWindow(full string, offset, limit int) (string, error) {
	file, err := os.Open(full)
	if err != nil {
		return "", fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var window strings.Builder
	lineNo := 0
	kept := 0
	truncated := false
	for scanner.Scan() {
		lineNo++
		if lineNo < offset || kept >= limit {
			continue
		}
		piece := fmt.Sprintf("%d|%s\n", lineNo, scanner.Text())
		overflows := window.Len()+len(piece) > maxReadBytes
		if truncated || overflows {
			truncated = true
			continue
		}
		window.WriteString(piece)
		kept++
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading file: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "lines: %d\n", lineNo)
	if truncated {
		b.WriteString("truncated: true\n")
	}
	b.WriteString(window.String())
	return strings.TrimRight(b.String(), "\n"), nil
}

func (s *Set) grep(_ context.Context, args map[string]any) (string, error) {
	pattern, ok, err := requiredString(args, "pattern")
	if err != nil {
		return "", err
	}
	if !ok || pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	path, err := optionalPath(args)
	if err != nil {
		return "", err
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("pattern %q: %w", pattern, err)
	}
	full, err := resolve(s.root, path)
	if err != nil {
		return "", err
	}
	realRoot, err := resolve(s.root, ".")
	if err != nil {
		return "", err
	}

	info, err := os.Stat(full)
	if err != nil {
		return "", fmt.Errorf("path %q: %w", path, err)
	}

	state := &grepState{
		root:    realRoot,
		re:      re,
		matches: []string{},
	}
	if info.IsDir() {
		if filepath.Base(full) == ".git" {
			return formatGrep(state), nil
		}
		err = filepath.WalkDir(full, func(current string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if entry.IsDir() && entry.Name() == ".git" {
				return filepath.SkipDir
			}
			if entry.IsDir() || !entry.Type().IsRegular() {
				return nil
			}
			if err := state.search(current); err != nil {
				return err
			}
			if state.capped {
				return errGrepCap
			}
			return nil
		})
		if err != nil && !errors.Is(err, errGrepCap) {
			return "", err
		}
	} else {
		if err := state.search(full); err != nil {
			return "", err
		}
	}
	return formatGrep(state), nil
}

var errGrepCap = errors.New("match cap reached")

type grepState struct {
	root    string
	re      *regexp.Regexp
	matches []string
	capped  bool
}

func (g *grepState) search(full string) error {
	if g.capped {
		return nil
	}
	file, err := os.Open(full)
	if err != nil {
		return nil
	}
	defer file.Close()

	rel, err := filepath.Rel(g.root, full)
	if err != nil {
		return fmt.Errorf("path %q: %w", full, err)
	}
	rel = filepath.ToSlash(rel)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		text := scanner.Text()
		if !g.re.MatchString(text) {
			continue
		}
		if len(g.matches) >= maxGrepMatches {
			g.capped = true
			return nil
		}
		g.matches = append(g.matches, fmt.Sprintf("%s:%d:%s", rel, lineNo, text))
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading %q: %w", rel, err)
	}
	return nil
}

func formatGrep(state *grepState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "matches: %d\n", len(state.matches))
	if state.capped {
		b.WriteString("truncated: true\n")
	}
	for _, match := range state.matches {
		b.WriteString(match)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
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
