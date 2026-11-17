package ui

import (
	"fmt"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/secato/yarm/internal/core"
)

// customView is the state of the Custom content page.
type customView struct {
	state loadState
	err   string
	found core.CustomFound
}

func (a *App) openCustom() {
	a.view = viewCustom
	a.custom.state = stateLoading
	dir := a.deps.CustomDir
	var found core.CustomFound
	var err error
	a.bg(func() { found, err = core.ScanCustomContent(dir) }, func() {
		if err != nil {
			a.custom.state, a.custom.err = stateFailed, core.FriendlyError(err)
			return
		}
		a.custom.state, a.custom.err, a.custom.found = stateReady, "", found
	})
}

func (a *App) customPane(c *ui.Context) {
	dir := a.deps.CustomDir
	ui.Column(c).Fill().Padding(28).Gap(20).Children(func() {
		ui.Row(c).Gap(16).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				lbl(c, "YOUR OWN SHADERS AND ADD-ONS")
				display(ui.Text(c, "CUSTOM CONTENT"), 40)
			})
			if button(c, "open-custom", "Open folder", icons.folder, btnPrimary, dir == "", "") {
				// The folder is created on demand: most people never make one
				// until they are told where it goes.
				_ = os.MkdirAll(dir, 0o755)
				_ = mygo.Shell.OpenPath(dir)
			}
		})
		card(c, "custom-how", false, toneNeutral).Children(func() {
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				ui.Text(c, "Drop shader packs and add-ons into this folder, each in its own folder under shaders/ or addons/. They show up beside the catalog, marked Custom, the next time you open a game.").
					FontSize(13).TextColor(pal.muted)
				ui.Box(c).Padding(10, 12).Radius(10).Background(pal.code).Border(1, pal.line).Children(func() {
					mono(ui.Text(c, dir), 13)
				})
			})
		})

		f := a.custom.found
		switch a.custom.state {
		case stateLoading, stateIdle:
			ui.Row(c).Gap(10).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "Looking in the folder…").TextColor(pal.muted)
			})
		case stateFailed:
			notice(c, "custom-err", toneRed, icons.warn, "Couldn't read the folder", a.custom.err)
		default:
			n := len(f.Shaders) + len(f.Addons)
			lbl(c, fmt.Sprintf("FOUND · %d", n))
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(10).Children(func() {
					if n == 0 {
						ui.Text(c, "Nothing found yet. Drop a shader pack or add-on into one of the folders above.").TextColor(pal.muted)
					}
					for _, s := range f.Shaders {
						a.customRow(c, s.ID, s.Name, s.Description, s.Path, "Shader pack", toneAccent)
					}
					for _, ad := range f.Addons {
						a.customRow(c, ad.ID, ad.Name, ad.Description, ad.Path, "Add-on", toneCyan)
					}
				})
			})
		}
	})
}

func (a *App) customRow(c *ui.Context, id, name, desc, path, kind string, t tone) {
	card(c, "custom-"+id, false, toneNeutral).Gap(16).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			display(ui.Text(c, name), 18).SingleLine()
			mono(ui.Text(c, path), 12).TextColor(pal.muted).SingleLine()
			if desc != "" {
				ui.Text(c, desc).FontSize(13).TextColor(pal.muted).SingleLine()
			}
		})
		pill(c, kind, t)
	})
}
