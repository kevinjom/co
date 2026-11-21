package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/kevinjom/co/internal/tools"
)

type lineRead func(context.Context) (string, error)

func lineAsker(w io.Writer, read lineRead, interactive bool) tools.Asker {
	return func(ctx context.Context, question tools.Question) (string, error) {
		if !interactive {
			return "", fmt.Errorf("ask_user needs a terminal")
		}
		return askLine(ctx, w, read, question)
	}
}

func askLine(ctx context.Context, w io.Writer, read lineRead, question tools.Question) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fmt.Fprintln(w, question.Text)
	for i, option := range question.Options {
		fmt.Fprintf(w, "%d. %s\n", i+1, option)
	}
	if hint := askHint(question); hint != "" {
		fmt.Fprintln(w, hint)
	}
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		fmt.Fprint(w, "reply: ")
		line, err := read(ctx)
		if err != nil {
			return "", err
		}
		text, ok, err := parseAskReply(line, question.Options, question.Multiple)
		if err != nil {
			fmt.Fprintln(w, err)
			continue
		}
		if !ok {
			continue
		}
		return text, nil
	}
}

func askHint(question tools.Question) string {
	switch {
	case len(question.Options) == 0:
		return ""
	case question.Multiple:
		return "Reply with option numbers, your own words, or both as: 1 2 -- words"
	default:
		return "Reply with an option number, your own words, or both as: 1 -- words"
	}
}

// parseAskReply reads one line of a terminal answer.
// A bare number selects that option. Words are the typed answer.
// "1 2 -- words" selects options and keeps the words. An empty line is not an answer.
func parseAskReply(line string, options []string, multiple bool) (string, bool, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false, nil
	}
	if len(options) == 0 {
		return line, true, nil
	}

	head := line
	typed := ""
	switch {
	case line == "--":
		return "", false, nil
	case strings.HasPrefix(line, "-- "):
		typed = strings.TrimSpace(strings.TrimPrefix(line, "-- "))
		head = ""
	case strings.Contains(line, " -- "):
		var rest string
		head, rest, _ = strings.Cut(line, " -- ")
		head = strings.TrimSpace(head)
		typed = strings.TrimSpace(rest)
	}

	if head == "" {
		if typed == "" {
			return "", false, nil
		}
		return typed, true, nil
	}

	nums, numeric := selectionNumbers(head)
	if !numeric {
		return line, true, nil
	}
	if !multiple && len(nums) > 1 {
		return "", false, fmt.Errorf("pick one option")
	}

	picked := make([]int, 0, len(nums))
	seen := map[int]bool{}
	for _, n := range nums {
		if n < 1 || n > len(options) {
			return "", false, fmt.Errorf("option %d is out of range", n)
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		picked = append(picked, n-1)
	}
	text, err := tools.FormatAnswer(options, picked, typed)
	if err != nil {
		return "", false, err
	}
	return text, true, nil
}

func selectionNumbers(head string) ([]int, bool) {
	fields := strings.Fields(head)
	if len(fields) == 0 {
		return nil, false
	}
	nums := make([]int, 0, len(fields))
	for _, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return nil, false
		}
		nums = append(nums, n)
	}
	return nums, true
}
