// Package theme is the named TUI palettes.
// Each palette has a light side and a dark side. The TUI picks the side from the terminal background.
package theme

import (
	"fmt"
	"strings"
)

// Default is the palette when theme is unset.
const Default = "teal"

// Colors is one side of a palette, as hex colors.
type Colors struct {
	Accent string
	Warn   string
	Good   string
	Bad    string
	Faint  string
	Text   string
	Code   string
	Blur   string
}

type pair struct {
	light string
	dark  string
}

func (p pair) pick(dark bool) string {
	if dark {
		return p.dark
	}
	return p.light
}

type palette struct {
	name   string
	accent pair
	warn   pair
	good   pair
	bad    pair
	faint  pair
	text   pair
	code   pair
	blur   pair
}

func (p palette) colors(dark bool) Colors {
	return Colors{
		Accent: p.accent.pick(dark),
		Warn:   p.warn.pick(dark),
		Good:   p.good.pick(dark),
		Bad:    p.bad.pick(dark),
		Faint:  p.faint.pick(dark),
		Text:   p.text.pick(dark),
		Code:   p.code.pick(dark),
		Blur:   p.blur.pick(dark),
	}
}

// Neutrals stay zinc. Good stays green. Warn and bad move only when they would match the accent.
func base(name, accentLight, accentDark, codeLight, codeDark string) palette {
	return palette{
		name:   name,
		accent: pair{light: accentLight, dark: accentDark},
		warn:   pair{light: "#B45309", dark: "#FBBF24"},
		good:   pair{light: "#15803D", dark: "#86EFAC"},
		bad:    pair{light: "#BE123C", dark: "#FB7185"},
		faint:  pair{light: "#71717A", dark: "#A1A1AA"},
		text:   pair{light: "#18181B", dark: "#E4E4E7"},
		code:   pair{light: codeLight, dark: codeDark},
		blur:   pair{light: "#D4D4D8", dark: "#3F3F46"},
	}
}

func palettes() []palette {
	amber := base("amber", "#B45309", "#FBBF24", "#C2410C", "#FED7AA")
	amber.warn = pair{light: "#9A3412", dark: "#FB923C"}

	rose := base("rose", "#BE123C", "#FB7185", "#9F1239", "#FECDD3")
	rose.bad = pair{light: "#B91C1C", dark: "#F87171"}

	return []palette{
		base(Default, "#0F766E", "#5EEAD4", "#C2410C", "#FDBA74"),
		amber,
		rose,
		base("sky", "#0369A1", "#7DD3FC", "#1E40AF", "#93C5FD"),
		base("violet", "#6D28D9", "#C4B5FD", "#7E22CE", "#E9D5FF"),
		base("ink", "#27272A", "#FAFAFA", "#3F3F46", "#E4E4E7"),
	}
}

// Names is the palettes in picker order. The default is first.
func Names() []string {
	items := palettes()
	names := make([]string, len(items))
	for i, item := range items {
		names[i] = item.name
	}
	return names
}

// Parse returns the canonical palette name.
// An empty name is Default. Any other name must be one of Names.
func Parse(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		return Default, nil
	}
	for _, item := range palettes() {
		if item.name == name {
			return name, nil
		}
	}
	return "", fmt.Errorf("invalid theme %q", strings.TrimSpace(raw))
}

// Resolve is the light or dark side of a palette.
func Resolve(name string, dark bool) (Colors, error) {
	canonical, err := Parse(name)
	if err != nil {
		return Colors{}, err
	}
	for _, item := range palettes() {
		if item.name == canonical {
			return item.colors(dark), nil
		}
	}
	return Colors{}, fmt.Errorf("invalid theme %q", name)
}
