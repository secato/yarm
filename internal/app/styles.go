// Package app implements YARM's terminal UI: a root model that routes
// between screens, plus the screens themselves.
package app

import (
	"charm.land/lipgloss/v2"
)

// Styles holds every style the UI draws with, resolved for the terminal's
// background.
//
// Colors are chosen once via lipgloss.LightDark rather than with adaptive
// colors, because Bubble Tea v2 reports the real background color and
// resolving up front keeps rendering pure.
type Styles struct {
	Title      lipgloss.Style
	Subtitle   lipgloss.Style
	Faint      lipgloss.Style
	Accent     lipgloss.Style
	Good       lipgloss.Style
	Warn       lipgloss.Style
	Bad        lipgloss.Style
	Border     lipgloss.Style
	Panel      lipgloss.Style
	StatusBar  lipgloss.Style
	Selected   lipgloss.Style
	TableHead  lipgloss.Style
	TableCell  lipgloss.Style
	Overlay    lipgloss.Style
	OverlayDim lipgloss.Style
}

// NewStyles builds the palette for a light or dark terminal.
func NewStyles(isDark bool) Styles {
	c := lipgloss.LightDark(isDark)

	var (
		fg      = c(lipgloss.Color("#1a1a1a"), lipgloss.Color("#e6e6e6"))
		faint   = c(lipgloss.Color("#6b7280"), lipgloss.Color("#8b93a1"))
		accent  = c(lipgloss.Color("#7c3aed"), lipgloss.Color("#a78bfa"))
		good    = c(lipgloss.Color("#15803d"), lipgloss.Color("#4ade80"))
		warn    = c(lipgloss.Color("#b45309"), lipgloss.Color("#fbbf24"))
		bad     = c(lipgloss.Color("#b91c1c"), lipgloss.Color("#f87171"))
		border  = c(lipgloss.Color("#d4d4d8"), lipgloss.Color("#3f3f46"))
		selBg   = c(lipgloss.Color("#ede9fe"), lipgloss.Color("#3b2f5e"))
		panelBg = c(lipgloss.Color("#fafafa"), lipgloss.Color("#1c1c20"))
	)

	base := lipgloss.NewStyle().Foreground(fg)

	return Styles{
		Title:    base.Bold(true).Foreground(accent),
		Subtitle: base.Bold(true),
		Faint:    lipgloss.NewStyle().Foreground(faint),
		Accent:   lipgloss.NewStyle().Foreground(accent),
		Good:     lipgloss.NewStyle().Foreground(good),
		Warn:     lipgloss.NewStyle().Foreground(warn),
		Bad:      lipgloss.NewStyle().Foreground(bad),
		Border:   lipgloss.NewStyle().Foreground(border),
		Panel: lipgloss.NewStyle().
			Background(panelBg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 1),
		StatusBar: lipgloss.NewStyle().Foreground(faint),
		Selected:  lipgloss.NewStyle().Background(selBg).Foreground(fg).Bold(true),
		TableHead: lipgloss.NewStyle().Foreground(faint).Bold(true),
		TableCell: base,
		Overlay: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(1, 2),
		OverlayDim: lipgloss.NewStyle().Foreground(faint),
	}
}
