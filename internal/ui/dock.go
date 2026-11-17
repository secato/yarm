package ui

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/secato/yarm/internal/core"
	"github.com/secato/yarm/internal/install"
)

// dock is the bar at the bottom of a game: what applying will do, the
// buttons, and — one apply at a time — its progress and its result.
type dock struct {
	running  bool
	cancel   context.CancelFunc
	canceled bool
	title    string // "Applying to Elden Ring"
	step     string // the current step's own words
	done     int64
	total    int64
	report   *core.Report
	showAll  bool // the result lists the finer points
}

// dockView draws the dock for the game on the page.
func (a *App) dockView(c *ui.Context, e core.GameEntry, s *core.Session) {
	ui.Column(c).Margin(0, 28, 24).Padding(14, 18).Gap(12).Radius(14).Background(pal.panel).Border(1, pal.lineStrong).
		Shadow(0, 16, 48, 0, ui.RGBA(0, 0, 0, pal.shadow)).Children(func() {
		switch {
		case a.dock.running:
			a.dockRunning(c)
		case a.dock.report != nil:
			a.dockResult(c)
		default:
			a.dockIdle(c, e, s)
		}
	})
}

func (a *App) dockRunning(c *ui.Context) {
	d := &a.dock
	ui.Row(c).Gap(16).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			lbl(c, strings.ToUpper(d.title))
			mono(ui.Text(c, ternary(d.step == "", "Starting…", d.step)), 14).Margin(4, 0, 0).SingleLine()
		})
		if button(c, "cancel", "Cancel", nil, btnNormal, d.canceled, "") {
			d.canceled = true
			if d.cancel != nil {
				d.cancel()
			}
		}
	})
	v := -1.0
	if d.total > 0 {
		v = float64(d.done) / float64(d.total)
	}
	ui.Progress(c, v).Label("Apply progress")
	ui.Text(c, "Cancel undoes what this run wrote. Other games wait until it finishes.").FontSize(12).TextColor(pal.muted)
}

func (a *App) dockResult(c *ui.Context) {
	rep := a.dock.report
	ui.Row(c).Gap(16).Children(func() {
		display(ui.Text(c, rep.Summary()), 17).Grow(1)
		if button(c, "done", "Done", nil, btnPrimary, false, "") {
			a.finishApply()
		}
	})
	ui.Column(c).Gap(8).Children(func() {
		for i, r := range rep.Rows {
			t := map[core.Status]tone{
				core.StatusInstalled: toneGreen, core.StatusUninstalled: toneGreen, core.StatusFailed: toneRed,
				core.StatusSkipped: toneAmber, core.StatusRolledBack: toneAmber,
			}[r.Status]
			ui.Row(c.Key(fmt.Sprintf("res-%d", i))).Gap(12).Children(func() {
				ui.Row(c).Width(96).Justify(ui.Center).Children(func() { pill(c, strings.ToUpper(string(r.Status[:1]))+string(r.Status[1:]), t) })
				mono(ui.Text(c, r.Folder), 13).SingleLine()
				ui.Text(c, r.Detail).FontSize(13).TextColor(pal.muted).Grow(1).MinWidth(0).MaxLines(2)
			})
			if a.dock.showAll {
				for _, l := range r.Lines {
					mono(ui.Text(c, "    "+l), 12).TextColor(pal.muted)
				}
			}
		}
		if hasLines(rep) {
			if button(c, "details", ternary(a.dock.showAll, "Hide details", "Show details"), nil, btnGhost, false, "") {
				a.dock.showAll = !a.dock.showAll
			}
		}
	})
}

func hasLines(r *core.Report) bool {
	for _, row := range r.Rows {
		if len(row.Lines) > 0 {
			return true
		}
	}
	return false
}

// dockIdle is what applying will do, and the buttons for it.
func (a *App) dockIdle(c *ui.Context, e core.GameEntry, s *core.Session) {
	installed := len(core.InstalledGroups(e)) > 0
	_, unmanaged := core.SplitTargets(e)
	busy := a.dock.running

	var title, head string
	var warn bool
	switch {
	case s == nil:
		title = "Nothing to apply"
		head = ternary(len(unmanaged) > 0, "ReShade is here, but YARM does not track it yet", "This game cannot be changed")
	case !s.Dirty():
		title, head = "Installed", "Up to date"
		if s.HasForeign() {
			warn = true
			head = "Up to date · another file holds " + filepath.Base(firstForeign(s))
		}
	default:
		title = ternary(s.Installed(), "Changes not applied yet", "Ready to install")
		head = s.Headline() + " · " + s.DownloadNote()
	}

	ui.Row(c).Gap(16).Children(func() {
		if warn {
			icon(c, icons.warn, 20, pal.amber)
		}
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			lbl(c, strings.ToUpper(title))
			display(ui.Text(c, head), 17).Margin(2, 0, 0).SingleLine().LetterSpacing(0.3)
		})
		ui.Row(c).Gap(8).Children(func() {
			if installed {
				if button(c, "uninstall", "Uninstall", icons.trash, btnDanger, busy, "Remove what YARM installed in this game") {
					a.uninstalling = true
				}
			}
			if len(unmanaged) > 0 {
				if button(c, "adopt", "Adopt existing install", nil, btnNormal, busy, "Let YARM track the ReShade already in this game; nothing on disk changes") {
					a.adopting = true
				}
			}
			if s == nil {
				return
			}
			if s.Installed() && s.Dirty() {
				if button(c, "discard", "Discard changes", nil, btnGhost, busy, "") {
					a.sess, a.sessGame = nil, ""
				}
			}
			if s.Dirty() {
				label := ternary(s.Installed(), "Apply changes", "Install")
				if button(c, "apply", label, icons.download, btnPrimary, busy || !a.canApply(s), "Ctrl+Enter") {
					a.apply(e, s)
				}
			}
		})
	})
}

func firstForeign(s *core.Session) string {
	for _, c := range s.Conflicts() {
		if c.Kind == install.ConflictForeign {
			return c.Path
		}
	}
	return ""
}

// canApply is false while something would stop the request from being built.
func (a *App) canApply(s *core.Session) bool {
	_, ok := s.Ops()
	return ok
}

// apply runs what the session says, in the background, with progress.
func (a *App) apply(e core.GameEntry, s *core.Session) {
	if a.dock.running {
		return
	}
	// Something that is not yarm's sits where ReShade goes: that is a decision
	// to see, not one to make by default, so the Review tab comes first.
	if s.HasForeign() && a.tab != tabReview {
		a.tab = tabReview
		a.toast("Another file is in the way. Check what happens to it, then apply.")
		return
	}
	ops, ok := s.Ops()
	if !ok {
		return
	}
	a.runOps(e, ops, "Applying to "+e.Name)
}

// runOps is shared by apply and uninstall: one apply at a time.
func (a *App) runOps(e core.GameEntry, ops []core.Op, title string) {
	ctx, cancel := context.WithCancel(context.Background())
	a.dock = dock{running: true, cancel: cancel, title: title}
	inst, un := a.deps.Installer, a.deps.Uninstaller

	var outcomes []core.Outcome
	a.bg(func() {
		outcomes = core.RunOps(ctx, inst, un, ops, func(u core.ProgressUpdate) {
			a.onMain(func() {
				a.dock.step, a.dock.done, a.dock.total = u.Label, u.Done, u.Total
				a.invalidate()
			})
		})
	}, func() {
		canceled := ctx.Err() != nil
		cancel()
		rep := core.NewReport(outcomes, canceled)
		for _, r := range rep.Rows {
			if r.Status == core.StatusFailed {
				slog.Warn("apply failed", "game", e.ID, "folder", r.Folder, "detail", r.Detail)
			}
		}
		a.dock = dock{report: &rep}
		a.sess, a.sessGame = nil, ""
	})
}

// finishApply closes the result and reads the game again, since what is on
// disk changed.
func (a *App) finishApply() {
	a.dock = dock{}
	a.rescan(false)
}

// uninstallDialog confirms before removing, naming the files.
func (a *App) uninstallDialog(c *ui.Context, e core.GameEntry) {
	modal(c, &a.uninstalling, func() {
		groups := core.InstalledGroups(e)
		ui.Column(c).Width(440).Gap(16).Children(func() {
			display(ui.Text(c, "UNINSTALL FROM "+strings.ToUpper(e.Name)+"?"), 24)
			ui.Text(c, core.UninstallDetail(a.deps.StateDir, e.ID, groups)).FontSize(14)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if button(c, "uninstall-cancel", "Cancel", nil, btnGhost, false, "") {
					a.uninstalling = false
				}
				if button(c, "uninstall-go", "Uninstall", icons.trash, btnDanger, false, "") {
					a.uninstalling = false
					a.runOps(e, core.UninstallOps(e, groups), "Uninstalling from "+e.Name)
				}
			})
		})
	})
}

// adoptDialog confirms before recording an untracked ReShade as ours.
func (a *App) adoptDialog(c *ui.Context, e core.GameEntry) {
	modal(c, &a.adopting, func() {
		_, unmanaged := core.SplitTargets(e)
		ui.Column(c).Width(440).Gap(16).Children(func() {
			display(ui.Text(c, "ADOPT THE EXISTING RESHADE?"), 24)
			ui.Text(c, core.AdoptDetail).FontSize(14)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if button(c, "adopt-cancel", "Cancel", nil, btnGhost, false, "") {
					a.adopting = false
				}
				if button(c, "adopt-go", "Adopt", nil, btnPrimary, false, "") {
					a.adopting = false
					a.adopt(e, unmanaged)
				}
			})
		})
	})
}

func (a *App) adopt(e core.GameEntry, targets []core.FolderGroup) {
	var n int
	var err error
	deps := a.deps
	a.bg(func() { n, err = core.AdoptAll(deps, e, targets) }, func() {
		if err != nil {
			a.toast("Couldn't adopt: " + core.FriendlyError(err))
			return
		}
		a.toast(fmt.Sprintf("YARM now tracks ReShade in %s. Nothing on disk changed.", plural(n, "folder")))
		a.rescan(false)
	})
}
