package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	palette "github.com/kevinjom/co/internal/theme"
)

// theme is one palette painted for the current terminal background.
type theme struct {
	name        string
	dark        bool
	accent      lipgloss.Style
	warn        lipgloss.Style
	good        lipgloss.Style
	dim         lipgloss.Style
	bad         lipgloss.Style
	code        lipgloss.Style
	accentColor color.Color
	warnColor   color.Color
	blurBorder  color.Color
	faint       color.Color
	textHex     string
	accentHex   string
	warnHex     string
	faintHex    string
	codeHex     string
}

func newTheme(name string, dark bool) (theme, error) {
	canonical, err := palette.Parse(name)
	if err != nil {
		return theme{}, err
	}
	colors, err := palette.Resolve(canonical, dark)
	if err != nil {
		return theme{}, err
	}
	accent := lipgloss.Color(colors.Accent)
	warn := lipgloss.Color(colors.Warn)
	faint := lipgloss.Color(colors.Faint)
	return theme{
		name:        canonical,
		dark:        dark,
		accent:      lipgloss.NewStyle().Foreground(accent),
		warn:        lipgloss.NewStyle().Foreground(warn),
		good:        lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Good)),
		dim:         lipgloss.NewStyle().Foreground(faint),
		bad:         lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Bad)),
		code:        lipgloss.NewStyle().Foreground(lipgloss.Color(colors.Code)),
		accentColor: accent,
		warnColor:   warn,
		blurBorder:  lipgloss.Color(colors.Blur),
		faint:       faint,
		textHex:     colors.Text,
		accentHex:   colors.Accent,
		warnHex:     colors.Warn,
		faintHex:    colors.Faint,
		codeHex:     colors.Code,
	}, nil
}

func (m *model) applyTheme(name string, dark bool) error {
	th, err := newTheme(name, dark)
	if err != nil {
		return err
	}
	m.theme = th
	styleInput(&m.input, th)
	m.spin.Style = lipgloss.NewStyle().Foreground(th.accentColor)
	return nil
}
