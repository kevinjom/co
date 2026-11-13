package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSearchCapsAndOrders(t *testing.T) {
	hub := New([]Server{
		{Name: "beta", URL: "https://beta.example/mcp"},
		{Name: "alpha", URL: "https://alpha.example/mcp"},
	})
	hub.rows["beta"].fetch = func(context.Context) ([]toolEntry, int, error) {
		tools := make([]toolEntry, 9)
		for i := range tools {
			tools[i] = toolEntry{
				name:        fmt.Sprintf("z%d", len(tools)-1-i),
				description: "issue tool",
				schema:      `{"type":"object"}`,
			}
		}
		return tools, 1000, nil
	}
	hub.rows["alpha"].fetch = func(context.Context) ([]toolEntry, int, error) {
		return []toolEntry{
			{name: "create_issue", description: "Create an issue", schema: `{"type":"object","properties":{"title":{"type":"string"}}}`},
			{name: "noop", description: "Does nothing", schema: "{}"},
		}, 1000, nil
	}

	got, err := hub.Search(t.Context(), "issue")
	if err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	want.WriteString("hits: 8\n")
	for i := range 8 {
		fmt.Fprintf(&want, "server: beta\ntool: z%d\ndescription: issue tool\ninput_schema: {\"type\":\"object\"}\n", i)
	}
	if got != strings.TrimRight(want.String(), "\n") {
		t.Fatalf("got\n%s", got)
	}
	if strings.Contains(got, "tool: z8") || strings.Contains(got, "create_issue") {
		t.Fatalf("cap leaked\n%s", got)
	}

	miss, err := hub.Search(t.Context(), "create missing")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(miss, "hits: 0") || strings.Contains(miss, "create_issue") {
		t.Fatalf("miss %s", miss)
	}
}

func TestSearchNamesUnavailable(t *testing.T) {
	hub := New([]Server{{Name: "github", URL: "https://github.example/mcp"}})
	hub.rows["github"].fetch = func(context.Context) ([]toolEntry, int, error) {
		return nil, 0, errors.New("connect refused")
	}
	got, err := hub.Search(t.Context(), "issue")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hits: 0\nunavailable:\ngithub: connect refused" {
		t.Fatalf("got %q", got)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	hub := New([]Server{{Name: "github", URL: "https://github.example/mcp"}})
	calls := 0
	hub.rows["github"].fetch = func(context.Context) ([]toolEntry, int, error) {
		calls++
		return nil, 0, nil
	}
	if _, err := hub.Search(t.Context(), "  "); err == nil || !strings.Contains(err.Error(), "query is empty") {
		t.Fatalf("err %v", err)
	}
	if calls != 0 {
		t.Fatalf("calls %d", calls)
	}
}

func TestCatalogCache(t *testing.T) {
	now := time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC)
	hub := New([]Server{{Name: "github", URL: "https://github.example/mcp"}})
	hub.now = func() time.Time { return now }
	calls := 0
	hub.rows["github"].fetch = func(context.Context) ([]toolEntry, int, error) {
		calls++
		return []toolEntry{{name: "create_issue", description: "Create an issue", schema: "{}"}}, 1000, nil
	}
	if _, err := hub.Search(t.Context(), "issue"); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Search(t.Context(), "issue"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls %d", calls)
	}
	now = now.Add(time.Second)
	if _, err := hub.Search(t.Context(), "issue"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls after expiry %d", calls)
	}
}

func TestCatalogZeroTTLRefetches(t *testing.T) {
	hub := New([]Server{{Name: "github", URL: "https://github.example/mcp"}})
	calls := 0
	hub.rows["github"].fetch = func(context.Context) ([]toolEntry, int, error) {
		calls++
		return []toolEntry{{name: "create_issue", description: "issue", schema: "{}"}}, 0, nil
	}
	if _, err := hub.Search(t.Context(), "issue"); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.Search(t.Context(), "issue"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls %d", calls)
	}
}

func TestFormatCall(t *testing.T) {
	var result mcp.CallToolResult
	raw := `{
		"resultType": "complete",
		"content": [
			{"type": "text", "text": "made"},
			{"type": "image", "mimeType": "image/png", "data": "aaaa"},
			{"type": "image", "mimeType": "image/png", "data": "bbbb"}
		],
		"structuredContent": {"id": "1"}
	}`
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	got, err := formatCall(&result)
	if err != nil {
		t.Fatal(err)
	}
	want := "made\n{\"id\":\"1\"}\nomitted: 2 images"
	if got != want {
		t.Fatalf("got %q", got)
	}

	var needs mcp.CallToolResult
	if err := json.Unmarshal([]byte(`{"resultType":"input_required","content":[{"type":"text","text":"confirm"}]}`), &needs); err != nil {
		t.Fatal(err)
	}
	if _, err := formatCall(&needs); err == nil || err.Error() != "input required" {
		t.Fatalf("err %v", err)
	}
}

func TestClipKeepsTail(t *testing.T) {
	body := strings.Repeat("a", maxResultBytes-2) + "Ωtail"
	got := clip(body)
	if !strings.HasPrefix(got, "truncated: true\n") {
		t.Fatalf("prefix %q", got[:32])
	}
	if !strings.HasSuffix(got, "Ωtail") {
		t.Fatalf("suffix %q", got[len(got)-8:])
	}
	if strings.Contains(got, "\uFFFD") {
		t.Fatal("replacement rune")
	}
}
