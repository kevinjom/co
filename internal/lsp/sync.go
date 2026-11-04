package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// document is the text the server has for one file.
type document struct {
	URI     string
	Version int
	Text    string
}

// ensure sends the file as it is on disk.
// The first call is didOpen. A later call sends one full didChange when the
// bytes differ. A future writer saves the file, and the next request uses this.
func (c *client) ensure(ctx context.Context, abs string) (string, string, error) {
	body, err := os.ReadFile(abs)
	if err != nil {
		return "", "", fmt.Errorf("opening %s: %w", filepath.Base(abs), err)
	}
	text := string(body)
	ext := strings.ToLower(filepath.Ext(abs))
	languageID, ok := c.spec.Files[ext]
	if !ok {
		return "", "", fmt.Errorf("no language server for %s", extLabel(abs))
	}
	uri := pathToURI(abs)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return "", "", errShutdown
	}

	doc, open := c.docs[abs]
	if !open {
		doc = &document{URI: uri, Version: 1, Text: text}
		c.docs[abs] = doc
		err = c.conn.Notify(ctx, "textDocument/didOpen", didOpenParams{
			TextDocument: textDocumentItem{
				URI:        uri,
				LanguageID: languageID,
				Version:    doc.Version,
				Text:       text,
			},
		})
		if err != nil {
			delete(c.docs, abs)
			return "", "", err
		}
		return text, uri, nil
	}
	if doc.Text == text {
		return text, uri, nil
	}

	previous := doc.Text
	previousVersion := doc.Version
	doc.Version++
	doc.Text = text
	err = c.conn.Notify(ctx, "textDocument/didChange", didChangeParams{
		TextDocument: versionedDoc{URI: uri, Version: doc.Version},
		ContentChanges: []textChange{
			{Text: text},
		},
	})
	if err != nil {
		doc.Version = previousVersion
		doc.Text = previous
		return "", "", err
	}
	return text, uri, nil
}
