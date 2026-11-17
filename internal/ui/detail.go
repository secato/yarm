package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/secato/yarm/internal/core"
	"github.com/secato/yarm/internal/game"
	"github.com/secato/yarm/internal/install"
)

// The tabs of a game page, in the order the design shows them.
const (
	tabFolders = iota
	tabReShade
	tabShaders
	tabAddons
	tabRenoDX
	tabReview
	tabCount
)

var tabNames = [tabCount]string{"Folders", "ReShade", "Shaders", "Add-ons", "RenoDX", "Review"}

// main is the right-hand pane: whatever the sidebar selected.
func (a *App) mainPane(c *ui.Context) {
	ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(func() {
		switch {
		case a.view == viewResources:
			a.resourcesPane(c)
		case a.view == viewCustom:
			a.customPane(c)
		case len(a.games) == 0:
			a.emptyPane(c)
		default:
			if e, ok := a.entry(a.selected); ok {
				a.gamePage(c, e)
			} else {
				a.emptyPane(c)
			}
		}
	})
}

// emptyPane is the first-run and nothing-found state: the loading bar while
// looking, then what to do when there is nothing.
func (a *App) emptyPane(c *ui.Context) {
	ui.Column(c).Fill().Padding(28).Gap(20).Children(func() {
		if a.offline != "" {
			a.offlineBanner(c)
		}
		ui.Column(c).Grow(1).Center().Gap(14).Children(func() {
			ui.Box(c).Width(84).Height(84).Radius(22).Gradient(ui.Hex("#A78BFA"), ui.Hex("#6D28D9"), 135).Center().
				Shadow(0, 0, 40, 0, pal.accentGlow).Margin(0, ui.Auto).Children(func() {
				icon(c, icons.logo, 44, ui.Hex("#FFFFFF"))
			})
			if a.gamesLoad == stateLoading {
				display(ui.Text(c, "LOOKING FOR YOUR GAMES…"), 34).TextAlign(ui.Center)
				ui.Text(c, "Checking Steam, then GOG, Epic and Battle.net. This takes a few seconds the first time.").
					TextColor(pal.muted).MaxWidth(420).TextAlign(ui.Center).Margin(0, ui.Auto)
				ui.Progress(c, -1).Width(320).Margin(8, ui.Auto, 0).Label("Looking for games")
				return
			}
			title, note := "NO GAMES FOUND", "YARM looked in Steam, GOG, Epic and Battle.net and found nothing it can install ReShade into. Add the folder of a game you already have."
			if a.gamesLoad == stateFailed {
				title, note = "COULDN'T READ YOUR GAMES", a.gamesErr
			}
			display(ui.Text(c, title), 34).TextAlign(ui.Center)
			ui.Text(c, note).TextColor(pal.muted).MaxWidth(420).TextAlign(ui.Center).Margin(0, ui.Auto)
			ui.Row(c).Gap(10).Margin(6, ui.Auto, 0).Children(func() {
				if button(c, "empty-add", "Add a game folder", icons.plus, btnPrimary, false, "") {
					a.openAddGame()
				}
				if button(c, "empty-settings", "Check Settings", icons.sliders, btnNormal, false, "") {
					a.openSettings()
				}
			})
			ui.Row(c).Gap(6).Margin(8, ui.Auto, 0).Children(func() {
				ui.Text(c, "Shortcuts:").FontSize(12).TextColor(pal.muted)
				kbd(c, "Ctrl N")
				ui.Text(c, "add game ·").FontSize(12).TextColor(pal.muted)
				kbd(c, "Ctrl R")
				ui.Text(c, "rescan").FontSize(12).TextColor(pal.muted)
			})
		})
	})
}

func kbd(c *ui.Context, s string) {
	ui.Row(c).Padding(1, 7).Radius(6).Background(pal.surface).Border(1, pal.lineStrong).Children(func() {
		mono(ui.Text(c, s), 12).TextColor(pal.muted)
	})
}

// offlineBanner says the catalog is not live: what is on disk is shown.
func (a *App) offlineBanner(c *ui.Context) {
	card(c, "offline", false, toneAmber).Gap(12).Children(func() {
		icon(c, icons.warn, 18, pal.amber)
		ui.Column(c).Grow(1).Children(func() {
			ui.Row(c).Gap(4).Children(func() {
				ui.Text(c, "You're offline.").Bold().FontSize(13)
				ui.Text(c, "Showing what's already downloaded. Shader lists refresh when you reconnect.").FontSize(13)
			})
			if a.offlineOpen {
				mono(ui.Text(c, a.offline), 12).TextColor(pal.muted)
			}
		})
		if button(c, "offline-details", ternary(a.offlineOpen, "Hide details", "Show details"), nil, btnGhost, false, "") {
			a.offlineOpen = !a.offlineOpen
		}
	})
}

// gamePage is a game: its header, the tabs, the current tab and the dock.
func (a *App) gamePage(c *ui.Context, e core.GameEntry) {
	a.header(c, e)
	sess := a.session()

	if sess == nil {
		ui.Column(c).Grow(1).Padding(22, 28).Gap(12).Children(func() {
			a.cannotEdit(c, e)
		})
		a.dockView(c, e, nil)
		return
	}

	a.tabsView(c, sess)
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Padding(22, 28).Gap(12).Children(func() {
			if a.offline != "" {
				a.offlineBanner(c)
			}
			switch a.tab {
			case tabFolders:
				a.foldersTab(c, sess)
			case tabReShade:
				a.reshadeTab(c, sess)
			case tabShaders:
				a.shadersTab(c, sess)
			case tabAddons:
				a.addonsTab(c, sess)
			case tabRenoDX:
				a.renodxTab(c, sess)
			case tabReview:
				a.reviewTab(c, sess)
			}
		})
	})
	a.dockView(c, e, sess)
}

// cannotEdit says why a game has no tabs: a native build, an unreadable
// folder, a catalog that did not load, or only an unmanaged ReShade to adopt.
func (a *App) cannotEdit(c *ui.Context, e core.GameEntry) {
	switch {
	case e.NativeBuild:
		notice(c, "native", toneCyan, icons.info, "This is a native Linux build",
			"It ships no Windows executable, so ReShade cannot apply to it.")
	case e.ScanErr != nil:
		notice(c, "scanerr", toneRed, icons.warn, "Can't read this game's folder", core.FriendlyError(e.ScanErr))
	case a.dataLoad == stateLoading || a.dataLoad == stateIdle:
		ui.Row(c).Gap(10).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "Loading the catalog…").TextColor(pal.muted)
		})
	case a.dataLoad == stateFailed:
		notice(c, "nodata", toneAmber, icons.warn, "Can't load the shader catalog",
			a.dataErr+" Connect to the internet once so YARM can download it, then rescan with Ctrl+R.")
	default:
		if installable, unmanaged := core.SplitTargets(e); len(installable) == 0 && len(unmanaged) > 0 {
			notice(c, "unmanaged", toneAmber, icons.warn, "ReShade is already here",
				"YARM did not put it there. Adopt it to let YARM update or uninstall it; nothing on disk changes.")
		} else {
			notice(c, "noexe", toneAmber, icons.warn, "No game executable found",
				"YARM found no Windows executable to install ReShade next to.")
		}
	}
}

// header is the game's name, where it comes from, what it is and where it is.
func (a *App) header(c *ui.Context, e core.GameEntry) {
	ui.Column(c).Padding(22, 28, 18).Gap(0).BorderWidth(0, 0, 1, 0).BorderColor(pal.line).
		Gradient(washColor(e.Name), pal.ground, 120).Children(func() {
		mono(ui.Text(c, strings.ToUpper(providerLabel(e.Provider))+" · "+e.ID), 12).TextColor(pal.faint).LetterSpacing(1)
		ui.Row(c).Gap(16).Margin(6, 0, 0).Children(func() {
			display(ui.Text(c, strings.ToUpper(e.Name)), 40).Grow(1).SingleLine()
			a.installedPill(c, e)
		})
		ui.Row(c).Gap(8).Margin(12, 0, 0).Wrap().Children(func() {
			a.headerPills(c, e)
			mono(ui.Text(c, e.Root), 12).TextColor(pal.muted).Margin(0, 0, 0, 8).SingleLine()
			if button(c, "openfolder", "Open folder", icons.folder, btnGhost, false, "Show it in the file manager") {
				mygo.Shell.ShowItemInFolder(e.Root)
			}
		})
	})
}

func (a *App) installedPill(c *ui.Context, e core.GameEntry) {
	for _, g := range e.Groups {
		if in := g.Installed; in != nil {
			ver := in.ReShade.Version
			if ver == "" || strings.Contains(ver, "unknown") {
				pillIcon(c, icons.check, "Installed · adopted", toneGreen)
			} else {
				pillIcon(c, icons.check, "Installed · ReShade "+ver, toneGreen)
			}
			return
		}
	}
	if e.HasUnmanaged() {
		pill(c, "ReShade found, not tracked", toneAmber)
	}
}

func (a *App) headerPills(c *ui.Context, e core.GameEntry) {
	if e.NativeBuild {
		pill(c, "Native Linux build", toneCyan)
		return
	}
	exe := core.Executable{}
	if sess := a.session(); sess != nil {
		exe = sess.Exe
	} else if px := e.PlayableExes(); len(px) > 0 {
		exe = px[0]
	}
	if exe.Arch == game.ArchX64 || exe.Arch == game.ArchX86 {
		pill(c, archLabel(exe.Arch), toneNeutral)
	}
	if exe.Path != "" {
		pill(c, core.APILabel(exe.API), toneNeutral)
		pill(c, "Windows build under Proton", toneCyan)
	}
}

// tabsView is the tab strip, with a count where something is chosen.
func (a *App) tabsView(c *ui.Context, s *core.Session) {
	counts := [tabCount]int{
		tabFolders: len(s.Folders.SelectedGroups()),
		tabShaders: len(s.SelectedPackages()) + countPrefix(s.SelectedCustom(), "custom:shaders:"),
		tabAddons:  len(s.SelectedAddons()) + countPrefix(s.SelectedCustom(), "custom:addons:"),
	}
	if s.RenoDXChoice() != "" {
		counts[tabRenoDX] = 1
	}
	tabs := ui.TabsBase(c.Key("tabs"), &a.tab, tabCount)
	tabs.List.Gap(4).Padding(0, 28).BorderWidth(0, 0, 1, 0).BorderColor(pal.line).Label("Game sections")
	tabs.List.Children(func() {
		for i, name := range tabNames {
			on := i == a.tab
			tab := tabs.Tab(i).Padding(0, 14).Height(48).Gap(8).Margin(0, 0, -1, 0)
			tab.BorderWidth(0, 0, 2, 0).BorderColor(ternary(on, pal.accent, ui.Transparent))
			tab.Children(func() {
				display(ui.Text(c, name), 16).TextColor(ternary(on, pal.text, pal.faint)).LetterSpacing(0.6)
				if n := counts[i]; n > 0 {
					badge(c, n)
				}
			})
		}
	})
}

func countPrefix(ids []string, prefix string) int {
	n := 0
	for _, id := range ids {
		if strings.HasPrefix(id, prefix) {
			n++
		}
	}
	return n
}

// ---- Folders ----

func (a *App) foldersTab(c *ui.Context, s *core.Session) {
	lbl(c, "WHERE TO INSTALL")
	groups := core.SortedDetailGroups(s.Folders.Groups())
	for _, g := range groups {
		checked := s.Folders.Checked(g.Dir)
		exe := s.Folders.ExeFor(g)
		row := ui.ButtonBase(c.Key("folder-"+g.Dir)).Padding(14, 16).Gap(14).Radius(12).Label(core.DetailFolderLabel(s.Entry, g.Dir)).
			Background(ternary(checked, pal.cardOn, pal.card)).Border(1, ternary(checked, pal.lineStrong, pal.line))
		if row.Hovered() && !checked {
			row.Background(pal.surfaceHover)
		}
		row.Children(func() {
			checkbox(c, checked)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				mono(ui.Text(c, core.DetailFolderLabel(s.Entry, g.Dir)), 13).SingleLine()
				ui.Text(c, fmt.Sprintf("%s · %s · %s", filepath.Base(exe.Path), archLabel(exe.Arch), core.APILabel(exe.API))).
					FontSize(13).TextColor(pal.muted).SingleLine()
				lines, hidden := core.FolderExes(g, exe.Path)
				if len(lines) > 1 {
					for _, l := range lines {
						ui.Text(c, ternary(l.Chosen, "● ", "○ ")+l.Name+" · "+l.Detail).FontSize(12).
							TextColor(ternary(l.Chosen, pal.accent, pal.faint)).SingleLine()
					}
					if hidden > 0 {
						ui.Text(c, fmt.Sprintf("+%d more", hidden)).FontSize(12).TextColor(pal.faint)
					}
				}
			})
			if g.NeedsExeChoice() {
				if button(c, "cycle-"+g.Dir, "Switch executable", nil, btnGhost, false, "Use the other executable of this folder") {
					s.CycleExe(g.Dir)
				}
			}
			a.folderPill(c, g)
		})
		if row.Clicked() {
			s.ToggleFolder(g.Dir)
		}
	}
	notice(c, "foldershint", toneCyan, icons.info, "",
		"A game can have several folders. Pick the one that holds the executable the game really runs.")
}

func archLabel(a game.Arch) string {
	switch a {
	case game.ArchX64:
		return "64-bit"
	case game.ArchX86:
		return "32-bit"
	}
	return "unknown arch"
}

func (a *App) folderPill(c *ui.Context, g core.FolderGroup) {
	switch {
	case g.Installed != nil:
		pill(c, "ReShade "+g.Installed.ReShade.Version+" installed", toneGreen)
	case g.Unmanaged != nil:
		pill(c, "Untracked ReShade", toneAmber)
	default:
		pill(c, "Not installed", toneNeutral)
	}
}

// ---- ReShade ----

func (a *App) reshadeTab(c *ui.Context, s *core.Session) {
	ui.Row(c).Gap(14).Children(func() {
		ui.Box(c).Width(120).Children(func() { lbl(c, "VERSION") })
		labels := make([]string, 0, len(s.Versions()))
		byLabel := map[string]string{}
		cur := ""
		for _, v := range s.Versions() {
			l := v.Version
			if v.Latest {
				l += " (latest)"
			}
			labels = append(labels, l)
			byLabel[l] = v.Version
			if v.Version == s.Version {
				cur = l
			}
		}
		if ui.Select(c.Key("version"), &cur, labels).Label("ReShade version").Width(220).Changed() {
			s.SetVersion(byLabel[cur])
		}
		ui.Text(c, "Downloaded once, shared by every game.").FontSize(13).TextColor(pal.muted)
	})

	lbl(c, "FLAVOR")
	ui.Grid(c).Columns(2).Gap(14).Children(func() {
		a.flavorCard(c, s, install.FlavorNormal, "Normal", "Shaders only. The smallest and safest choice for most games.")
		a.flavorCard(c, s, install.FlavorAddon, "Add-on support", "Also loads add-ons such as RenoDX. Needed for HDR mods. Anti-cheat can detect it.")
	})

	if s.Flavor.Addon() {
		notice(c, "anticheat", toneAmber, icons.warn, "", strings.TrimPrefix(core.AnticheatWarning, "⚠ "))
	}

	ui.Grid(c).Columns(2).Gap(14).Children(func() {
		card(c, "dllcard", false, toneNeutral).Gap(10).Children(func() {
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				lbl(c, "PROXY DLL")
				ui.Row(c).Gap(10).Children(func() {
					mono(ui.Text(c, s.DLL), 15).Grow(1)
					auto := s.DLL == s.RecommendedDLL() || s.RecommendedDLL() == ""
					if i := segmented(c, "dllmode", []string{"Auto", "Override"}, ternary(a.dllOverride && !auto || a.dllOverride, 1, 0), false); i >= 0 {
						a.dllOverride = i == 1
						if i == 0 {
							if rec := s.RecommendedDLL(); rec != "" {
								s.SetDLL(rec)
							} else {
								s.SetDLL("dxgi.dll")
							}
						}
					}
				})
				if a.dllOverride {
					labels := make([]string, 0, 6)
					byLabel := map[string]string{}
					cur := ""
					for _, o := range core.DLLOptions() {
						l := o.Name + " · " + o.For
						labels = append(labels, l)
						byLabel[l] = o.Name
						if o.Name == s.DLL {
							cur = l
						}
					}
					if ui.Select(c.Key("dll"), &cur, labels).Label("Proxy DLL").Changed() {
						s.SetDLL(byLabel[cur])
					}
					ui.Text(c, "Change it only if another tool already uses that name.").FontSize(12).TextColor(pal.muted)
				}
			})
		})
		card(c, "apicard", false, toneNeutral).Children(func() {
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				lbl(c, "GRAPHICS API")
				ui.Row(c).Gap(10).Children(func() {
					mono(ui.Text(c, core.APILabel(s.Exe.API)), 15).Grow(1)
					if s.Exe.API == game.APIUnknown || s.Exe.API == "" {
						pill(c, "Not detected", toneAmber)
					} else {
						pill(c, "Detected", toneCyan)
					}
				})
			})
		})
	})
}

func (a *App) flavorCard(c *ui.Context, s *core.Session, f install.Flavor, name, text string) {
	on := s.Flavor == f
	b := ui.ButtonBase(c.Key("flavor-"+string(f))).Padding(14, 16).Radius(12).MinHeight(112).Label(name).
		Background(ternary(on, pal.cardOn, pal.card)).Border(1, ternary(on, pal.accent, pal.line))
	if on {
		b.Shadow(0, 0, 18, 0, pal.accentGlow)
	} else if b.Hovered() {
		b.Background(pal.surfaceHover)
	}
	b.Children(func() {
		ui.Column(c).Grow(1).Gap(6).Children(func() {
			ui.Row(c).Children(func() {
				display(ui.Text(c, name), 20).Grow(1)
				if on {
					pill(c, "Selected", toneAccent)
				}
			})
			ui.Text(c, text).FontSize(13).TextColor(pal.muted)
		})
	})
	if b.Clicked() {
		s.SetFlavor(f)
	}
}

// ---- Shaders and add-ons ----

func (a *App) shadersTab(c *ui.Context, s *core.Session) {
	rows := s.PackageRows(a.showAll)
	a.listHeader(c, "shaders", "Search shader packs", &a.shaderQ, s.PackageTotal(), &a.showAll)
	a.itemGrid(c, s, filterRows(rows, a.shaderQ), false, func(id string) {
		if pulled := s.TogglePackage(id); len(pulled) > 0 {
			a.toast(strings.Join(pulled, ", ") + ternary(len(pulled) == 1, " was added because ", " were added because ") + s.ItemNames()[id] + " needs it.")
		}
	})
}

func (a *App) addonsTab(c *ui.Context, s *core.Session) {
	off := !s.Flavor.Addon()
	if off {
		notice(c, "addonflavor", toneCyan, icons.info, "Add-ons need the Add-on support flavor",
			"Switch the flavor on the ReShade tab to choose add-ons.")
		if button(c, "useaddonflavor", "Use Add-on support", nil, btnNormal, false, "") {
			s.SetFlavor(install.FlavorAddon)
		}
	} else {
		lbl(c, "ADD-ONS NEED THE ADD-ON SUPPORT FLAVOR")
	}
	rows := s.AddonRows(a.addonAll)
	a.listHeader(c, "addons", "Search add-ons", &a.addonQ, s.AddonTotal(), &a.addonAll)
	a.itemGrid(c, s, filterRows(rows, a.addonQ), off, func(id string) {
		if pulled := s.ToggleAddon(id); len(pulled) > 0 {
			a.toast(strings.Join(pulled, ", ") + " was added because " + s.ItemNames()[id] + " needs it.")
		}
	})
}

func filterRows(rows []core.Row, q string) []core.Row {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return rows
	}
	var out []core.Row
	for _, r := range rows {
		if r.Header || strings.Contains(strings.ToLower(r.Name), q) || strings.Contains(strings.ToLower(r.Description), q) {
			out = append(out, r)
		}
	}
	return out
}

// listHeader is the search box and the Curated / All switch above a list.
func (a *App) listHeader(c *ui.Context, key, placeholder string, q *string, total int, all *bool) {
	ui.Row(c).Gap(14).Children(func() {
		ui.SearchField(c.Key("q-"+key), q).Placeholder(placeholder).Label(placeholder).MaxWidth(380).Grow(1)
		ui.Spacer(c)
		cur := 0
		if *all {
			cur = 1
		}
		if i := segmented(c, "all-"+key, []string{"Curated", fmt.Sprintf("All %d", total)}, cur, false); i >= 0 {
			*all = i == 1
		}
	})
}

// itemGrid is two columns of cards, each with a switch.
func (a *App) itemGrid(c *ui.Context, s *core.Session, rows []core.Row, off bool, toggle func(id string)) {
	if len(rows) == 0 {
		ui.Text(c, "Nothing matches.").TextColor(pal.muted)
		return
	}
	ui.Grid(c).Columns(2).Gap(14).Children(func() {
		for _, r := range rows {
			if r.Header {
				ui.Row(c.Key("hdr-"+r.Name)).ColumnSpan(-1).Padding(8, 0, 0).Children(func() { lbl(c, strings.ToUpper(r.Name)) })
				continue
			}
			a.itemCard(c, r, off, toggle)
		}
	})
}

func (a *App) itemCard(c *ui.Context, r core.Row, off bool, toggle func(id string)) {
	locked := r.Disabled || off || r.Required
	on := r.Selected
	cd := ui.Row(c.Key("item-"+r.ID)).Padding(14, 16).Gap(14).Radius(12).
		Background(ternary(on, pal.cardOn, pal.card)).Border(1, ternary(on, pal.lineStrong, pal.line))
	cd.Children(func() {
		v := on
		sw := ui.Switch(c.Key("sw-"+r.ID), &v).Label(r.Name).Disabled(locked)
		if sw.Changed() && !locked {
			toggle(r.ID)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			display(ui.Text(c, r.Name), 18).TextColor(ternary(r.Disabled || off, pal.faint, pal.text)).SingleLine()
			desc := r.Description
			if r.Disabled && r.DisabledNote != "" {
				desc = r.DisabledNote
			}
			if desc != "" {
				ui.Text(c, desc).FontSize(13).TextColor(pal.muted).MaxLines(2)
			}
			ui.Row(c).Gap(6).Margin(8, 0, 0).Wrap().Children(func() {
				switch {
				case r.Required:
					pill(c, "Required", toneCyan)
				case r.Note != "" && r.NoteWarn:
					pill(c, r.Note, toneAmber)
				case r.Note != "":
					pill(c, r.Note, toneCyan)
				}
				switch {
				case strings.HasPrefix(r.ID, "custom:"):
					pill(c, "Custom", toneAccent)
				case r.Disabled:
				case on && !r.Cached:
					pill(c, "Will install", toneAccent)
				case r.Cached:
					pill(c, "Cached", toneGreen)
				}
			})
		})
	})
}

// ---- RenoDX ----

func (a *App) renodxTab(c *ui.Context, s *core.Session) {
	if !s.HasRenoDX() {
		notice(c, "norenodx", toneAmber, icons.warn, "The RenoDX list is not available",
			"It comes from another project. Connect to the internet and rescan with Ctrl+R.")
		return
	}
	if !s.Flavor.Addon() {
		notice(c, "renoflavor", toneCyan, icons.info, "RenoDX needs the Add-on support flavor",
			"Switch the flavor on the ReShade tab to choose a RenoDX mod.")
		if button(c, "usereno-flavor", "Use Add-on support", nil, btnNormal, false, "") {
			s.SetFlavor(install.FlavorAddon)
		}
	}
	rows := s.RenoDXRows(a.renodxQ)
	_, hasMatch := s.RenoDXMatch()
	ui.Row(c).Gap(14).Children(func() {
		ui.SearchField(c.Key("q-reno"), &a.renodxQ).Placeholder("Search RenoDX mods").Label("Search RenoDX mods").MaxWidth(380).Grow(1)
	})
	lbl(c, ternary(hasMatch, "HDR MOD OFFERED FOR THIS GAME", "RENODX MODS"))
	shown := rows
	if len(shown) > 40 {
		shown = shown[:40]
	}
	for _, r := range shown {
		a.renoCard(c, s, r)
	}
	if len(rows) > len(shown) {
		ui.Text(c, fmt.Sprintf("Showing 40 of %d. Search to find the rest.", len(rows))).FontSize(13).TextColor(pal.muted)
	}
	notice(c, "hdrhint", toneCyan, icons.info, "",
		"Your display needs HDR turned on in the desktop settings for this mod to do anything.")
}

func (a *App) renoCard(c *ui.Context, s *core.Session, r core.Row) {
	locked := r.Disabled || !s.Flavor.Addon()
	cd := ui.Row(c.Key("reno-"+r.ID)).Padding(14, 16).Gap(14).Radius(12).
		Background(ternary(r.Selected, pal.cardOn, pal.card)).Border(1, ternary(r.Selected, pal.accent, pal.line))
	if r.Selected {
		cd.Shadow(0, 0, 18, 0, pal.accentGlow)
	}
	cd.Children(func() {
		v := r.Selected
		if ui.Switch(c.Key("rsw-"+r.ID), &v).Label(r.Name).Disabled(locked).Changed() && !locked {
			s.ChooseRenoDX(ternary(r.Selected, "", r.ID))
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			display(ui.Text(c, "RenoDX · "+r.Name), 19).TextColor(ternary(locked, pal.faint, pal.text)).SingleLine()
			desc := r.Description
			if r.Disabled {
				desc = r.DisabledNote
			}
			if desc != "" {
				ui.Text(c, desc).FontSize(13).TextColor(pal.muted).MaxLines(2)
			}
			ui.Row(c).Gap(6).Margin(8, 0, 0).Wrap().Children(func() {
				if r.Note != "" {
					pill(c, r.Note, ternary(r.NoteWarn, toneAmber, toneCyan))
				}
				pill(c, "Needs Add-on support", toneCyan)
				if r.Cached {
					pill(c, "Cached", toneGreen)
				} else if r.Selected {
					pill(c, "Will install", toneAccent)
				}
			})
		})
	})
}

// ---- Review ----

type reviewRow struct {
	kind string
	tone tone
	text string
}

func (a *App) reviewTab(c *ui.Context, s *core.Session) {
	conflicts := s.Conflicts()
	unmet := s.Unmet()
	for _, u := range unmet {
		notice(c, "unmet-"+u, toneAmber, icons.warn, "Worth a look first", u)
	}
	for _, cf := range conflicts {
		if cf.Kind == install.ConflictForeign {
			notice(c, "foreign-"+cf.Path, toneAmber, icons.warn,
				"Another file holds "+filepath.Base(cf.Path),
				ternary(s.Overwrite && s.Backup,
					"YARM backs it up and puts ReShade there. Uninstalling brings it back.",
					ternary(s.Overwrite, "YARM replaces it without a backup.", "YARM keeps it, so ReShade will not load.")))
		}
	}

	if s.HasForeign() {
		ui.Row(c).Gap(24).Children(func() {
			ov, bk := s.Overwrite, s.Backup
			ui.Row(c).Gap(10).Children(func() {
				if ui.Switch(c.Key("overwrite"), &ov).Label("Replace files in the way").Changed() {
					s.SetOverwrite(ov)
				}
				ui.Text(c, "Replace files in the way").FontSize(13)
			})
			ui.Row(c).Gap(10).Children(func() {
				if ui.Switch(c.Key("backup"), &bk).Label("Back up what is replaced").Changed() {
					s.SetBackup(bk)
				}
				ui.Text(c, "Back up what is replaced").FontSize(13)
			})
		})
	}

	lbl(c, "WHAT WILL HAPPEN")
	rows := a.reviewRows(s, conflicts)
	if len(rows) == 0 {
		ui.Text(c, "Nothing. What is installed already matches your choices.").TextColor(pal.muted)
	}
	ui.Column(c).Children(func() {
		for i, r := range rows {
			ui.Row(c.Key(fmt.Sprintf("rv-%d", i))).Gap(12).Padding(7, 0).BorderWidth(1, 0, 0, 0).BorderColor(pal.line).Children(func() {
				ui.Row(c).Width(92).Justify(ui.Center).Children(func() { pill(c, r.kind, r.tone) })
				mono(ui.Text(c, r.text), 13)
			})
		}
	})
	ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
		pill(c, s.DownloadNote(), toneNeutral)
	})
}

func (a *App) reviewRows(s *core.Session, conflicts []install.Conflict) []reviewRow {
	var rows []reviewRow
	if m := s.Missing(); len(m) > 0 {
		rows = append(rows, reviewRow{"Download", toneAccent, strings.Join(m, ", ")})
	}
	if s.Installed() {
		// An edit shows what differs, not everything: the files that stay as
		// they are are the plan's business, and listing them as "written"
		// would say something that is not going to happen.
		for _, ch := range s.Changes() {
			switch ch.Kind {
			case "add":
				rows = append(rows, reviewRow{"Add", toneGreen, ch.Text})
			case "remove":
				rows = append(rows, reviewRow{"Remove", toneRed, ch.Text})
			default:
				rows = append(rows, reviewRow{"Change", toneAmber, ch.Text})
			}
		}
	} else {
		for _, g := range s.Folders.SelectedGroups() {
			rows = append(rows, reviewRow{
				"Write", toneGreen,
				fmt.Sprintf("ReShade %s (%s) as %s in %s", s.Version, s.Flavor, s.DLL, core.DetailFolderLabel(s.Entry, g.Dir)),
			})
		}
		names := s.ItemNames()
		if p := s.SelectedPackages(); len(p) > 0 {
			rows = append(rows, reviewRow{"Write", toneGreen, "reshade-shaders/: " + joinNames(p, names)})
		}
		if ad := s.SelectedAddons(); len(ad) > 0 {
			rows = append(rows, reviewRow{"Write", toneGreen, "add-ons: " + joinNames(ad, names)})
		}
		if t := s.RenoDXTitle(); t != "" && s.Flavor.Addon() {
			rows = append(rows, reviewRow{"Write", toneGreen, "RenoDX " + t})
		}
	}
	for _, cf := range conflicts {
		switch cf.Kind {
		case install.ConflictConfig:
			rows = append(rows, reviewRow{"Keep", toneNeutral, core.DescribeConflict(cf, s.Overwrite, s.Backup)})
		case install.ConflictForeign:
			rows = append(rows, reviewRow{
				ternary(s.Overwrite, "Replace", "Keep"), ternary(s.Overwrite, toneAmber, toneNeutral),
				core.DescribeConflict(cf, s.Overwrite, s.Backup),
			})
		default:
			rows = append(rows, reviewRow{"Replace", toneAmber, core.DescribeConflict(cf, s.Overwrite, s.Backup)})
		}
	}
	for _, g := range s.Folders.Removed() {
		rows = append(rows, reviewRow{"Remove", toneRed, "ReShade from " + core.DetailFolderLabel(s.Entry, g.Dir)})
	}
	return rows
}

func joinNames(ids []string, names map[string]string) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = names[id]
		if out[i] == "" {
			out[i] = id
		}
	}
	return strings.Join(out, ", ")
}
