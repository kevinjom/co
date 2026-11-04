package lsp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPositionEncodingRoundTrip(t *testing.T) {
	text := "é\n"
	utf8Pos, err := lspPosition(text, 1, 2, "utf-8")
	if err != nil {
		t.Fatal(err)
	}
	if utf8Pos.Character != 2 {
		t.Fatalf("utf-8 character %d", utf8Pos.Character)
	}
	if got := runeColumn("é", utf8Pos.Character, "utf-8"); got != 2 {
		t.Fatalf("utf-8 column %d", got)
	}

	utf16Pos, err := lspPosition(text, 1, 2, "utf-16")
	if err != nil {
		t.Fatal(err)
	}
	if utf16Pos.Character != 1 {
		t.Fatalf("utf-16 character %d", utf16Pos.Character)
	}
	if got := runeColumn("é", utf16Pos.Character, "utf-16"); got != 2 {
		t.Fatalf("utf-16 column %d", got)
	}
}

func TestURIRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a file.go")
	got, err := uriToPath(pathToURI(path))
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("got %q want %q", got, path)
	}
}

func TestFormatListCaps(t *testing.T) {
	lines := make([]string, 0, 51)
	for range 51 {
		lines = append(lines, "row")
	}
	got := formatList("locations", lines)
	if !strings.Contains(got, "locations: 50\n") || !strings.Contains(got, "truncated: true\n") {
		t.Fatalf("format %q", got)
	}
	if strings.Count(got, "\nrow") != 50 {
		t.Fatalf("rows %d", strings.Count(got, "\nrow"))
	}
}
