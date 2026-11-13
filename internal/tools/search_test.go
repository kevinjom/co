package tools

import (
	"strings"
	"testing"
)

func TestDetectSearch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		exists func(string) bool
		want   searcher
	}{
		{
			name:   "rg wins when both are installed",
			exists: func(name string) bool { return name == "rg" || name == "ag" },
			want:   searchRg,
		},
		{
			name:   "rg alone",
			exists: func(name string) bool { return name == "rg" },
			want:   searchRg,
		},
		{
			name:   "ag when rg is missing",
			exists: func(name string) bool { return name == "ag" },
			want:   searchAg,
		},
		{
			name:   "grep tool when neither is installed",
			exists: func(string) bool { return false },
			want:   searchGrep,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := detectSearch(tt.exists); got != tt.want {
				t.Fatalf("detectSearch() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommandOnPath(t *testing.T) {
	t.Parallel()

	if !commandOnPath("go") {
		t.Fatal("go is not on PATH")
	}
	if commandOnPath("co-not-a-real-binary") {
		t.Fatal("missing command reported as installed")
	}
}

func TestNewSetOmitsGrepWhenExternalSearchExists(t *testing.T) {
	t.Parallel()

	for _, search := range []searcher{searchRg, searchAg} {
		set := newSet(t.TempDir(), search)
		if set.Search() != string(search) {
			t.Fatalf("Search() = %q, want %q", set.Search(), search)
		}
		if schemaNamed(set, "grep") {
			t.Fatalf("grep tool offered for %s", search)
		}
		set.EnableLSP(&fakeLang{})
		definition := schemaNamedText(set, "lsp_definition")
		if !strings.Contains(definition, "over "+string(search)+" ") || strings.Contains(definition, "grep") {
			t.Fatalf("definition %q", definition)
		}
	}

	set := newSet(t.TempDir(), searchGrep)
	if !schemaNamed(set, "grep") {
		t.Fatal("grep tool missing")
	}
	set.EnableLSP(&fakeLang{})
	definition := schemaNamedText(set, "lsp_definition")
	if !strings.Contains(definition, "over grep ") {
		t.Fatalf("definition %q", definition)
	}
}

func TestNewUsesInstalledSearch(t *testing.T) {
	t.Parallel()

	set := New(t.TempDir())
	want := detectSearch(commandOnPath)
	if set.Search() != string(want) {
		t.Fatalf("Search() = %q, want %q", set.Search(), want)
	}
	if schemaNamed(set, "grep") != (want == searchGrep) {
		t.Fatalf("grep offered=%t search=%s", schemaNamed(set, "grep"), set.Search())
	}
}

func schemaNamed(set *Set, name string) bool {
	return schemaNamedText(set, name) != ""
}

func schemaNamedText(set *Set, name string) string {
	for _, schema := range set.Schemas() {
		if schema.Name == name {
			return schema.Description
		}
	}
	return ""
}
