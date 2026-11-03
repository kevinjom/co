package lsp

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxResults    = 50
	maxHoverRunes = 8 * 1024
)

func formatList(kind string, lines []string) string {
	if lines == nil {
		lines = []string{}
	}
	truncated := false
	if len(lines) > maxResults {
		lines = lines[:maxResults]
		truncated = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d\n", kind, len(lines))
	if truncated {
		b.WriteString("truncated: true\n")
	}
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func trimHover(text string) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return "hover: empty"
	}
	runes := []rune(text)
	if len(runes) <= maxHoverRunes {
		return text
	}
	return string(runes[:maxHoverRunes]) + "\ntruncated: true"
}

func (c *client) display(loc location) string {
	path, err := uriToPath(loc.URI)
	if err != nil {
		return loc.URI
	}
	line := loc.Range.Start.Line + 1
	column := loc.Range.Start.Character + 1
	if outside(c.root, path) {
		return fmt.Sprintf("%s:%d:%d outside the working directory", path, line, column)
	}
	if text, ok := readLine(path, loc.Range.Start.Line); ok {
		column = runeColumn(text, loc.Range.Start.Character, c.encoding)
	}
	rel, err := filepath.Rel(c.root, path)
	if err == nil {
		path = filepath.ToSlash(rel)
	}
	return fmt.Sprintf("%s:%d:%d", path, line, column)
}

func outside(root, path string) bool {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel)
}

func readLine(path string, line int) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	n := 0
	for scanner.Scan() {
		if n == line {
			return scanner.Text(), true
		}
		n++
	}
	return "", false
}

func prefixLines(command string, lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, command+" "+line)
	}
	return out
}
