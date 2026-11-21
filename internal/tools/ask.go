package tools

import (
	"context"
	"fmt"
	"strings"
)

// Question is one ask_user call.
type Question struct {
	Text     string
	Options  []string
	Multiple bool
}

// Asker collects the answer to a Question.
// It blocks until the user answers or ctx ends.
type Asker func(ctx context.Context, question Question) (string, error)

func init() {
	registerDetail("ask_user", askDetail)
}

func askDetail(args map[string]any) string {
	question, _ := args["question"].(string)
	return strings.Join(strings.Fields(question), " ")
}

func askUserSchema() Schema {
	return Schema{
		Name: "ask_user",
		Description: "Ask the user one clarifying question and wait for the answer. " +
			"Pass options when a short list would settle it. " +
			"Set multiple when more than one option can apply. " +
			"The user can pick options and can also type their own words. " +
			"Selected options come back one per line. Words they typed follow after a blank line.",
		Parameters: objectSchema(
			map[string]any{
				"question": stringProp("The question to ask."),
				"options": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Choices to show. Omit this when the user should answer in their own words. Keep the list to a handful.",
				},
				"multiple": map[string]any{
					"type":        "boolean",
					"description": "When true, the user can select more than one option.",
				},
			},
			[]string{"question"},
		),
	}
}

// SetAsker installs the function ask_user calls.
// A nil asker makes the tool report that a terminal is required.
func (s *Set) SetAsker(asker Asker) {
	if s == nil {
		return
	}
	s.asker = asker
}

func (s *Set) askUser(ctx context.Context, args map[string]any) (string, error) {
	question, ok, err := requiredString(args, "question")
	if err != nil {
		return "", err
	}
	question = strings.TrimSpace(question)
	if !ok || question == "" {
		return "", fmt.Errorf("question is required")
	}
	options, err := stringList(args, "options")
	if err != nil {
		return "", err
	}
	multiple, err := boolArg(args, "multiple")
	if err != nil {
		return "", err
	}
	if multiple && len(options) == 0 {
		return "", fmt.Errorf("options are required when multiple is set")
	}
	if s.asker == nil {
		return "", fmt.Errorf("ask_user needs a terminal")
	}
	copied := make([]string, len(options))
	copy(copied, options)
	answer, err := s.asker(ctx, Question{
		Text:     question,
		Options:  copied,
		Multiple: multiple,
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(answer) == "" {
		return "", fmt.Errorf("empty answer")
	}
	return answer, nil
}

// FormatAnswer is the tool result for a question.
// One selected option is that option's text. Several options are one per line.
// Typed words follow the options after a blank line. An empty reply is an error.
func FormatAnswer(options []string, picked []int, typed string) (string, error) {
	typed = strings.TrimSpace(typed)
	lines := make([]string, 0, len(picked))
	seen := map[int]bool{}
	for _, index := range picked {
		if index < 0 || index >= len(options) {
			return "", fmt.Errorf("option %d is out of range", index+1)
		}
		if seen[index] {
			continue
		}
		seen[index] = true
		lines = append(lines, options[index])
	}
	if len(lines) == 0 && typed == "" {
		return "", fmt.Errorf("empty answer")
	}
	if typed == "" {
		return strings.Join(lines, "\n"), nil
	}
	if len(lines) == 0 {
		return typed, nil
	}
	return strings.Join(lines, "\n") + "\n\n" + typed, nil
}

func stringList(args map[string]any, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return []string{}, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected an array of strings", key)
	}
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s: expected an array of strings", key)
		}
		text = strings.TrimSpace(text)
		if text == "" || strings.ContainsAny(text, "\r\n") {
			return nil, fmt.Errorf("%s: each option must be one line", key)
		}
		if seen[text] {
			return nil, fmt.Errorf("%s: %q is repeated", key, text)
		}
		seen[text] = true
		out = append(out, text)
	}
	return out, nil
}

func boolArg(args map[string]any, key string) (bool, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return false, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, fmt.Errorf("%s: expected a boolean", key)
	}
	return value, nil
}
