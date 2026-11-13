package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSearchRejectsOldProtocol(t *testing.T) {
	srv := startServer(t, serverOpts{
		versions: []string{"2025-11-25"},
	})
	hub := New([]Server{{Name: "old", URL: srv.URL}})
	t.Cleanup(func() { _ = hub.Close() })

	got, err := hub.Search(t.Context(), "issue")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "hits: 0") || !strings.Contains(got, "old: protocol 2025-11-25") {
		t.Fatalf("got %s", got)
	}
	if srv.lists.Load() != 0 {
		t.Fatalf("listed tools on %s", srv.versions.joined())
	}
}

func TestCallAndList(t *testing.T) {
	var sawAuth string
	var sawVersion string
	var sawName string
	srv := startServer(t, serverOpts{
		versions: []string{ProtocolVersion},
		ttl:      60_000,
		pageSize: 1,
		onRequest: func(r *http.Request) {
			if r.Header.Get("Mcp-Method") == "tools/call" {
				sawAuth = r.Header.Get("Authorization")
				sawVersion = r.Header.Get("Mcp-Protocol-Version")
				sawName = r.Header.Get("Mcp-Name")
			}
		},
		tools: []stubTool{
			{name: "create_issue", description: "Create an issue"},
			{name: "list_issues", description: "List issues"},
		},
	})
	hub := New([]Server{{
		Name: "github",
		URL:  srv.URL,
		Headers: map[string]string{
			"Authorization": "Bearer hardcoded-token",
		},
	}})
	t.Cleanup(func() { _ = hub.Close() })

	got, err := hub.Search(t.Context(), "issue")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "tool: create_issue") || !strings.Contains(got, "tool: list_issues") {
		t.Fatalf("search %s", got)
	}
	if srv.lists.Load() != 2 {
		t.Fatalf("list calls %d", srv.lists.Load())
	}
	if strings.Contains(srv.versions.joined(), "2025-11-25") {
		t.Fatalf("downgraded: %s", srv.versions.joined())
	}

	out, err := hub.Call(t.Context(), "github", "create_issue", map[string]any{"title": "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "made hi") {
		t.Fatalf("call %q", out)
	}
	if sawAuth != "Bearer hardcoded-token" || sawVersion != ProtocolVersion || sawName != "create_issue" {
		t.Fatalf("auth %q version %q name %q", sawAuth, sawVersion, sawName)
	}

	big, err := hub.Call(t.Context(), "github", "list_issues", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(big, "truncated: true\n") || !strings.HasSuffix(big, "TAIL") {
		t.Fatalf("truncated prefix/suffix missing: %d bytes", len(big))
	}
}

type serverOpts struct {
	versions  []string
	ttl       int
	pageSize  int
	tools     []stubTool
	onRequest func(*http.Request)
}

type stubTool struct {
	name        string
	description string
}

type recorded struct {
	URL      string
	lists    counted
	versions versionSet
}

type counted struct {
	mu sync.Mutex
	n  int
}

func (c *counted) Add(n int) {
	c.mu.Lock()
	c.n += n
	c.mu.Unlock()
}

func (c *counted) Load() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

type versionSet struct {
	mu   sync.Mutex
	seen map[string]bool
}

func (v *versionSet) add(version string) {
	if version == "" {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.seen == nil {
		v.seen = map[string]bool{}
	}
	v.seen[version] = true
}

func (v *versionSet) joined() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]string, 0, len(v.seen))
	for version := range v.seen {
		out = append(out, version)
	}
	return strings.Join(out, ",")
}

func startServer(t *testing.T, opt serverOpts) *recorded {
	t.Helper()
	impl := &mcp.Implementation{Name: "stub", Version: "v1"}
	server := mcp.NewServer(impl, &mcp.ServerOptions{
		SupportedProtocolVersions: opt.versions,
		PageSize:                  opt.pageSize,
		SetCacheable: func(_ context.Context, _ mcp.Request, c *mcp.Cacheable) {
			if opt.ttl > 0 {
				c.TTLMs = opt.ttl
			}
		},
	})
	for _, tool := range opt.tools {
		addStub(server, tool)
	}
	if len(opt.tools) == 0 {
		addStub(server, stubTool{name: "create_issue", description: "Create an issue"})
	}
	stateless := len(opt.versions) == 1 && opt.versions[0] == ProtocolVersion
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: stateless, JSONResponse: true})
	rec := &recorded{}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.versions.add(r.Header.Get("Mcp-Protocol-Version"))
		if r.Header.Get("Mcp-Method") == "tools/list" {
			rec.lists.Add(1)
		}
		if opt.onRequest != nil {
			opt.onRequest(r)
		}
		handler.ServeHTTP(w, r)
	}))
	rec.URL = httpServer.URL
	t.Cleanup(httpServer.Close)
	return rec
}

func addStub(server *mcp.Server, tool stubTool) {
	server.AddTool(&mcp.Tool{
		Name:        tool.name,
		Description: tool.description,
		InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if tool.name == "list_issues" {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("x", maxResultBytes) + "TAIL"}},
			}, nil
		}
		title := ""
		if req != nil && req.Params != nil && len(req.Params.Arguments) > 0 {
			var args map[string]any
			if err := json.Unmarshal(req.Params.Arguments, &args); err == nil {
				title, _ = args["title"].(string)
			}
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "made " + title}},
		}, nil
	})
}
