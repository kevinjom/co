package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const requestTimeout = 2 * time.Minute

// Hub talks to the configured servers for every session.
// A server is contacted on the first search or call that needs it.
type Hub struct {
	servers []Server
	client  *mcp.Client
	mu      sync.Mutex
	rows    map[string]*row
	closed  bool
	now     func() time.Time
}

type row struct {
	hub      *Hub
	spec     Server
	mu       sync.Mutex
	session  *mcp.ClientSession
	tools    []toolEntry
	expires  time.Time
	have     bool
	closed   bool
	rejected error
	// fetch replaces the remote list in tests.
	fetch func(context.Context) ([]toolEntry, int, error)
}

// New returns a hub for servers.
// No server is contacted until a search or a call.
func New(servers []Server) *Hub {
	copied := make([]Server, len(servers))
	copy(copied, servers)
	h := &Hub{
		servers: copied,
		client: mcp.NewClient(&mcp.Implementation{Name: "co", Version: "dev"}, &mcp.ClientOptions{
			Capabilities:   &mcp.ClientCapabilities{},
			MultiRoundTrip: &mcp.MultiRoundTripOptions{Disabled: true},
		}),
		rows: map[string]*row{},
	}
	for _, server := range copied {
		headers := map[string]string{}
		for key, value := range server.Headers {
			headers[key] = value
		}
		server.Headers = headers
		h.rows[server.Name] = &row{hub: h, spec: server}
	}
	return h
}

// Close closes every session this hub opened.
func (h *Hub) Close() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	h.closed = true
	rows := make([]*row, 0, len(h.rows))
	for _, row := range h.rows {
		rows = append(rows, row)
	}
	h.mu.Unlock()

	var joined error
	for _, row := range rows {
		if err := row.close(); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

// Search matches query against each server's catalog.
// An empty query is an error. A server that cannot be listed is named in the result.
func (h *Hub) Search(ctx context.Context, query string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("query is empty")
	}
	if err := h.alive(); err != nil {
		return "", err
	}
	hits := []toolHit{}
	unavailable := []string{}
	for _, server := range h.servers {
		tools, err := h.rows[server.Name].catalog(ctx)
		if err != nil {
			unavailable = append(unavailable, server.Name+": "+err.Error())
			continue
		}
		for _, tool := range tools {
			if matchTool(tool, query) {
				hits = append(hits, toolHit{server: server.Name, tool: tool})
			}
		}
	}
	return formatSearch(hits, unavailable), nil
}

// Call runs one tool on a configured server.
func (h *Hub) Call(ctx context.Context, server, tool string, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := h.alive(); err != nil {
		return "", err
	}
	row := h.rows[server]
	if row == nil {
		return "", fmt.Errorf("unknown server %q", server)
	}
	if strings.TrimSpace(tool) == "" {
		return "", fmt.Errorf("tool is required")
	}
	return row.call(ctx, tool, args)
}

func (h *Hub) alive() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("mcp hub closed")
	}
	return nil
}

func (r *row) catalog(ctx context.Context) ([]toolEntry, error) {
	ctx, cancel := bound(ctx)
	defer cancel()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, fmt.Errorf("mcp hub closed")
	}
	if r.rejected != nil {
		return nil, r.rejected
	}
	if r.have && r.hub.clock().Before(r.expires) {
		return slices.Clone(r.tools), nil
	}
	tools, ttl, err := r.fetchLocked(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(tools, func(a, b toolEntry) int {
		return strings.Compare(a.name, b.name)
	})
	r.tools = tools
	if ttl <= 0 {
		r.have = false
		r.expires = time.Time{}
	} else {
		r.have = true
		r.expires = r.hub.clock().Add(time.Duration(ttl) * time.Millisecond)
	}
	return slices.Clone(tools), nil
}

func (r *row) fetchLocked(ctx context.Context) ([]toolEntry, int, error) {
	if r.fetch != nil {
		return r.fetch(ctx)
	}
	return r.listLocked(ctx)
}

func (r *row) listLocked(ctx context.Context) ([]toolEntry, int, error) {
	session, err := r.sessionLocked(ctx)
	if err != nil {
		return nil, 0, err
	}
	cursor := ""
	seen := map[string]bool{"": true}
	tools := []toolEntry{}
	ttl := -1
	for {
		result, err := session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			r.dropSession(err)
			return nil, 0, err
		}
		if result.TTLMs <= 0 {
			ttl = 0
		} else if ttl != 0 && (ttl < 0 || result.TTLMs < ttl) {
			ttl = result.TTLMs
		}
		for _, tool := range result.Tools {
			if tool == nil || tool.Name == "" {
				continue
			}
			schema, err := schemaJSON(tool.InputSchema)
			if err != nil {
				return nil, 0, fmt.Errorf("tool %s: %w", tool.Name, err)
			}
			tools = append(tools, toolEntry{
				name:        tool.Name,
				description: tool.Description,
				schema:      schema,
			})
		}
		if result.NextCursor == "" {
			break
		}
		if seen[result.NextCursor] {
			return nil, 0, fmt.Errorf("tools/list repeated cursor")
		}
		seen[result.NextCursor] = true
		cursor = result.NextCursor
	}
	if ttl < 0 {
		ttl = 0
	}
	return tools, ttl, nil
}

func (r *row) call(ctx context.Context, tool string, args map[string]any) (string, error) {
	ctx, cancel := bound(ctx)
	defer cancel()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "", fmt.Errorf("mcp hub closed")
	}
	if r.rejected != nil {
		return "", r.rejected
	}
	session, err := r.sessionLocked(ctx)
	if err != nil {
		return "", err
	}
	if args == nil {
		args = map[string]any{}
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      tool,
		Arguments: args,
	})
	if err != nil {
		r.dropSession(err)
		return "", err
	}
	return formatCall(result)
}

func (r *row) sessionLocked(ctx context.Context) (*mcp.ClientSession, error) {
	if r.closed {
		return nil, fmt.Errorf("mcp hub closed")
	}
	if r.session != nil {
		return r.session, nil
	}
	session, err := r.hub.client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             r.spec.URL,
		HTTPClient:           httpClient(r.spec.Headers),
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}, &mcp.ClientSessionOptions{ProtocolVersion: ProtocolVersion})
	if err != nil {
		return nil, err
	}
	got := ""
	if init := session.InitializeResult(); init != nil {
		got = init.ProtocolVersion
	}
	if got != ProtocolVersion {
		_ = session.Close()
		if got == "" {
			got = "unknown"
		}
		r.rejected = fmt.Errorf("protocol %s", got)
		return nil, r.rejected
	}
	r.session = session
	return session, nil
}

func (r *row) dropSession(err error) {
	if !errors.Is(err, mcp.ErrConnectionClosed) {
		return
	}
	if r.session != nil {
		_ = r.session.Close()
		r.session = nil
	}
}

func (r *row) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.session == nil {
		return nil
	}
	err := r.session.Close()
	r.session = nil
	return err
}

func (h *Hub) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

func bound(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, requestTimeout)
}

func schemaJSON(schema any) (string, error) {
	if schema == nil {
		return "{}", nil
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (h headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if len(h.headers) > 0 {
		req = req.Clone(req.Context())
		for key, value := range h.headers {
			req.Header.Set(key, value)
		}
	}
	base := h.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func httpClient(headers map[string]string) *http.Client {
	return &http.Client{
		Timeout:   requestTimeout,
		Transport: headerTransport{base: http.DefaultTransport, headers: headers},
	}
}
