package lsp

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

func fileLines(text string) []string {
	if text == "" {
		return []string{""}
	}
	cut := strings.TrimSuffix(text, "\n")
	parts := strings.Split(cut, "\n")
	for i, part := range parts {
		parts[i] = strings.TrimSuffix(part, "\r")
	}
	return parts
}

// lspPosition converts a 1-based line and Unicode code-point column into the
// server's position encoding. Column 1 is the first code point on the line.
func lspPosition(text string, line, column int, encoding string) (position, error) {
	if line < 1 || column < 1 {
		return position{}, fmt.Errorf("line and column start at 1")
	}
	lines := fileLines(text)
	if line > len(lines) {
		return position{}, fmt.Errorf("line %d is past the end", line)
	}
	runes := []rune(lines[line-1])
	index := column - 1
	if index > len(runes) {
		index = len(runes)
	}
	return position{
		Line:      line - 1,
		Character: encodeOffset(string(runes[:index]), encoding),
	}, nil
}

func encodeOffset(prefix, encoding string) int {
	switch encoding {
	case "utf-8":
		return len(prefix)
	case "utf-32":
		return utf8.RuneCountInString(prefix)
	default:
		width := 0
		for _, r := range prefix {
			width += encodedWidth(r, "utf-16")
		}
		return width
	}
}

// runeColumn converts an LSP character offset back to a 1-based code-point column.
func runeColumn(line string, character int, encoding string) int {
	if character <= 0 {
		return 1
	}
	width := 0
	count := 0
	for _, r := range line {
		next := width + encodedWidth(r, encoding)
		if next > character {
			break
		}
		width = next
		count++
		if width == character {
			break
		}
	}
	return count + 1
}

func encodedWidth(r rune, encoding string) int {
	switch encoding {
	case "utf-8":
		return utf8.RuneLen(r)
	case "utf-32":
		return 1
	default:
		if r > 0xFFFF {
			return 2
		}
		return 1
	}
}
