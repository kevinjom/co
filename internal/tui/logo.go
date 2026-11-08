package tui

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"
)

// Geometry of docs/logo.svg. The plate is 1024 and the mark is centered.
// The C is the annulus with a slot on the right; the ring sits inside it.
const (
	logoPixels = 20

	logoCenter = 512.0
	logoOuter  = 360.0
	logoInner  = 242.0
	logoMouth  = 124.0
	ringOuter  = 165.0
	ringInner  = 77.0

	// logoPad keeps the outer stroke off the pixel edge.
	logoPad = 1.06

	// Fills from docs/logo.svg. They stay put when the theme changes.
	logoBodyColor = "#2C1B4D"
	logoRingColor = "#A3FED1"
)

type logoInk int

const (
	logoEmpty logoInk = iota
	logoBody
	logoRing
)

// renderLogo draws the mark with half blocks, so each text row is two pixels tall.
func renderLogo(width int) string {
	if width < logoPixels {
		return ""
	}
	rows := rasterLogo(logoPixels)
	styles := newLogoStyles()
	lines := make([]string, 0, logoPixels/2)
	for y := 0; y < len(rows); y += 2 {
		var b strings.Builder
		for x := range rows[y] {
			b.WriteString(styles.cell(rows[y][x], rows[y+1][x]))
		}
		lines = append(lines, b.String())
	}
	return strings.Join(lines, "\n")
}

type logoStyles struct {
	body       lipgloss.Style
	ring       lipgloss.Style
	bodyOnRing lipgloss.Style
	ringOnBody lipgloss.Style
}

func newLogoStyles() logoStyles {
	body := lipgloss.Color(logoBodyColor)
	ring := lipgloss.Color(logoRingColor)
	return logoStyles{
		body:       lipgloss.NewStyle().Foreground(body),
		ring:       lipgloss.NewStyle().Foreground(ring),
		bodyOnRing: lipgloss.NewStyle().Foreground(body).Background(ring),
		ringOnBody: lipgloss.NewStyle().Foreground(ring).Background(body),
	}
}

func (s logoStyles) cell(top, bottom logoInk) string {
	switch {
	case top == logoEmpty && bottom == logoEmpty:
		return " "
	case top == logoBody && bottom == logoBody:
		return s.body.Render("█")
	case top == logoRing && bottom == logoRing:
		return s.ring.Render("█")
	case top == logoBody && bottom == logoEmpty:
		return s.body.Render("▀")
	case top == logoRing && bottom == logoEmpty:
		return s.ring.Render("▀")
	case top == logoEmpty && bottom == logoBody:
		return s.body.Render("▄")
	case top == logoEmpty && bottom == logoRing:
		return s.ring.Render("▄")
	case top == logoBody && bottom == logoRing:
		return s.bodyOnRing.Render("▀")
	case top == logoRing && bottom == logoBody:
		return s.ringOnBody.Render("▀")
	default:
		return " "
	}
}

func rasterLogo(n int) [][]logoInk {
	rows := make([][]logoInk, n)
	for y := range rows {
		rows[y] = make([]logoInk, n)
		for x := range rows[y] {
			rows[y][x] = logoPixel(n, x, y)
		}
	}
	return rows
}

func logoPixel(n, x, y int) logoInk {
	var body, ring, empty int
	for _, fy := range [2]float64{0.25, 0.75} {
		for _, fx := range [2]float64{0.25, 0.75} {
			sx := logoCenter + ((float64(x)+fx)/float64(n)*2-1)*logoOuter*logoPad
			sy := logoCenter + ((float64(y)+fy)/float64(n)*2-1)*logoOuter*logoPad
			switch logoSample(sx, sy) {
			case logoBody:
				body++
			case logoRing:
				ring++
			default:
				empty++
			}
		}
	}
	if body >= ring && body >= empty && body > 0 {
		return logoBody
	}
	if ring >= empty && ring > 0 {
		return logoRing
	}
	return logoEmpty
}

func logoSample(sx, sy float64) logoInk {
	dx := sx - logoCenter
	dy := sy - logoCenter
	r := math.Hypot(dx, dy)
	if r > ringInner && r < ringOuter {
		return logoRing
	}
	if r >= logoInner && r <= logoOuter && !(dx > 0 && math.Abs(dy) < logoMouth) {
		return logoBody
	}
	return logoEmpty
}
