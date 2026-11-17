package ui

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/secato/yarm/internal/core"
)

// gameStatus is what a game's row says: its second line and the light.
type gameStatus struct {
	line  string
	light ui.Color
	lit   bool
	on    bool // a light is shown at all
}

func (a *App) statusOf(g core.GameEntry) gameStatus {
	switch {
	case g.NativeBuild:
		return gameStatus{line: "Native Linux build"}
	case g.ScanErr != nil:
		return gameStatus{line: "Can't read this folder", light: pal.red, on: true}
	}
	for _, grp := range g.Groups {
		if in := grp.Installed; in != nil {
			ver := in.ReShade.Version
			if ver == "" || strings.Contains(ver, "unknown") {
				ver = "(adopted)"
			}
			line := "ReShade " + ver
			if n := len(in.Packages) + len(in.Custom); n > 0 {
				line += " · " + plural(n, "shader pack")
			}
			return gameStatus{line: line, light: pal.green, lit: true, on: true}
		}
	}
	if g.HasUnmanaged() {
		return gameStatus{line: "ReShade found, not tracked by YARM", light: pal.amber, lit: true, on: true}
	}
	if g.Provider == "manual" {
		return gameStatus{line: g.Root}
	}
	return gameStatus{line: "Nothing installed"}
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// visibleGames filters by the search box, matching name or provider.
func (a *App) visibleGames() []core.GameEntry {
	q := strings.ToLower(strings.TrimSpace(a.query))
	var out []core.GameEntry
	for _, g := range a.games {
		if q == "" || strings.Contains(strings.ToLower(g.Name), q) ||
			strings.Contains(strings.ToLower(providerLabel(g.Provider)), q) {
			out = append(out, g)
		}
	}
	return out
}

// sidebar is the left column: the logo, search, the games, and the places
// that are not games.
func (a *App) sidebar(c *ui.Context) {
	ui.Column(c).Width(300).FillHeight().Background(pal.panel).BorderWidth(0, 1, 0, 0).BorderColor(pal.line).
		Padding(18, 14, 14).Gap(14).Children(func() {
		a.logo(c)

		ui.Row(c).Gap(8).Children(func() {
			ui.SearchField(c.Key("search"), &a.query).Placeholder("Search games").Label("Search games").Grow(1).Bind(&a.search)
			if iconButton(c, "rescan", icons.refresh, "Rescan games", "Rescan (Ctrl+R)") {
				a.rescan(true)
			}
			if iconButton(c, "addgame", icons.plus, "Add a game", "Add a game (Ctrl+N)") {
				a.openAddGame()
			}
		})

		ui.Scroll(c).Grow(1).Children(func() {
			ui.Column(c).Gap(4).Children(func() { a.gameList(c) })
		})

		ui.Column(c).Gap(4).Children(func() {
			a.footerButton(c, "resources", "Resources", icons.download, a.view == viewResources, func() { a.openResources() })
			a.footerButton(c, "custom", "Custom content", icons.layers, a.view == viewCustom, func() { a.openCustom() })
			a.footerButton(c, "settings", "Settings", icons.sliders, false, func() { a.openSettings() })
		})
		a.themeToggle(c)
	})
}

func (a *App) logo(c *ui.Context) {
	ui.Row(c).Gap(12).Padding(0, 4).Children(func() {
		ui.Box(c).Width(36).Height(36).Radius(10).Gradient(ui.Hex("#A78BFA"), ui.Hex("#6D28D9"), 135).Center().
			Shadow(0, 0, 18, 0, pal.accentGlow).Children(func() {
			icon(c, icons.logo, 20, ui.Hex("#FFFFFF"))
		})
		display(ui.Text(c, "YARM"), 24).LetterSpacing(3.4)
	})
}

func (a *App) footerButton(c *ui.Context, key, label string, ic *ui.SVG, on bool, click func()) {
	b := ui.ButtonBase(c.Key(key)).Height(40).Padding(0, 12).Gap(10).Radius(10).Label(label).Justify(ui.Start).
		Background(ternary(on, pal.accentDim, ui.Transparent)).Border(1, ternary(on, pal.accent, ui.Transparent))
	if b.Hovered() && !on {
		b.Background(pal.surfaceHover)
	}
	fg := ternary(on, pal.accent, pal.muted)
	b.Children(func() {
		icon(c, ic, 16, fg)
		display(ui.Text(c, label), 15).TextColor(fg).LetterSpacing(0.4)
	})
	if b.Clicked() {
		click()
	}
}

// themeToggle is the Dark / Light / System switch at the bottom.
func (a *App) themeToggle(c *ui.Context) {
	cur := a.themeChoice()
	locked := a.themeMode != ""
	ui.Row(c).Padding(3).Gap(3).Radius(12).Background(pal.card).Border(1, pal.line).Children(func() {
		for _, o := range []struct{ id, label, tip string }{
			{"dark", "Dark", "Always use the dark look"},
			{"light", "Light", "Always use the light look"},
			{"system", "System", "Follow the desktop's dark or light setting"},
		} {
			tip := o.tip
			if locked {
				tip = "YARM_THEME is set, so it decides the look"
			}
			if segment(c, "theme-"+o.id, o.label, tip, cur == o.id, locked, true) {
				a.setTheme(o.id)
			}
		}
	})
}

// gameList is the body of the sidebar: skeletons while looking, a note when
// there is nothing, otherwise the games in their groups.
func (a *App) gameList(c *ui.Context) {
	switch {
	case a.gamesLoad == stateLoading && len(a.games) == 0:
		a.skeletons(c)
		return
	case a.gamesLoad == stateFailed && len(a.games) == 0:
		ui.Column(c).Padding(24, 10).Gap(8).Children(func() {
			display(ui.Text(c, "Couldn't read your games"), 18).TextAlign(ui.Center)
			ui.Text(c, a.gamesErr).FontSize(13).TextColor(pal.muted).TextAlign(ui.Center)
		})
		return
	}

	games := a.visibleGames()
	if len(games) == 0 {
		ui.Column(c).Padding(24, 10).Gap(8).Children(func() {
			title := "No games found"
			note := "Checked Steam, GOG, Epic and Battle.net."
			if strings.TrimSpace(a.query) != "" {
				title, note = "No game matches", "Try another name, or clear the search."
			}
			display(ui.Text(c, title), 18).TextAlign(ui.Center)
			ui.Text(c, note).FontSize(13).TextColor(pal.muted).TextAlign(ui.Center)
		})
		return
	}

	var installed, rest, manual []core.GameEntry
	for _, g := range games {
		switch {
		case g.Provider == "manual":
			manual = append(manual, g)
		case a.statusOf(g).on && a.statusOf(g).light == pal.green, g.InstalledSummary() != "":
			installed = append(installed, g)
		default:
			rest = append(rest, g)
		}
	}
	for _, grp := range []struct {
		title string
		games []core.GameEntry
	}{{"Installed", installed}, {"Not installed", rest}, {"Added by you", manual}} {
		if len(grp.games) == 0 {
			continue
		}
		ui.Row(c.Key("sec-"+grp.title)).Padding(10, 8, 2).Children(func() {
			lbl(c, fmt.Sprintf("%s · %d", strings.ToUpper(grp.title), len(grp.games)))
		})
		for _, g := range grp.games {
			a.gameRow(c, g)
		}
	}
}

func (a *App) skeletons(c *ui.Context) {
	ui.Row(c).Padding(6, 8, 2).Children(func() { lbl(c, "LOOKING FOR YOUR GAMES…") })
	for i, w := range []float32{0.7, 0.55, 0.8, 0.62} {
		ui.Row(c.Key(fmt.Sprintf("sk-%d", i))).Gap(10).Padding(6, 4).Children(func() {
			ui.Box(c).Width(34).Height(34).Radius(8).Background(pal.surface)
			ui.Column(c).Grow(1).Gap(6).Children(func() {
				ui.Box(c).Height(12).WidthPercent(w * 100).Radius(6).Background(pal.surface)
				ui.Box(c).Height(10).WidthPercent((1-w)*100 + 20).Radius(5).Background(pal.surfaceHover)
			})
		})
	}
}

// gameRow is one game of the list: a tile of its own color, its name and
// state, where it comes from, and a light.
func (a *App) gameRow(c *ui.Context, g core.GameEntry) {
	on := a.view == viewGame && a.selected == g.ID
	st := a.statusOf(g)
	row := ui.ButtonBase(c.Key("game-"+g.ID)).Height(52).Padding(6, 10).Gap(10).Radius(12).Label(g.Name).
		Background(ternary(on, pal.cardOn, ui.Transparent)).Border(1, ternary(on, pal.accent, ui.Transparent))
	if on {
		row.Shadow(0, 0, 18, 0, pal.accentGlow)
	} else if row.Hovered() {
		row.Background(pal.surfaceHover)
	}
	row.Children(func() {
		from, to := tileColors(g.Name)
		ui.Box(c).Width(34).Height(34).Radius(9).Gradient(from, to, 135).Center().Children(func() {
			display(ui.Text(c, initials(g.Name)), 14).TextColor(ui.Hex("#FFFFFF"))
		})
		ui.Column(c).Grow(1).Children(func() {
			ui.Text(c, g.Name).Bold().SingleLine().FontSize(15)
			ui.Text(c, st.line).FontSize(12).TextColor(pal.muted).SingleLine()
		})
		if p := providerLabel(g.Provider); p != "" {
			pill(c, p, toneNeutral)
		}
		if st.on {
			dot(c, st.light, st.lit)
		}
	})
	if row.Clicked() {
		a.choose(g.ID)
	}
}
