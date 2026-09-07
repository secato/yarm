// Package app implements YARM's terminal UI: a root model that routes
// between screens, plus the screens themselves.
package app

import (
	"charm.land/bubbles/v2/table"
	"charm.land/lipgloss/v2"
)

// Styles holds every style the UI draws with, resolved for the terminal's
// background.
//
// Colors are chosen once via lipgloss.LightDark rather than with adaptive
// colors, because Bubble Tea v2 reports the real background color and
// resolving up front keeps rendering pure.
type Styles struct {
	Title     lipgloss.Style
	Subtitle  lipgloss.Style
	Faint     lipgloss.Style
	Accent    lipgloss.Style
	Good      lipgloss.Style
	Warn      lipgloss.Style
	Bad       lipgloss.Style
	Panel     lipgloss.Style
	StatusBar lipgloss.Style
	Selected  lipgloss.Style
	Overlay   lipgloss.Style

	// tableHead and tableCell are only ever handed to bubbles' table
	// through Table(); nothing else draws with them.
	tableHead lipgloss.Style
	tableCell lipgloss.Style
}

// Table maps this palette onto bubbles' table styles. Without it a table
// renders with the library's own defaults, which hardcode a pink for the
// selected row and give the header no color at all — so the one list in
// the app built on bubbles/table was the one list that ignored the
// terminal's theme and highlighted its cursor differently from everything
// else.
//
// The padding matches DefaultStyles: the table lays its columns out
// assuming each cell carries one column of padding on either side.
func (s Styles) Table() table.Styles {
	return table.Styles{
		Header:   s.tableHead.Padding(0, 1),
		Cell:     s.tableCell.Padding(0, 1),
		Selected: s.Selected,
	}
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
		Panel: lipgloss.NewStyle().
			Background(panelBg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 1),
		StatusBar: lipgloss.NewStyle().Foreground(faint),
		Selected:  lipgloss.NewStyle().Background(selBg).Foreground(fg).Bold(true),
		Overlay: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accent).
			Padding(1, 2),

		tableHead: lipgloss.NewStyle().Foreground(faint).Bold(true),
		// Deliberately colorless: bubbles renders each cell first and then
		// wraps the whole row in Selected, so a cell that sets a color
		// emits a reset that also clears the row's highlight — the
		// selection ends up striped. Cells inherit the terminal's own
		// foreground, which is what base was setting anyway.
		tableCell: lipgloss.NewStyle(),
	}
}
