package ui

import (
	"github.com/egoist/mygo/ui"
)

// View builds the whole window.
func (a *App) View(c *ui.Context) {
	setup()
	pal = a.look(c)
	c.SetTheme(yarmTheme())
	a.ctx = c

	a.shortcuts(c)
	ui.Row(c).Fill().Background(pal.ground).AlignItems(ui.Stretch).Children(func() {
		a.sidebar(c)
		a.mainPane(c)
	})
	a.dialogs(c)
}

// shortcuts are the few keys the window has: Ctrl+R rescan, Ctrl+N add a
// game, / or Ctrl+F to search, Ctrl+Enter to apply. Esc closes a dialog and
// is the dialog's own. They wait while a dialog is open.
func (a *App) shortcuts(c *ui.Context) {
	if c.Shortcut(ui.Cmd, ui.KeyR) {
		a.rescan(true)
	}
	if c.Shortcut(ui.Cmd, ui.KeyN) {
		a.openAddGame()
	}
	if c.Shortcut(ui.Cmd, ui.KeyF) || c.Shortcut(0, ui.KeySlash) {
		a.search.Focus()
	}
	if c.Shortcut(ui.Cmd, ui.KeyEnter) && a.view == viewGame {
		if e, ok := a.entry(a.selected); ok {
			if s := a.session(); s != nil && s.Dirty() && !a.dock.running && a.dock.report == nil {
				a.apply(e, s)
			}
		}
	}
}

func (a *App) dialogs(c *ui.Context) {
	a.settingsDialog(c)
	a.addGameDialog(c)
	if e, ok := a.entry(a.selected); ok {
		a.uninstallDialog(c, e)
		a.adoptDialog(c, e)
	}
}
