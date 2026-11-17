package ui

import (
	"log/slog"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/secato/yarm/internal/config"
)

// settingsView is the state of the Settings dialog.
type settingsView struct {
	open bool
	ttl  string // the RenoDX list's hours, as typed
}

func (a *App) openSettings() {
	a.setting.open = true
	a.setting.ttl = strconv.Itoa(a.cfg.RenodxTTLHours)
}

// pickFolder asks for a folder with the system's dialog. Blocking: call it
// off the UI thread.
func pickFolder(title string) string {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: title, Directory: true})
	if err != nil || len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// saveConfig validates and writes a configuration, and takes it in. Every
// change applies at once; providers and library paths also rescan. A
// configuration that does not validate or cannot be written is dropped with
// a message, and what is on disk stays what the window shows.
func (a *App) saveConfig(next config.Config, rescan bool) bool {
	if err := next.Validate(); err != nil {
		a.toast(err.Error())
		return false
	}
	if a.deps.ConfigDir != "" {
		if err := config.Save(a.deps.ConfigDir, next); err != nil {
			slog.Warn("could not save settings", "error", err)
			a.toast("Couldn't save the settings: " + err.Error())
			return false
		}
	}
	a.cfg = next
	if rescan {
		if a.opts.NewLoader != nil {
			a.loader = a.opts.NewLoader(next)
		}
		a.rescan(false)
	}
	return true
}

func (a *App) settingsDialog(c *ui.Context) {
	modal(c, &a.setting.open, func() {
		ui.Column(c).Width(600).MaxHeight(680).Gap(16).Children(func() {
			ui.Row(c).Children(func() {
				display(ui.Text(c, "SETTINGS"), 30).Grow(1)
				if iconButton(c, "settings-close", icons.close, "Close", "Close (Esc)") {
					a.setting.open = false
				}
			})
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(16).Children(func() { a.settingsBody(c) })
			})
		})
	})
}

func (a *App) settingsBody(c *ui.Context) {
	cfg := a.cfg
	count := map[string]int{}
	for _, g := range a.games {
		count[g.Provider]++
	}

	lbl(c, "PROVIDERS")
	ui.Column(c).Gap(8).Children(func() {
		for _, p := range []struct {
			id, name, via string
			on            *bool
		}{
			{"steam", "Steam", "", &cfg.Steam.Enabled},
			{"gog", "GOG", "via Heroic or Lutris", &cfg.GOG.Enabled},
			{"epic", "Epic", "via Heroic or Lutris", &cfg.Epic.Enabled},
			{"battlenet", "Battle.net", "via wine prefixes", &cfg.BattleNet.Enabled},
		} {
			on := *p.on
			card(c, "prov-"+p.id, false, toneNeutral).Children(func() {
				if ui.Switch(c.Key("provsw-"+p.id), p.on).Label("Search " + p.name).Changed() {
					if !a.saveConfig(cfg, true) {
						*p.on = on
					}
				}
				ui.Row(c).Grow(1).Gap(6).Children(func() {
					display(ui.Text(c, p.name), 17)
					if p.via != "" {
						ui.Text(c, p.via).FontSize(13).TextColor(pal.muted)
					}
				})
				switch {
				case !*p.on:
					pill(c, "Off", toneNeutral)
				case a.gamesLoad == stateLoading:
					pill(c, "Looking…", toneNeutral)
				default:
					pill(c, plural(count[p.id], "game"), toneGreen)
				}
			})
		}
	})

	lbl(c, "STEAM LIBRARY PATHS")
	ui.Column(c).Gap(8).Children(func() {
		for i, path := range cfg.Steam.ExtraLibraryPaths {
			ui.Row(c.Key("lib-" + path)).Gap(8).Children(func() {
				ui.Box(c).Grow(1).Padding(10, 12).Radius(10).Background(pal.code).Border(1, pal.line).Children(func() {
					mono(ui.Text(c, path), 12).SingleLine()
				})
				if iconButton(c, "lib-rm-"+path, icons.close, "Remove "+path, "Remove this path") {
					next := cfg
					next.Steam.ExtraLibraryPaths = append(append([]string(nil), cfg.Steam.ExtraLibraryPaths[:i]...), cfg.Steam.ExtraLibraryPaths[i+1:]...)
					a.saveConfig(next, true)
				}
			})
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, "Add a library the scan missed. Steam's own are found without it.").FontSize(13).TextColor(pal.muted).Grow(1)
			if button(c, "lib-add", "Add folder", icons.plus, btnNormal, false, "") {
				a.pickThen("Steam library folder", func(path string) {
					next := cfg
					next.Steam.ExtraLibraryPaths = append(append([]string(nil), cfg.Steam.ExtraLibraryPaths...), path)
					a.saveConfig(next, true)
				})
			}
		})
	})

	lbl(c, "DEFAULTS FOR NEW INSTALLS")
	ui.Row(c).Gap(14).Children(func() {
		cur := 0
		if cfg.Defaults.ReshadeFlavor == "addon" {
			cur = 1
		}
		if i := segmented(c, "defflavor", []string{"Normal", "Add-on support"}, cur, false); i >= 0 {
			next := cfg
			next.Defaults.ReshadeFlavor = []string{"normal", "addon"}[i]
			a.saveConfig(next, false)
		}
		ui.Text(c, "Starting packs: "+strings.Join(cfg.Defaults.Packages, ", ")).FontSize(13).TextColor(pal.muted)
	})

	lbl(c, "CONTENT REFRESH")
	ui.Row(c).Gap(14).Children(func() {
		ui.Row(c).Grow(1).Gap(10).Padding(0, 12).Height(40).Radius(10).Background(pal.surface).Border(1, pal.lineStrong).Children(func() {
			ui.Text(c, "RenoDX list, hours").FontSize(13).TextColor(pal.muted).Grow(1)
			if ui.TextInput(c.Key("ttl"), &a.setting.ttl).Width(70).TextAlign(ui.End).Label("RenoDX list, hours").Changed() {
				if n, err := strconv.Atoi(strings.TrimSpace(a.setting.ttl)); err == nil && n > 0 && n != cfg.RenodxTTLHours {
					next := cfg
					next.RenodxTTLHours = n
					a.saveConfig(next, false)
				}
			}
		})
	})

	lbl(c, "CACHE LOCATION")
	ui.Row(c).Gap(8).Children(func() {
		ui.Box(c).Grow(1).Padding(10, 12).Radius(10).Background(pal.code).Border(1, pal.line).Children(func() {
			dir := cfg.CacheDir
			if dir == "" && a.deps.Cache != nil {
				dir = a.deps.Cache.Root
			}
			mono(ui.Text(c, dir), 12).SingleLine()
		})
		if button(c, "cache-change", "Change…", nil, btnNormal, false, "") {
			a.pickThen("Cache folder", func(path string) {
				next := cfg
				next.CacheDir = path
				if a.saveConfig(next, false) {
					a.toast("The new cache location is used the next time YARM starts.")
				}
			})
		}
	})
	notice(c, "settings-note", toneCyan, icons.info, "",
		"Changes apply right away. A new cache location, and the RenoDX list's hours, need a restart.")
}

// pickThen opens the system's folder dialog off the UI thread and hands the
// choice back on it. Canceling does nothing.
func (a *App) pickThen(title string, then func(path string)) {
	var path string
	a.bg(func() { path = a.pick(title) }, func() {
		if path != "" {
			then(path)
		}
	})
}
