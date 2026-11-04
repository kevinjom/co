package tools

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/dotcommander/defuddle"
)

const maxFetchBytes = 32 * 1024

func webFetchSchema() Schema {
	return Schema{
		Name:        "web_fetch",
		Description: "Fetch one http or https page and return its main content as Markdown.",
		Parameters: objectSchema(
			map[string]any{
				"url": stringProp("Absolute http or https URL to read."),
			},
			[]string{"url"},
		),
	}
}

func (s *Set) webFetch(ctx context.Context, args map[string]any) (string, error) {
	raw, ok, err := requiredString(args, "url")
	if err != nil {
		return "", err
	}
	raw = strings.TrimSpace(raw)
	if !ok || raw == "" {
		return "", fmt.Errorf("url is required")
	}

	page, err := defuddle.ParseFromURL(ctx, raw, &defuddle.Options{
		Markdown: true,
		Client:   webClient,
	})
	if err != nil {
		return "", err
	}

	body, truncated := clipText(strings.TrimSpace(page.Content), maxFetchBytes)
	return formatFetch(raw, strings.TrimSpace(page.Title), page.WordCount, body, truncated), nil
}

func formatFetch(pageURL, title string, words int, body string, truncated bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "url: %s\n", pageURL)
	if title != "" {
		fmt.Fprintf(&b, "title: %s\n", title)
	}
	fmt.Fprintf(&b, "words: %d\n", words)
	if truncated {
		b.WriteString("truncated: true\n")
	}
	if body != "" {
		b.WriteByte('\n')
		b.WriteString(body)
	}
	return strings.TrimRight(b.String(), "\n")
}

func clipText(text string, maxBytes int) (string, bool) {
	if len(text) <= maxBytes {
		return text, false
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	clipped := text[:cut]
	if i := strings.LastIndex(clipped, "\n"); i > 0 && i >= cut-1024 {
		clipped = clipped[:i]
	}
	return strings.TrimRight(clipped, "\n"), true
}
