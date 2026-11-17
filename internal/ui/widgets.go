package ui

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// ---- type ----

// display is the title face: condensed, spaced out.
func display(e ui.Element, size float32) ui.Element {
	return e.Font(fontDisplay + ", system-ui").Bold().FontSize(size).LetterSpacing(size / 16)
}

// mono is the face for paths and versions.
func mono(e ui.Element, size float32) ui.Element {
	return e.Font(fontMono + ", monospace").FontSize(size)
}

// lbl is the small caps label above a group of things.
func lbl(c *ui.Context, text string) {
	display(ui.Text(c, text), 12).TextColor(pal.faint).LetterSpacing(1.6)
}

// ---- pills and dots ----

type tone int

const (
	toneNeutral tone = iota
	toneAccent
	toneAmber
	toneCyan
	toneGreen
	toneRed
)

func (t tone) colors() (bg, border, fg ui.Color) {
	switch t {
	case toneAccent:
		return pal.accentDim, ui.Transparent, pal.accent
	case toneAmber:
		return pal.amberDim, pal.amberBorder, pal.amber
	case toneCyan:
		return pal.cyanDim, pal.cyanBorder, pal.cyan
	case toneGreen:
		return pal.greenDim, pal.greenBorder, pal.green
	case toneRed:
		return pal.redDim, pal.redBorder, pal.red
	}
	return pal.surface, pal.lineStrong, pal.muted
}

// pill is a small rounded label.
func pill(c *ui.Context, text string, t tone) {
	bg, border, fg := t.colors()
	ui.Row(c).Padding(2, 10).Height(22).Radius(99).Background(bg).Border(1, border).Children(func() {
		ui.Text(c, text).FontSize(12).Bold().TextColor(fg).SingleLine()
	})
}

// pillIcon is a pill with an icon before its text.
func pillIcon(c *ui.Context, ic *ui.SVG, text string, t tone) {
	bg, border, fg := t.colors()
	ui.Row(c).Padding(2, 10).Height(22).Gap(6).Radius(99).Background(bg).Border(1, border).Children(func() {
		icon(c, ic, 14, fg)
		ui.Text(c, text).FontSize(12).Bold().TextColor(fg).SingleLine()
	})
}

// badge is the count on a tab.
func badge(c *ui.Context, n int) {
	ui.Row(c).Height(20).Padding(0, 6).Radius(99).Background(pal.accentDim).Justify(ui.Center).Children(func() {
		ui.Text(c, itoa(n)).FontSize(12).Bold().TextColor(pal.accent)
	})
}

// dot is a small round status light, with a glow when lit.
func dot(c *ui.Context, color ui.Color, glow bool) {
	b := ui.Box(c).Width(8).Height(8).Radius(4).Background(color)
	if glow {
		b.Shadow(0, 0, 8, 0, color)
	}
}

// ---- cards ----

// card draws a bordered block; on tints it as selected. tone picks the
// warning, error, info and success variants.
func card(c *ui.Context, key string, on bool, t tone) ui.Element {
	bg, border := pal.card, pal.line
	switch {
	case t == toneAmber:
		bg, border = pal.amberDim, pal.amberBorder
	case t == toneRed:
		bg, border = pal.redDim, pal.redBorder
	case t == toneCyan:
		bg, border = pal.cyanDim, pal.cyanBorder
	case t == toneGreen:
		bg, border = pal.greenDim, pal.greenBorder
	case on:
		bg, border = pal.cardOn, pal.lineStrong
	}
	return ui.Row(c.Key(key)).Padding(14, 16).Gap(14).Radius(12).Background(bg).Border(1, border)
}

// notice is a one-line message in a tinted card, with its icon.
func notice(c *ui.Context, key string, t tone, ic *ui.SVG, title, text string) {
	_, _, fg := t.colors()
	card(c, key, false, t).Gap(12).Children(func() {
		icon(c, ic, 18, fg)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			if title != "" {
				display(ui.Text(c, title), 17).TextColor(pal.text)
			}
			if text != "" {
				ui.Text(c, text).FontSize(13).TextColor(pal.text)
			}
		})
	})
}

// ---- buttons ----

type btnStyle int

const (
	btnNormal btnStyle = iota
	btnPrimary
	btnGhost
	btnDanger
)

// button draws one of the four buttons and reports a click. A disabled
// button looks faded and never clicks.
func button(c *ui.Context, key, label string, ic *ui.SVG, style btnStyle, disabled bool, tip string) bool {
	h := float32(40)
	if style == btnPrimary {
		h = 44
	}
	b := ui.ButtonBase(c.Key(key)).Height(h).Padding(0, 16).Gap(8).Radius(10).Label(label).Disabled(disabled)
	if tip != "" {
		b.Tooltip(tip)
	}
	fg := pal.text
	switch style {
	case btnPrimary:
		h = 44
		b.Padding(0, 22).Gradient(pal.accentFill1, pal.accentFill2, 180)
		if !disabled {
			b.Shadow(0, 0, 22, 0, pal.accentGlow)
		}
		fg = pal.onAccent
	case btnGhost:
		fg = pal.muted
		if b.Hovered() && !disabled {
			b.Background(pal.surfaceHover)
			fg = pal.text
		}
	case btnDanger:
		fg = pal.red
		b.Border(1, pal.redBorder)
		if b.Hovered() && !disabled {
			b.Background(pal.redDim)
		}
	default:
		b.Background(pal.surface).Border(1, pal.lineStrong)
		if b.Hovered() && !disabled {
			b.Background(pal.surfaceHover)
		}
	}
	if disabled {
		b.Opacity(0.4)
	}
	b.Children(func() {
		if ic != nil {
			icon(c, ic, 16, fg)
		}
		if label != "" {
			display(ui.Text(c, label), 15).TextColor(fg).LetterSpacing(0.3)
		}
	})
	return b.Clicked() && !disabled
}

// iconButton is a square button with only an icon.
func iconButton(c *ui.Context, key string, ic *ui.SVG, label, tip string) bool {
	b := ui.ButtonBase(c.Key(key)).Width(40).Height(40).Radius(10).Center().Label(label).Tooltip(tip).
		Background(pal.surface).Border(1, pal.lineStrong)
	if b.Hovered() {
		b.Background(pal.surfaceHover)
	}
	b.Children(func() { icon(c, ic, 16, pal.muted) })
	return b.Clicked()
}

// segment is one choice of a row of equal choices; a segmented control is a
// few of them in a rounded track.
func segment(c *ui.Context, key, label, tip string, on, disabled bool, grow bool) bool {
	b := ui.ButtonBase(c.Key(key)).Height(34).Padding(0, 14).Radius(9).Tooltip(tip).Disabled(disabled).Label(label).
		Background(ternary(on, pal.accentDim, ui.Transparent)).Border(1, ternary(on, pal.accent, ui.Transparent))
	if grow {
		b.Grow(1)
	}
	if b.Hovered() && !on && !disabled {
		b.Background(pal.surfaceHover)
	}
	b.Children(func() {
		ui.Row(c).Grow(1).Justify(ui.Center).Children(func() {
			display(ui.Text(c, label), 14).TextColor(ternary(on, pal.accent, pal.muted)).LetterSpacing(0.3)
		})
	})
	return b.Clicked() && !disabled
}

// segmented draws a track of segments and returns the index clicked, or -1.
func segmented(c *ui.Context, key string, labels []string, cur int, grow bool) int {
	clicked := -1
	t := ui.Row(c).Padding(3).Gap(3).Radius(12).Background(pal.card).Border(1, pal.line)
	if grow {
		t.Grow(1)
	}
	t.Children(func() {
		for i, l := range labels {
			if segment(c, key+"-"+l, l, "", i == cur, false, grow) {
				clicked = i
			}
		}
	})
	return clicked
}

// checkbox is the square tick of a folder row.
func checkbox(c *ui.Context, on bool) {
	b := ui.Box(c).Width(22).Height(22).Radius(6).Center().Border(1, ternary(on, ui.Transparent, pal.lineStrong)).
		Background(ternary(on, pal.accentFill1, pal.surface))
	b.Children(func() {
		if on {
			icon(c, icons.check, 15, pal.onAccent)
		}
	})
}

// itoa is strconv.Itoa without the import in every file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// initials are the two letters on a game's tile.
func initials(name string) string {
	var out []rune
	for _, w := range splitWords(name) {
		r := []rune(w)
		out = append(out, []rune(toUpper(string(r[0])))[0])
		if len(out) == 2 {
			break
		}
	}
	return string(out)
}

func splitWords(s string) []string { return strings.Fields(s) }
func toUpper(s string) string      { return strings.ToUpper(s) }

// modal is a dialog in the look of the design: the panel color, a strong
// border, rounded and shadowed.
func modal(c *ui.Context, open *bool, fn func()) {
	ui.Modal(c, open, fn).Background(pal.panel).Border(1, pal.lineStrong).Radius(16).Padding(22).
		Shadow(0, 16, 48, 0, ui.RGBA(0, 0, 0, pal.shadow))
}
