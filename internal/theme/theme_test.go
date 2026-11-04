package theme

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	got, err := Parse("  Rose ")
	if err != nil || got != "rose" {
		t.Fatalf("parse %q %v", got, err)
	}
	got, err = Parse("")
	if err != nil || got != Default {
		t.Fatalf("empty %q %v", got, err)
	}
	if _, err := Parse("nope"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err %v", err)
	}
}

func TestPalettes(t *testing.T) {
	seen := map[string]bool{}
	for _, name := range Names() {
		if seen[name] {
			t.Fatalf("duplicate %s", name)
		}
		seen[name] = true
		light, err := Resolve(name, false)
		if err != nil {
			t.Fatal(err)
		}
		dark, err := Resolve(name, true)
		if err != nil {
			t.Fatal(err)
		}
		if light.Accent == dark.Accent {
			t.Fatalf("%s accent does not follow the background", name)
		}
		for _, colors := range []Colors{light, dark} {
			for _, hex := range []string{colors.Accent, colors.Warn, colors.Good, colors.Bad, colors.Faint, colors.Text, colors.Code, colors.Blur} {
				if len(hex) != 7 || !strings.HasPrefix(hex, "#") {
					t.Fatalf("%s color %q", name, hex)
				}
			}
		}
	}
	if !seen[Default] {
		t.Fatal("default missing")
	}
}

func TestDefaultDarkIsTeal(t *testing.T) {
	colors, err := Resolve(Default, true)
	if err != nil {
		t.Fatal(err)
	}
	if colors.Accent != "#5EEAD4" || colors.Warn != "#FBBF24" || colors.Good != "#86EFAC" || colors.Bad != "#FB7185" {
		t.Fatalf("%+v", colors)
	}
	if colors.Faint != "#A1A1AA" || colors.Text != "#E4E4E7" || colors.Code != "#FDBA74" || colors.Blur != "#3F3F46" {
		t.Fatalf("%+v", colors)
	}
}
