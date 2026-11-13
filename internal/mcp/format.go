package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// ProtocolVersion is the only revision this client speaks.
	ProtocolVersion = "2026-07-28"
	// maxResultBytes is the tail kept from a tool result, matching bash.
	maxResultBytes = 32 * 1024
	maxHits        = 8
)

type toolEntry struct {
	name        string
	description string
	schema      string
}

type toolHit struct {
	server string
	tool   toolEntry
}

func matchTool(tool toolEntry, query string) bool {
	words := strings.Fields(query)
	if len(words) == 0 {
		return false
	}
	hay := strings.ToLower(tool.name + "\n" + tool.description)
	for _, word := range words {
		if !strings.Contains(hay, strings.ToLower(word)) {
			return false
		}
	}
	return true
}

func formatSearch(hits []toolHit, unavailable []string) string {
	if len(hits) > maxHits {
		hits = hits[:maxHits]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "hits: %d\n", len(hits))
	for _, hit := range hits {
		fmt.Fprintf(&b, "server: %s\n", hit.server)
		fmt.Fprintf(&b, "tool: %s\n", hit.tool.name)
		fmt.Fprintf(&b, "description: %s\n", oneLine(hit.tool.description))
		fmt.Fprintf(&b, "input_schema: %s\n", hit.tool.schema)
	}
	if len(unavailable) > 0 {
		b.WriteString("unavailable:\n")
		for _, line := range unavailable {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func formatCall(result *mcp.CallToolResult) (string, error) {
	if result == nil {
		return "", fmt.Errorf("empty tool result")
	}
	if result.NeedsInput() {
		return "", fmt.Errorf("input required")
	}
	var texts []string
	counts := map[string]int{}
	for _, item := range result.Content {
		switch c := item.(type) {
		case *mcp.TextContent:
			texts = append(texts, c.Text)
		case *mcp.ImageContent:
			counts["image"]++
		case *mcp.AudioContent:
			counts["audio"]++
		case *mcp.EmbeddedResource:
			counts["resource"]++
		case *mcp.ResourceLink:
			counts["resource link"]++
		default:
			if item != nil {
				counts["other"]++
			}
		}
	}
	var b strings.Builder
	if len(texts) > 0 {
		b.WriteString(strings.Join(texts, "\n"))
	}
	if result.StructuredContent != nil {
		raw, err := json.Marshal(result.StructuredContent)
		if err != nil {
			return "", fmt.Errorf("structured content: %w", err)
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.Write(raw)
	}
	if line := omittedLine(counts); line != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	return clip(b.String()), nil
}

func omittedLine(counts map[string]int) string {
	order := []string{"image", "audio", "resource", "resource link", "other"}
	parts := make([]string, 0, len(order))
	for _, kind := range order {
		n := counts[kind]
		if n == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, plural(n, kind)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "omitted: " + strings.Join(parts, ", ")
}

func plural(n int, kind string) string {
	if n == 1 {
		return kind
	}
	switch kind {
	case "image":
		return "images"
	case "resource":
		return "resources"
	case "resource link":
		return "resource links"
	case "other":
		return "others"
	default:
		return kind
	}
}

func clip(body string) string {
	body = strings.TrimRight(body, "\n")
	if len(body) <= maxResultBytes {
		return body
	}
	tail := []byte(body)[len(body)-maxResultBytes:]
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	return "truncated: true\n" + string(tail)
}
