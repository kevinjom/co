package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if i := stubArg(os.Args); i >= 0 {
		stubMain(os.Args[i+1:])
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func stubArg(args []string) int {
	for i, arg := range args {
		if arg == "lsp-stub" {
			return i
		}
	}
	return -1
}

func stubMain(args []string) {
	bare := false
	for _, arg := range args {
		if arg == "bare" {
			bare = true
		}
	}
	s := &stub{
		bare:    bare,
		docs:    map[string]string{},
		cancels: map[string]context.CancelFunc{},
		waits:   map[string]chan struct{}{},
	}
	in := bufio.NewReader(os.Stdin)
	for {
		payload, err := readFrame(in)
		if err != nil {
			return
		}
		var msg stubMsg
		if err := json.Unmarshal(payload, &msg); err != nil {
			continue
		}
		if msg.Method == "" {
			s.deliver(msg.ID)
			continue
		}
		switch msg.Method {
		case "initialized", "textDocument/didOpen", "textDocument/didChange", "$/cancelRequest", "shutdown", "exit":
			if s.handle(msg) {
				return
			}
		default:
			go s.handle(msg)
		}
	}
}

type stubMsg struct {
	Method string          `json:"method"`
	ID     json.RawMessage `json:"id"`
	Params json.RawMessage `json:"params"`
}

type stub struct {
	bare    bool
	mu      sync.Mutex
	docs    map[string]string
	cancels map[string]context.CancelFunc
	waits   map[string]chan struct{}
	outMu   sync.Mutex
}

func (s *stub) handle(msg stubMsg) bool {
	switch msg.Method {
	case "initialize":
		s.initialize(msg.ID)
	case "initialized":
	case "shutdown":
		s.reply(msg.ID, nil)
	case "exit":
		return true
	case "textDocument/didOpen":
		s.didOpen(msg.Params)
	case "textDocument/didChange":
		s.didChange(msg.Params)
	case "$/cancelRequest":
		s.cancel(msg.Params)
	case "textDocument/definition":
		s.definition(msg.ID, msg.Params)
	case "textDocument/references":
		s.references(msg.ID, msg.Params)
	case "textDocument/hover":
		s.hover(msg.ID, msg.Params)
	case "textDocument/documentSymbol":
		s.documentSymbol(msg.ID, msg.Params)
	case "workspace/symbol":
		s.workspaceSymbol(msg.ID)
	case "textDocument/diagnostic":
		s.fileDiagnostic(msg.ID)
	case "workspace/diagnostic":
		s.workspaceDiagnostic(msg.ID)
	default:
		if len(msg.ID) > 0 {
			s.fail(msg.ID, -32601, "method not found")
		}
	}
	return false
}

func (s *stub) initialize(id json.RawMessage) {
	const confID = "9001"
	if !s.waitFor(confID, 3*time.Second, func() {
		s.request(json.RawMessage(confID), "workspace/configuration", map[string]any{
			"items": []any{map[string]any{"section": "gopls"}},
		})
	}) {
		s.fail(id, -32603, "configuration timed out")
		return
	}
	caps := map[string]any{
		"positionEncoding":        "utf-8",
		"definitionProvider":      true,
		"hoverProvider":           true,
		"referencesProvider":      true,
		"documentSymbolProvider":  true,
		"workspaceSymbolProvider": true,
	}
	if !s.bare {
		caps["diagnosticProvider"] = map[string]any{
			"interFileDependencies": false,
			"workspaceDiagnostics":  true,
		}
	}
	s.reply(id, map[string]any{"capabilities": caps})
}

func (s *stub) waitFor(id string, d time.Duration, send func()) bool {
	ch := make(chan struct{})
	s.mu.Lock()
	s.waits[id] = ch
	s.mu.Unlock()
	send()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ch:
		return true
	case <-timer.C:
		return false
	}
}

func (s *stub) deliver(id json.RawMessage) {
	key := string(bytes.TrimSpace(id))
	s.mu.Lock()
	ch := s.waits[key]
	delete(s.waits, key)
	s.mu.Unlock()
	if ch != nil {
		close(ch)
	}
}

func (s *stub) didOpen(params json.RawMessage) {
	var body struct {
		TextDocument struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(params, &body); err != nil {
		return
	}
	s.mu.Lock()
	s.docs[body.TextDocument.URI] = body.TextDocument.Text
	s.mu.Unlock()
}

func (s *stub) didChange(params json.RawMessage) {
	var body struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		ContentChanges []struct {
			Text string `json:"text"`
		} `json:"contentChanges"`
	}
	if err := json.Unmarshal(params, &body); err != nil || len(body.ContentChanges) == 0 {
		return
	}
	s.mu.Lock()
	s.docs[body.TextDocument.URI] = body.ContentChanges[len(body.ContentChanges)-1].Text
	s.mu.Unlock()
}

func (s *stub) text(uri string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	text, ok := s.docs[uri]
	return text, ok
}

func (s *stub) definition(id, params json.RawMessage) {
	uri, ok := requestURI(params)
	if !ok {
		s.fail(id, -32602, "missing uri")
		return
	}
	text, open := s.text(uri)
	if !open {
		s.fail(id, -32602, "file is not open")
		return
	}
	locURI := uri
	line := 0
	char := 0
	if strings.Contains(text, "OUTSIDE") {
		locURI = "file:///outside.go"
		line = 3
		char = 1
	} else if strings.Contains(text, "beta") {
		char = 4
	}
	s.reply(id, stubLoc(locURI, line, char))
}

func (s *stub) references(id, params json.RawMessage) {
	uri, ok := requestURI(params)
	if !ok {
		s.fail(id, -32602, "missing uri")
		return
	}
	if _, open := s.text(uri); !open {
		s.fail(id, -32602, "file is not open")
		return
	}
	s.reply(id, []any{stubLoc(uri, 0, 0), stubLoc("file:///outside.go", 1, 0)})
}

func (s *stub) hover(id, params json.RawMessage) {
	uri, ok := requestURI(params)
	if !ok {
		s.fail(id, -32602, "missing uri")
		return
	}
	text, open := s.text(uri)
	if !open {
		s.fail(id, -32602, "file is not open")
		return
	}
	if strings.Contains(text, "BLOCK") {
		ctx, cancel := context.WithCancel(context.Background())
		s.mu.Lock()
		s.cancels[string(bytes.TrimSpace(id))] = cancel
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			s.fail(id, -32800, "cancelled")
		case <-time.After(5 * time.Second):
			s.reply(id, map[string]any{"contents": "func Example()"})
		}
		return
	}
	s.reply(id, map[string]any{"contents": "func Example()"})
}

func (s *stub) cancel(params json.RawMessage) {
	var body struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(params, &body); err != nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancels[string(bytes.TrimSpace(body.ID))]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *stub) documentSymbol(id, params json.RawMessage) {
	uri, ok := requestURI(params)
	if !ok {
		s.fail(id, -32602, "missing uri")
		return
	}
	if _, open := s.text(uri); !open {
		s.fail(id, -32602, "file is not open")
		return
	}
	s.reply(id, []any{
		map[string]any{
			"name":  "Example",
			"kind":  12,
			"range": stubRange(0, 0),
		},
	})
}

func (s *stub) workspaceSymbol(id json.RawMessage) {
	s.reply(id, []any{
		map[string]any{
			"name":     "Example",
			"kind":     12,
			"location": stubLoc("file:///outside.go", 0, 0),
		},
	})
}

func (s *stub) fileDiagnostic(id json.RawMessage) {
	s.reply(id, map[string]any{
		"kind": "full",
		"items": []any{
			map[string]any{
				"range":    stubRange(2, 0),
				"severity": 1,
				"message":  "undefined: x",
			},
		},
	})
}

func (s *stub) workspaceDiagnostic(id json.RawMessage) {
	s.reply(id, map[string]any{
		"items": []any{
			map[string]any{
				"uri":  "file:///outside.go",
				"kind": "full",
				"items": []any{
					map[string]any{
						"range":    stubRange(0, 0),
						"severity": 2,
						"message":  "unused",
					},
				},
			},
		},
	})
}

func requestURI(params json.RawMessage) (string, bool) {
	var body struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
	}
	if err := json.Unmarshal(params, &body); err != nil || body.TextDocument.URI == "" {
		return "", false
	}
	return body.TextDocument.URI, true
}

func stubLoc(uri string, line, char int) map[string]any {
	return map[string]any{
		"uri":   uri,
		"range": stubRange(line, char),
	}
}

func stubRange(line, char int) map[string]any {
	pos := map[string]any{"line": line, "character": char}
	return map[string]any{"start": pos, "end": pos}
}

func (s *stub) request(id json.RawMessage, method string, params any) {
	s.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
}

func (s *stub) reply(id json.RawMessage, result any) {
	s.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
}

func (s *stub) fail(id json.RawMessage, code int, message string) {
	s.write(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func (s *stub) write(v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		return
	}
	frame := append([]byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(payload))), payload...)
	s.outMu.Lock()
	_, _ = os.Stdout.Write(frame)
	s.outMu.Unlock()
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		rest, ok := strings.CutPrefix(line, "Content-Length: ")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil {
			return nil, err
		}
		length = n
	}
	if length < 0 {
		return nil, errors.New("missing Content-Length")
	}
	buf := make([]byte, length)
	_, err := io.ReadFull(r, buf)
	return buf, err
}
