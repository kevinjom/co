package tools

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

const (
	defaultReadLines = 200
	maxReadBytes     = 32 * 1024
)

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
