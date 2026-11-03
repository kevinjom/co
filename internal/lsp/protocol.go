package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type rng struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

type location struct {
	URI   string `json:"uri"`
	Range rng    `json:"range"`
}

type locationLink struct {
	TargetURI   string `json:"targetUri"`
	TargetRange rng    `json:"targetRange"`
}

type docID struct {
	URI string `json:"uri"`
}

type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type versionedDoc struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type textChange struct {
	Text string `json:"text"`
}

// didChangeParams carries one full-document change.
// The range is omitted on purpose: a content change without a range replaces
// the whole file, so a later writer does not have to compute a diff.
type didChangeParams struct {
	TextDocument   versionedDoc `json:"textDocument"`
	ContentChanges []textChange `json:"contentChanges"`
}

type textDocumentPosition struct {
	TextDocument docID    `json:"textDocument"`
	Position     position `json:"position"`
}

type referenceParams struct {
	TextDocument docID    `json:"textDocument"`
	Position     position `json:"position"`
	Context      struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

type documentParams struct {
	TextDocument docID `json:"textDocument"`
}

type workspaceSymbolParams struct {
	Query string `json:"query"`
}

type workspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type initializeParams struct {
	ProcessID             int               `json:"processId"`
	RootURI               string            `json:"rootUri"`
	Capabilities          map[string]any    `json:"capabilities"`
	WorkspaceFolders      []workspaceFolder `json:"workspaceFolders"`
	InitializationOptions any               `json:"initializationOptions,omitempty"`
}

type initializeResult struct {
	Capabilities serverCapabilities `json:"capabilities"`
}

type serverCapabilities struct {
	PositionEncoding        string          `json:"positionEncoding"`
	HoverProvider           json.RawMessage `json:"hoverProvider"`
	DefinitionProvider      json.RawMessage `json:"definitionProvider"`
	ReferencesProvider      json.RawMessage `json:"referencesProvider"`
	DocumentSymbolProvider  json.RawMessage `json:"documentSymbolProvider"`
	WorkspaceSymbolProvider json.RawMessage `json:"workspaceSymbolProvider"`
	DiagnosticProvider      json.RawMessage `json:"diagnosticProvider"`
}

type featureSet struct {
	definition          bool
	references          bool
	hover               bool
	documentSymbol      bool
	workspaceSymbol     bool
	diagnostic          bool
	workspaceDiagnostic bool
}

func featuresOf(caps serverCapabilities) featureSet {
	return featureSet{
		definition:          enabled(caps.DefinitionProvider),
		references:          enabled(caps.ReferencesProvider),
		hover:               enabled(caps.HoverProvider),
		documentSymbol:      enabled(caps.DocumentSymbolProvider),
		workspaceSymbol:     enabled(caps.WorkspaceSymbolProvider),
		diagnostic:          enabled(caps.DiagnosticProvider),
		workspaceDiagnostic: workspaceDiagnostics(caps.DiagnosticProvider),
	}
}

func enabled(raw json.RawMessage) bool {
	text := bytes.TrimSpace(raw)
	if len(text) == 0 || bytes.Equal(text, []byte("null")) || bytes.Equal(text, []byte("false")) {
		return false
	}
	if bytes.Equal(text, []byte("true")) {
		return true
	}
	return text[0] == '{' || text[0] == '['
}

func workspaceDiagnostics(raw json.RawMessage) bool {
	if !enabled(raw) || bytes.TrimSpace(raw)[0] != '{' {
		return false
	}
	var body struct {
		WorkspaceDiagnostics bool `json:"workspaceDiagnostics"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return false
	}
	return body.WorkspaceDiagnostics
}

// readOnlyCapabilities is what co tells the server it can do.
// It does not advertise workspace.applyEdit. A write mode adds that and
// handles the server's apply request through the path jail.
func readOnlyCapabilities() map[string]any {
	return map[string]any{
		"general": map[string]any{
			"positionEncodings": []string{"utf-8", "utf-16"},
		},
		"textDocument": map[string]any{
			"synchronization": map[string]any{
				"dynamicRegistration": false,
				"didSave":             false,
			},
			"hover": map[string]any{
				"contentFormat": []string{"plaintext", "markdown"},
			},
			"definition": map[string]any{
				"linkSupport": true,
			},
			"references": map[string]any{},
			"documentSymbol": map[string]any{
				"hierarchicalDocumentSymbolSupport": true,
			},
			"diagnostic": map[string]any{},
		},
		"workspace": map[string]any{
			"symbol":      map[string]any{},
			"diagnostics": map[string]any{},
		},
	}
}

type symbolHit struct {
	Name string
	Kind int
	Loc  location
}

func parseLocations(raw json.RawMessage) ([]location, error) {
	text := bytes.TrimSpace(raw)
	if len(text) == 0 || bytes.Equal(text, []byte("null")) {
		return []location{}, nil
	}
	if text[0] == '[' {
		return parseLocationArray(text)
	}
	var link locationLink
	if err := json.Unmarshal(text, &link); err == nil && link.TargetURI != "" {
		return []location{{URI: link.TargetURI, Range: link.TargetRange}}, nil
	}
	var loc location
	if err := json.Unmarshal(text, &loc); err != nil {
		return nil, err
	}
	if loc.URI == "" {
		return []location{}, nil
	}
	return []location{loc}, nil
}

func parseLocationArray(text []byte) ([]location, error) {
	var links []locationLink
	if err := json.Unmarshal(text, &links); err != nil {
		return nil, err
	}
	if len(links) > 0 && links[0].TargetURI != "" {
		out := make([]location, 0, len(links))
		for _, link := range links {
			out = append(out, location{URI: link.TargetURI, Range: link.TargetRange})
		}
		return out, nil
	}
	var locs []location
	if err := json.Unmarshal(text, &locs); err != nil {
		return nil, err
	}
	if locs == nil {
		return []location{}, nil
	}
	return locs, nil
}

func parseHover(raw json.RawMessage) (string, error) {
	text := bytes.TrimSpace(raw)
	if len(text) == 0 || bytes.Equal(text, []byte("null")) {
		return "", nil
	}
	var body struct {
		Contents json.RawMessage `json:"contents"`
	}
	if err := json.Unmarshal(text, &body); err != nil {
		return "", err
	}
	return markup(body.Contents), nil
}

func markup(raw json.RawMessage) string {
	text := bytes.TrimSpace(raw)
	if len(text) == 0 || bytes.Equal(text, []byte("null")) {
		return ""
	}
	switch text[0] {
	case '"':
		var value string
		if err := json.Unmarshal(text, &value); err != nil {
			return ""
		}
		return value
	case '[':
		var parts []json.RawMessage
		if err := json.Unmarshal(text, &parts); err != nil {
			return ""
		}
		lines := []string{}
		for _, part := range parts {
			piece := markup(part)
			if piece == "" {
				continue
			}
			lines = append(lines, piece)
		}
		return strings.Join(lines, "\n")
	default:
		var obj struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(text, &obj); err != nil {
			return ""
		}
		return obj.Value
	}
}

func parseSymbols(raw json.RawMessage) ([]symbolHit, error) {
	text := bytes.TrimSpace(raw)
	if len(text) == 0 || bytes.Equal(text, []byte("null")) {
		return []symbolHit{}, nil
	}
	if text[0] != '[' {
		return nil, fmt.Errorf("symbol result is not a list")
	}
	var probe []map[string]json.RawMessage
	if err := json.Unmarshal(text, &probe); err != nil {
		return nil, err
	}
	if len(probe) == 0 {
		return []symbolHit{}, nil
	}
	if _, ok := probe[0]["range"]; ok {
		var docs []documentSymbol
		if err := json.Unmarshal(text, &docs); err != nil {
			return nil, err
		}
		out := []symbolHit{}
		flattenSymbols(docs, &out)
		return out, nil
	}
	var infos []symbolInformation
	if err := json.Unmarshal(text, &infos); err != nil {
		return nil, err
	}
	out := make([]symbolHit, 0, len(infos))
	for _, info := range infos {
		loc := info.Location
		if loc.URI == "" {
			loc.URI = info.URI
		}
		out = append(out, symbolHit{Name: info.Name, Kind: info.Kind, Loc: loc})
	}
	return out, nil
}

type documentSymbol struct {
	Name     string           `json:"name"`
	Kind     int              `json:"kind"`
	Range    rng              `json:"range"`
	Children []documentSymbol `json:"children"`
}

type symbolInformation struct {
	Name     string   `json:"name"`
	Kind     int      `json:"kind"`
	Location location `json:"location"`
	URI      string   `json:"uri"`
}

func flattenSymbols(docs []documentSymbol, out *[]symbolHit) {
	for _, doc := range docs {
		if len(*out) > maxResults {
			return
		}
		*out = append(*out, symbolHit{
			Name: doc.Name,
			Kind: doc.Kind,
			Loc:  location{Range: doc.Range},
		})
		flattenSymbols(doc.Children, out)
	}
}

type diagnostic struct {
	Range    rng    `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
}

func parseFileDiagnostics(raw json.RawMessage) (string, []diagnostic, error) {
	text := bytes.TrimSpace(raw)
	if len(text) == 0 || bytes.Equal(text, []byte("null")) {
		return "", []diagnostic{}, nil
	}
	var body struct {
		Kind  string       `json:"kind"`
		Items []diagnostic `json:"items"`
	}
	if err := json.Unmarshal(text, &body); err != nil {
		return "", nil, err
	}
	if body.Items == nil {
		return body.Kind, []diagnostic{}, nil
	}
	return body.Kind, body.Items, nil
}

type workspaceDiagnosticItem struct {
	URI   string       `json:"uri"`
	Kind  string       `json:"kind"`
	Items []diagnostic `json:"items"`
}

func parseWorkspaceDiagnostics(raw json.RawMessage) ([]workspaceDiagnosticItem, error) {
	text := bytes.TrimSpace(raw)
	if len(text) == 0 || bytes.Equal(text, []byte("null")) {
		return []workspaceDiagnosticItem{}, nil
	}
	var body struct {
		Items []workspaceDiagnosticItem `json:"items"`
	}
	if err := json.Unmarshal(text, &body); err != nil {
		return nil, err
	}
	if body.Items == nil {
		return []workspaceDiagnosticItem{}, nil
	}
	return body.Items, nil
}

func symbolKind(kind int) string {
	names := []string{
		"",
		"file",
		"module",
		"namespace",
		"package",
		"class",
		"method",
		"property",
		"field",
		"constructor",
		"enum",
		"interface",
		"function",
		"variable",
		"constant",
		"string",
		"number",
		"boolean",
		"array",
		"object",
		"key",
		"null",
		"enummember",
		"struct",
		"event",
		"operator",
		"typeparameter",
	}
	if kind > 0 && kind < len(names) {
		return names[kind]
	}
	return fmt.Sprintf("kind-%d", kind)
}

func severityText(n int) string {
	switch n {
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	default:
		return "error"
	}
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func configNulls(params *json.RawMessage) []any {
	n := 1
	if params != nil {
		var body struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(*params, &body); err == nil && len(body.Items) > 0 {
			n = len(body.Items)
		}
	}
	return make([]any, n)
}
