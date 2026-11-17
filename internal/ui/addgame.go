package ui

import (
	"path/filepath"

	"github.com/egoist/mygo/ui"

	"github.com/secato/yarm/internal/core"
)

// addGameView is the state of the Add a game dialog.
type addGameView struct {
	open    bool
	path    string
	name    string
	touched bool // the name was typed, so it stops following the folder
	check   core.FolderCheck
}

func (a *App) openAddGame() {
	a.add = addGameView{open: true}
}

// setAddPath takes a folder, checks it and, until the name is typed, names
// the game after it.
func (a *App) setAddPath(path string) {
	a.add.path = path
	a.add.check = core.CheckGameFolder(path)
	if !a.add.touched && a.add.check.Path != "" {
		a.add.name = filepath.Base(a.add.check.Path)
	}
}

func (a *App) addGameDialog(c *ui.Context) {
	modal(c, &a.add.open, func() {
		ui.Column(c).Width(440).Gap(16).Children(func() {
			display(ui.Text(c, "ADD A GAME"), 26)
			ui.Column(c).Gap(6).Children(func() {
				lbl(c, "GAME FOLDER")
				ui.Row(c).Gap(8).Children(func() {
					p := a.add.path
					if ui.TextInput(c.Key("add-path"), &p).Placeholder("/path/to/game").Label("Game folder").AutoFocus().Grow(1).Changed() {
						a.setAddPath(p)
					}
					if button(c, "add-browse", "Browse…", icons.folder, btnNormal, false, "") {
						a.pickThen("Game folder", a.setAddPath)
					}
				})
			})
			ui.Column(c).Gap(6).Children(func() {
				lbl(c, "NAME")
				n := a.add.name
				if ui.TextInput(c.Key("add-name"), &n).Placeholder("Game name").Label("Name").Changed() {
					a.add.name, a.add.touched = n, true
				}
			})
			if msg := a.add.check.Message(); msg != "" {
				notice(c, "add-problem", toneRed, icons.warn, "", msg)
			} else if a.add.check.OK() {
				notice(c, "add-ok", toneGreen, icons.check, "", plural(a.add.check.Exes, "executable")+" found.")
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if button(c, "add-cancel", "Cancel", nil, btnGhost, false, "") {
					a.add.open = false
				}
				if button(c, "add-go", "Add game", nil, btnPrimary, !a.add.check.OK(), "") {
					a.addGame()
				}
			})
		})
	})
}

func (a *App) addGame() {
	next, err := core.AddManualGame(a.deps.ConfigDir, a.cfg, a.add.name, a.add.check.Path)
	if err != nil {
		a.toast(core.FriendlyError(err))
		return
	}
	name := a.add.name
	a.add.open = false
	a.cfg = next
	if a.opts.NewLoader != nil {
		a.loader = a.opts.NewLoader(next)
	}
	a.rescan(false)
	a.toast(name + " was added.")
}
