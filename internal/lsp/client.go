package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

var errShutdown = errors.New("language server shut down")

var jsonNull = json.RawMessage("null")

type client struct {
	spec     serverSpec
	root     string
	cmd      *exec.Cmd
	conn     *jsonrpc2.Conn
	cancel   context.CancelFunc
	encoding string
	features featureSet
	folders  []workspaceFolder
	docs     map[string]*document
	ids      atomic.Uint64
	mu       sync.Mutex
	closed   bool
}

type stdio struct {
	reader io.ReadCloser
	writer io.WriteCloser
	once   sync.Once
	err    error
}

func (s *stdio) Read(p []byte) (int, error) { return s.reader.Read(p) }

func (s *stdio) Write(p []byte) (int, error) { return s.writer.Write(p) }

func (s *stdio) Close() error {
	s.once.Do(func() {
		err1 := s.writer.Close()
		err2 := s.reader.Close()
		if err1 != nil {
			s.err = err1
			return
		}
		s.err = err2
	})
	return s.err
}

func startClient(ctx context.Context, root string, spec serverSpec) (*client, error) {
	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = root
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", spec.Command, err)
	}

	connCtx, cancel := context.WithCancel(context.Background())
	folders := []workspaceFolder{{
		URI:  pathToURI(root),
		Name: filepath.Base(root),
	}}
	c := &client{
		spec:     spec,
		root:     root,
		cmd:      cmd,
		cancel:   cancel,
		encoding: "utf-16",
		folders:  folders,
		docs:     map[string]*document{},
	}
	go logStderr(spec.Command, stderr)
	c.conn = jsonrpc2.NewConn(
		connCtx,
		jsonrpc2.NewBufferedStream(&stdio{reader: stdout, writer: stdin}, jsonrpc2.VSCodeObjectCodec{}),
		jsonrpc2.AsyncHandler(c),
	)
	if err := c.initialize(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func logStderr(command string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024), 64*1024)
	for scanner.Scan() {
		slog.Debug("language server", "command", command, "stderr", scanner.Text())
	}
}

func (c *client) initialize(ctx context.Context) error {
	params := initializeParams{
		ProcessID:             os.Getpid(),
		RootURI:               pathToURI(c.root),
		Capabilities:          readOnlyCapabilities(),
		WorkspaceFolders:      c.folders,
		InitializationOptions: c.spec.Init,
	}
	var result initializeResult
	if err := c.call(ctx, "initialize", params, &result); err != nil {
		return fmt.Errorf("initialize %s: %w", filepath.Base(c.spec.Command), err)
	}
	if result.Capabilities.PositionEncoding != "" {
		c.encoding = result.Capabilities.PositionEncoding
	}
	c.features = featuresOf(result.Capabilities)
	if err := c.conn.Notify(ctx, "initialized", struct{}{}); err != nil {
		return fmt.Errorf("initialized %s: %w", filepath.Base(c.spec.Command), err)
	}
	return nil
}

type cancelParams struct {
	ID uint64 `json:"id"`
}

func (c *client) call(ctx context.Context, method string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	closed := c.closed
	conn := c.conn
	c.mu.Unlock()
	if closed || conn == nil {
		return errShutdown
	}

	id := c.ids.Add(1)
	waiter, err := conn.DispatchCall(ctx, method, params, jsonrpc2.PickID(jsonrpc2.ID{Num: id}))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s: %w", method, err)
	}
	err = waiter.Wait(ctx, result)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		stop, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = conn.Notify(stop, "$/cancelRequest", cancelParams{ID: id})
		cancel()
		return ctx.Err()
	}
	return fmt.Errorf("%s: %w", method, err)
}

func (c *client) Handle(ctx context.Context, conn *jsonrpc2.Conn, req *jsonrpc2.Request) {
	if req.Notif {
		return
	}
	var err error
	switch req.Method {
	case "workspace/configuration":
		err = conn.Reply(ctx, req.ID, configNulls(req.Params))
	case "workspace/workspaceFolders":
		err = conn.Reply(ctx, req.ID, c.folders)
	case "client/registerCapability", "client/unregisterCapability",
		"window/workDoneProgress/create", "window/showMessageRequest":
		err = conn.Reply(ctx, req.ID, jsonNull)
	default:
		err = conn.ReplyWithError(ctx, req.ID, &jsonrpc2.Error{
			Code:    jsonrpc2.CodeMethodNotFound,
			Message: "method not found",
		})
	}
	if err != nil {
		slog.Debug("language server reply", "method", req.Method, "err", err)
	}
}

func (c *client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	conn := c.conn
	cmd := c.cmd
	cancel := c.cancel
	c.mu.Unlock()

	if conn != nil {
		ctx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		id := c.ids.Add(1)
		waiter, err := conn.DispatchCall(ctx, "shutdown", nil, jsonrpc2.PickID(jsonrpc2.ID{Num: id}))
		if err == nil {
			_ = waiter.Wait(ctx, nil)
		}
		_ = conn.Notify(ctx, "exit", nil)
		stop()
		_ = conn.Close()
	}
	if cancel != nil {
		cancel()
	}
	if cmd != nil && cmd.Process != nil {
		done := make(chan struct{})
		go func() {
			_ = cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}
	return nil
}

func (c *client) definitions(ctx context.Context, abs string, line, column int) ([]string, error) {
	if !c.features.definition {
		return nil, errors.New("language server has no definition provider")
	}
	return c.located(ctx, "textDocument/definition", abs, line, column, false)
}

func (c *client) references(ctx context.Context, abs string, line, column int) ([]string, error) {
	if !c.features.references {
		return nil, errors.New("language server has no references provider")
	}
	return c.located(ctx, "textDocument/references", abs, line, column, true)
}

func (c *client) located(ctx context.Context, method, abs string, line, column int, refs bool) ([]string, error) {
	text, uri, err := c.ensure(ctx, abs)
	if err != nil {
		return nil, err
	}
	pos, err := lspPosition(text, line, column, c.encoding)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if refs {
		params := referenceParams{TextDocument: docID{URI: uri}, Position: pos}
		params.Context.IncludeDeclaration = true
		err = c.call(ctx, method, params, &raw)
	} else {
		err = c.call(ctx, method, textDocumentPosition{TextDocument: docID{URI: uri}, Position: pos}, &raw)
	}
	if err != nil {
		return nil, err
	}
	locs, err := parseLocations(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	return c.locationLines(locs), nil
}

func (c *client) hover(ctx context.Context, abs string, line, column int) (string, error) {
	if !c.features.hover {
		return "", errors.New("language server has no hover provider")
	}
	text, uri, err := c.ensure(ctx, abs)
	if err != nil {
		return "", err
	}
	pos, err := lspPosition(text, line, column, c.encoding)
	if err != nil {
		return "", err
	}
	var raw json.RawMessage
	err = c.call(ctx, "textDocument/hover", textDocumentPosition{
		TextDocument: docID{URI: uri},
		Position:     pos,
	}, &raw)
	if err != nil {
		return "", err
	}
	body, err := parseHover(raw)
	if err != nil {
		return "", fmt.Errorf("textDocument/hover: %w", err)
	}
	return trimHover(body), nil
}

func (c *client) documentSymbols(ctx context.Context, abs string) ([]string, error) {
	if !c.features.documentSymbol {
		return nil, errors.New("language server has no document symbol provider")
	}
	_, uri, err := c.ensure(ctx, abs)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	err = c.call(ctx, "textDocument/documentSymbol", documentParams{TextDocument: docID{URI: uri}}, &raw)
	if err != nil {
		return nil, err
	}
	hits, err := parseSymbols(raw)
	if err != nil {
		return nil, fmt.Errorf("textDocument/documentSymbol: %w", err)
	}
	return c.symbolLines(hits, uri), nil
}

func (c *client) workspaceSymbols(ctx context.Context, query string) ([]string, error) {
	if !c.features.workspaceSymbol {
		return nil, errors.New("language server has no workspace symbol provider")
	}
	var raw json.RawMessage
	err := c.call(ctx, "workspace/symbol", workspaceSymbolParams{Query: query}, &raw)
	if err != nil {
		return nil, err
	}
	hits, err := parseSymbols(raw)
	if err != nil {
		return nil, fmt.Errorf("workspace/symbol: %w", err)
	}
	return c.symbolLines(hits, ""), nil
}

func (c *client) fileDiagnostics(ctx context.Context, abs string) ([]string, error) {
	if !c.features.diagnostic {
		return nil, errors.New("language server has no pull diagnostics")
	}
	_, uri, err := c.ensure(ctx, abs)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	err = c.call(ctx, "textDocument/diagnostic", documentParams{TextDocument: docID{URI: uri}}, &raw)
	if err != nil {
		return nil, err
	}
	kind, items, err := parseFileDiagnostics(raw)
	if err != nil {
		return nil, fmt.Errorf("textDocument/diagnostic: %w", err)
	}
	if kind == "unchanged" {
		return nil, errors.New("diagnostics unchanged")
	}
	return c.diagnosticLines(uri, items), nil
}

func (c *client) workspaceDiagnostics(ctx context.Context) ([]string, error) {
	if !c.features.workspaceDiagnostic {
		return nil, errors.New("language server has no pull diagnostics")
	}
	var raw json.RawMessage
	err := c.call(ctx, "workspace/diagnostic", map[string]any{}, &raw)
	if err != nil {
		return nil, err
	}
	items, err := parseWorkspaceDiagnostics(raw)
	if err != nil {
		return nil, fmt.Errorf("workspace/diagnostic: %w", err)
	}
	lines := []string{}
	for _, item := range items {
		if item.Kind == "unchanged" {
			continue
		}
		lines = append(lines, c.diagnosticLines(item.URI, item.Items)...)
	}
	return lines, nil
}

func (c *client) locationLines(locs []location) []string {
	lines := make([]string, 0, len(locs))
	for _, loc := range locs {
		lines = append(lines, c.display(loc))
	}
	return lines
}

func (c *client) symbolLines(hits []symbolHit, fallbackURI string) []string {
	lines := make([]string, 0, len(hits))
	for _, hit := range hits {
		loc := hit.Loc
		if loc.URI == "" {
			loc.URI = fallbackURI
		}
		lines = append(lines, symbolKind(hit.Kind)+" "+hit.Name+" "+c.display(loc))
	}
	return lines
}

func (c *client) diagnosticLines(uri string, items []diagnostic) []string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		line := c.display(location{URI: uri, Range: item.Range}) + " " + severityText(item.Severity)
		if msg := oneLine(item.Message); msg != "" {
			line += " " + msg
		}
		lines = append(lines, line)
	}
	return lines
}
