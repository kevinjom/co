package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxGrepMatches = 50

func init() {
	registerDetail("grep", grepDetail)
}

func grepDetail(args map[string]any) string {
	pattern, _ := args["pattern"].(string)
	return fmt.Sprintf("pattern=%q", pattern)
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
