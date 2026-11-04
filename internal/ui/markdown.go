package ui

import (
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
	xansi "github.com/charmbracelet/x/ansi"
)

type mdKey struct {
	name  string
	dark  bool
	width int
	muted bool
}

type markdownCache struct {
	items map[mdKey]*glamour.TermRenderer
}

func (c *markdownCache) render(th theme, text string, width int, muted bool) string {
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	if width < 1 {
		width = 1
	}
	renderer := c.renderer(th, width, muted)
	if renderer == nil {
		return wrapText(text, width)
	}
	out, err := renderer.Render(text)
	if err != nil || strings.TrimSpace(xansi.Strip(out)) == "" {
		return wrapText(text, width)
	}
	return clampWidth(strings.Trim(out, "\n"), width)
}

func (c *markdownCache) renderer(th theme, width int, muted bool) *glamour.TermRenderer {
	if c.items == nil {
		c.items = map[mdKey]*glamour.TermRenderer{}
	}
	key := mdKey{name: th.name, dark: th.dark, width: width, muted: muted}
	if renderer, ok := c.items[key]; ok {
		return renderer
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(markdownStyle(th, muted)),
		glamour.WithWordWrap(width),
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return nil
	}
	c.items[key] = renderer
	return renderer
}

func markdownStyle(th theme, muted bool) ansi.StyleConfig {
	base := styles.DarkStyleConfig
	if !th.dark {
		base = styles.LightStyleConfig
	}

	text := th.textHex
	code := th.codeHex
	link := th.accentHex
	head := th.accentHex
	if muted {
		text = th.faintHex
		code = th.faintHex
		link = th.faintHex
		head = th.warnHex
	}

	base.Document.Margin = uintPtr(0)
	base.Document.BlockPrefix = ""
	base.Document.BlockSuffix = ""
	base.Document.Color = stringPtr(text)

	base.CodeBlock.Margin = uintPtr(0)
	base.CodeBlock.Color = stringPtr(text)
	if muted {
		base.CodeBlock.Chroma = nil
	} else if base.CodeBlock.Chroma != nil {
		copied := *base.CodeBlock.Chroma
		copied.Background = ansi.StylePrimitive{}
		base.CodeBlock.Chroma = &copied
	}

	heading := ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color: stringPtr(head),
			Bold:  boolPtr(true),
		},
	}
	base.Heading = heading
	base.H1 = heading
	base.H2 = heading
	base.H3 = heading
	base.H4 = heading
	base.H5 = heading
	base.H6 = heading

	base.Code = ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{Color: stringPtr(code)},
	}
	base.Link.Color = stringPtr(link)
	base.Link.Underline = boolPtr(true)
	base.LinkText.Color = stringPtr(link)
	base.LinkText.Bold = boolPtr(true)
	base.BlockQuote.Color = stringPtr(th.faintHex)
	base.HorizontalRule.Color = stringPtr(th.faintHex)
	base.Strong.Bold = boolPtr(true)
	base.Emph.Italic = boolPtr(true)
	base.Strikethrough.CrossedOut = boolPtr(true)
	return base
}

func uintPtr(v uint) *uint { return &v }

func boolPtr(v bool) *bool { return &v }

func stringPtr(v string) *string { return &v }

func clampWidth(text string, width int) string {
	if width < 1 || text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if lipgloss.Width(line) > width {
			lines[i] = xansi.Truncate(line, width, "…")
		}
	}
	return strings.Join(lines, "\n")
}
