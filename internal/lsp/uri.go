package lsp

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

func pathToURI(path string) string {
	path = filepath.Clean(path)
	slash := filepath.ToSlash(path)
	if vol := filepath.VolumeName(path); vol != "" {
		slash = "/" + slash
	} else if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}

func uriToPath(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("uri %q: %w", raw, err)
	}
	if parsed.Scheme != "" && parsed.Scheme != "file" {
		return "", fmt.Errorf("unsupported uri %q", raw)
	}
	path := filepath.FromSlash(parsed.Path)
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return path, nil
}
