package ui

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/secato/yarm/internal/core"
)

// resourcesView is the state of the Resources page.
type resourcesView struct {
	state loadState
	err   string
	res   core.Resources
	pane  int
	all   bool
	busy  string // the id being downloaded or deleted
	clean bool   // the "clean unused" confirmation is open
}

func (a *App) openResources() {
	a.view = viewResources
	a.loadResources()
}

func (a *App) loadResources() {
	if a.deps.Cache == nil {
		a.res.state, a.res.err = stateFailed, "The download cache is not available."
		return
	}
	a.res.state = stateLoading
	var res core.Resources
	var err error
	deps := a.deps
	a.bg(func() { res, err = core.LoadResources(context.Background(), deps) }, func() {
		if err != nil {
			a.res.state, a.res.err = stateFailed, core.FriendlyError(err)
			return
		}
		a.res.state, a.res.err, a.res.res = stateReady, "", res
	})
}

func (a *App) resourcesPane(c *ui.Context) {
	r := &a.res
	ui.Column(c).Fill().Padding(28).Gap(20).Children(func() {
		items, inUse, _ := r.res.Items()
		ui.Row(c).Gap(16).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				lbl(c, "DOWNLOAD CACHE")
				display(ui.Text(c, "RESOURCES"), 40)
			})
			ui.Column(c).Children(func() {
				mono(ui.Text(c, core.HumanSize(r.res.Total)), 18).TextAlign(ui.End)
				ui.Text(c, fmt.Sprintf("in %s · %d in use", plural(items, "item"), inUse)).FontSize(12).TextColor(pal.muted).TextAlign(ui.End)
			})
			if button(c, "clean", "Clean unused", icons.trash, btnDanger, r.state != stateReady || items == inUse, "Delete every download no install uses") {
				r.clean = true
			}
		})

		panes := make([]string, core.PaneCount)
		for p := core.Pane(0); p < core.PaneCount; p++ {
			panes[p] = p.Label()
		}
		ui.Row(c).Gap(14).Children(func() {
			if i := segmented(c, "pane", panes, r.pane, false); i >= 0 {
				r.pane = i
			}
			ui.Spacer(c)
			if core.Pane(r.pane) != core.PaneReShade {
				cur := 0
				if r.all {
					cur = 1
				}
				if i := segmented(c, "res-all", []string{"Curated", "All"}, cur, false); i >= 0 {
					r.all = i == 1
				}
			}
		})

		switch r.state {
		case stateLoading, stateIdle:
			ui.Row(c).Gap(10).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "Reading the cache…").TextColor(pal.muted)
			})
		case stateFailed:
			notice(c, "res-err", toneRed, icons.warn, "Couldn't read the cache", r.err)
		default:
			rows := r.res.Visible(core.Pane(r.pane), r.all)
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(10).Children(func() {
					if len(rows) == 0 {
						ui.Text(c, "Nothing here yet. Choose All to see everything on offer.").TextColor(pal.muted)
					}
					for _, row := range rows {
						a.resourceRow(c, row)
					}
				})
			})
		}

		notice(c, "res-hint", toneCyan, icons.info, "",
			"Cached items are shared by every game, so the fifth game copies instead of downloading again. Items in use can't be deleted.")
	})

	if ui.AlertDialog(c, &r.clean, "Clean everything no install uses?",
		"Keeps every download a recorded install uses, so editing it never has to download again, and frees the rest. Custom content lives outside the cache and is untouched.",
		"Cancel", "Clean unused") == 1 {
		a.cleanUnused()
	}
}

func (a *App) resourceRow(c *ui.Context, row core.ResourceRow) {
	on := row.Cached
	card(c, "res-"+row.ID, on, toneNeutral).Gap(16).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			display(ui.Text(c, row.Name), 18).SingleLine()
			sub := row.Sub
			if row.Cached && row.Size > 0 {
				sub += " · " + core.HumanSize(row.Size)
			}
			if sub != "" {
				mono(ui.Text(c, sub), 12).TextColor(pal.muted).SingleLine()
			}
		})
		switch {
		case row.Custom:
			pill(c, "Custom", toneAccent)
		case row.Cached:
			pill(c, "Cached", toneGreen)
		case !row.Downloadable:
			pill(c, "Manual", toneNeutral)
		default:
			pill(c, "Not downloaded", toneNeutral)
		}
		if row.InUse() {
			pill(c, "In use by "+plural(row.UsedBy, "game"), toneCyan)
		} else if row.Cached {
			pill(c, "Not in use", toneNeutral)
		}
		busy := a.res.busy != ""
		switch {
		case row.Custom:
		case row.Cached:
			if row.CanRefresh() {
				if button(c, "refresh-"+row.ID, "Refresh", icons.refresh, btnNormal, busy, "Fetch it again, to pick up upstream changes") {
					a.resourceAction(row, "refresh")
				}
			}
			tip := ""
			if row.InUse() {
				tip = "Used by an install, so it can't be deleted"
			}
			if button(c, "delete-"+row.ID, "Delete", icons.trash, btnNormal, busy || row.InUse(), tip) {
				a.resourceAction(row, "delete")
			}
		case row.Downloadable:
			label := "Download"
			if a.res.busy == row.ID {
				label = "Downloading…"
			}
			if button(c, "download-"+row.ID, label, icons.download, btnNormal, busy, "") {
				a.resourceAction(row, "download")
			}
		}
	})
}

// resourceAction runs one download, delete or refresh in the background and
// reloads the page when it is done.
func (a *App) resourceAction(row core.ResourceRow, what string) {
	a.res.busy = row.ID
	deps := a.deps
	var err error
	a.bg(func() {
		switch what {
		case "download":
			err = core.DownloadResource(context.Background(), deps, row)
		case "delete":
			err = core.DeleteResource(deps, row)
		case "refresh":
			err = core.RefreshResource(context.Background(), deps, row)
		}
	}, func() {
		a.res.busy = ""
		if err != nil {
			a.toast(core.FriendlyError(err))
		}
		a.loadResources()
		// What is cached shows as a badge in every game's lists.
		a.sess, a.sessGame = nil, ""
	})
}

func (a *App) cleanUnused() {
	deps := a.deps
	var freed int64
	var kept int
	var err error
	a.res.busy = "clean"
	a.bg(func() { freed, kept, err = core.CleanUnused(deps) }, func() {
		a.res.busy = ""
		switch {
		case err != nil:
			a.toast(core.FriendlyError(err))
		case freed == 0:
			a.toast("Nothing to clean: everything cached is in use.")
		default:
			a.toast(fmt.Sprintf("Freed %s; kept %s in use.", core.HumanSize(freed), plural(kept, "item")))
		}
		a.loadResources()
		a.sess, a.sessGame = nil, ""
	})
}
