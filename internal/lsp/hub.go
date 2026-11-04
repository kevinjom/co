package lsp

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Hub routes language-server calls for one working directory.
// A row's process starts on the first call that needs it.
type Hub struct {
	root     string
	servers  []serverSpec
	mu       sync.Mutex
	clients  map[string]*client
	starting map[string]*startGate
	closed   bool
}

type startGate struct {
	done chan struct{}
	c    *client
	err  error
}

// New returns a hub for the working directory.
// The Go row is gopls. The process is not started until a tool call needs it.
func New(root string) *Hub {
	return newHub(root, builtinServers())
}

func newHub(root string, servers []serverSpec) *Hub {
	abs, err := filepath.Abs(root)
	if err == nil {
		root = abs
	}
	if real, err := filepath.EvalSymlinks(root); err == nil {
		root = real
	}
	copied := make([]serverSpec, len(servers))
	copy(copied, servers)
	return &Hub{
		root:     root,
		servers:  copied,
		clients:  map[string]*client{},
		starting: map[string]*startGate{},
	}
}

// Available reports whether at least one row's binary is on PATH.
func (h *Hub) Available() bool {
	for _, spec := range h.servers {
		if _, err := exec.LookPath(spec.Command); err == nil {
			return true
		}
	}
	return false
}

// Close shuts down every server this hub started.
func (h *Hub) Close() error {
	h.mu.Lock()
	h.closed = true
	clients := h.clients
	h.clients = map[string]*client{}
	h.mu.Unlock()

	var joined error
	for _, c := range clients {
		if err := c.Close(); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

// Definition returns locations for the symbol at line and column.
// line and column are 1-based. column counts Unicode code points.
func (h *Hub) Definition(ctx context.Context, path string, line, column int) (string, error) {
	c, err := h.clientFor(ctx, path)
	if err != nil {
		return "", err
	}
	lines, err := c.definitions(ctx, path, line, column)
	if err != nil {
		return "", err
	}
	return formatList("locations", lines), nil
}

// References returns references to the symbol at line and column, including the declaration.
func (h *Hub) References(ctx context.Context, path string, line, column int) (string, error) {
	c, err := h.clientFor(ctx, path)
	if err != nil {
		return "", err
	}
	lines, err := c.references(ctx, path, line, column)
	if err != nil {
		return "", err
	}
	return formatList("locations", lines), nil
}

// Hover returns the server's hover text for the symbol at line and column.
func (h *Hub) Hover(ctx context.Context, path string, line, column int) (string, error) {
	c, err := h.clientFor(ctx, path)
	if err != nil {
		return "", err
	}
	return c.hover(ctx, path, line, column)
}

// Symbols lists document symbols when path is set, or workspace symbols for query.
func (h *Hub) Symbols(ctx context.Context, path, query string) (string, error) {
	if path != "" {
		c, err := h.clientFor(ctx, path)
		if err != nil {
			return "", err
		}
		lines, err := c.documentSymbols(ctx, path)
		if err != nil {
			return "", err
		}
		return formatList("symbols", lines), nil
	}
	return h.merge(ctx, "symbols", func(c *client) ([]string, error) {
		return c.workspaceSymbols(ctx, query)
	})
}

// Diagnostics returns pull diagnostics for path, or the workspace when path is empty.
func (h *Hub) Diagnostics(ctx context.Context, path string) (string, error) {
	if path != "" {
		c, err := h.clientFor(ctx, path)
		if err != nil {
			return "", err
		}
		lines, err := c.fileDiagnostics(ctx, path)
		if err != nil {
			return "", err
		}
		return formatList("diagnostics", lines), nil
	}
	return h.merge(ctx, "diagnostics", func(c *client) ([]string, error) {
		return c.workspaceDiagnostics(ctx)
	})
}

func (h *Hub) clientFor(ctx context.Context, path string) (*client, error) {
	spec, err := h.specFor(path)
	if err != nil {
		return nil, err
	}
	return h.start(ctx, spec)
}

func (h *Hub) specFor(path string) (serverSpec, error) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, spec := range h.servers {
		if _, ok := spec.Files[ext]; !ok {
			continue
		}
		if _, err := exec.LookPath(spec.Command); err != nil {
			return serverSpec{}, fmt.Errorf("no language server for %s", extLabel(path))
		}
		return spec, nil
	}
	return serverSpec{}, fmt.Errorf("no language server for %s", extLabel(path))
}

func extLabel(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return filepath.Base(path)
	}
	return ext
}

func (h *Hub) ready() []serverSpec {
	out := []serverSpec{}
	for _, spec := range h.servers {
		if _, err := exec.LookPath(spec.Command); err != nil {
			continue
		}
		out = append(out, spec)
	}
	return out
}

func (h *Hub) merge(ctx context.Context, kind string, run func(*client) ([]string, error)) (string, error) {
	specs := h.ready()
	if len(specs) == 0 {
		return "", errors.New("no language server on PATH")
	}
	lines := []string{}
	var firstErr error
	for _, spec := range specs {
		c, err := h.start(ctx, spec)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		got, err := run(c)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		lines = append(lines, prefixLines(filepath.Base(spec.Command), got)...)
	}
	if len(lines) == 0 && firstErr != nil {
		return "", firstErr
	}
	return formatList(kind, lines), nil
}

func (h *Hub) start(ctx context.Context, spec serverSpec) (*client, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, errShutdown
	}
	if c, ok := h.clients[spec.Command]; ok {
		h.mu.Unlock()
		return c, nil
	}
	if gate, ok := h.starting[spec.Command]; ok {
		h.mu.Unlock()
		select {
		case <-gate.done:
			return gate.c, gate.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	gate := &startGate{done: make(chan struct{})}
	h.starting[spec.Command] = gate
	h.mu.Unlock()

	c, err := startClient(ctx, h.root, spec)

	h.mu.Lock()
	delete(h.starting, spec.Command)
	switch {
	case err != nil:
		gate.err = err
	case h.closed:
		gate.err = errShutdown
		go c.Close()
	default:
		h.clients[spec.Command] = c
		gate.c = c
	}
	close(gate.done)
	h.mu.Unlock()
	return gate.c, gate.err
}
