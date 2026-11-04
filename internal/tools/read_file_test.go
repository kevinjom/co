package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFileReportsLineCountAndTruncates(t *testing.T) {
	root := t.TempDir()
	small := filepath.Join(root, "small.txt")
	if err := os.WriteFile(small, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := readFileWindow(small, 1, defaultReadLines)
	if err != nil {
		t.Fatal(err)
	}
	want := "lines: 3\n1|a\n2|b\n3|c"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}

	var big strings.Builder
	const total = 400
	line := strings.Repeat("a", 100)
	for i := range total {
		if i > 0 {
			big.WriteByte('\n')
		}
		big.WriteString(line)
	}
	large := filepath.Join(root, "large.txt")
	if err := os.WriteFile(large, []byte(big.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err = readFileWindow(large, 1, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "lines: 400\n") {
		t.Fatalf("missing line count: %q", got)
	}
	if !strings.Contains(got, "truncated: true\n") {
		t.Fatalf("missing truncation marker: %q", got)
	}
}
