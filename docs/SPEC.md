# Spec and decisions

Settled with the owner in an interview. Treat as decided; change one only when the owner
asks, and edit this file in the same commit. Terms are defined in [GLOSSARY.md](../GLOSSARY.md).

## Direction (v2)
YARM stops being a terminal program and becomes a native desktop window, built with
[MyGo](https://github.com/egoist/mygo) like the owner's `slom` project (GPU drawn, no web
view, no cgo). The Bubble Tea TUI is removed, not kept beside it. There is still no CLI.

## Platforms
- **Linux only**, glibc, x86_64 and arm64. Windows and macOS host support is dropped:
  Windows already has other ReShade managers. v1.x releases stay available for those users.
- Games are still Windows executables running under Wine or Proton; only the machine YARM
  runs on changes. GOG, Epic and Battle.net are reached through Heroic and Lutris.
- Steam Deck is supported in Desktop Mode, best effort until tested on hardware. Game Mode
  is not a target.
- The binary is still built with `CGO_ENABLED=0`, but it is no longer self-contained: it
  needs the system's GPU and windowing libraries at run time. musl is not supported.

## Migration
Built on branch `v2-gui`, as one series (only step 1 is committed so far):
1. Windows and macOS support removed: the `*_windows.go` and `*_darwin.go` files, the goreleaser
   targets and zip, the Windows and macOS branches of `artifacts.TargetOS` (collapsed, since only
   Linux remains), and the docs and README.
2. The UI-agnostic logic moved out of `internal/app` into `internal/core` (pipeline,
   `RealInstaller`, wizard data, games data, curated, requires, deps, scan, friendly errors), and
   the wizard's state became `core.Session`.
3. `internal/ui` is the only package that imports MyGo; nothing imports it.
4. **The TUI was deleted while its logic was extracted**, not kept until the GUI had parity. The
   plan was to keep it beside the GUI; extracting the pure logic out of a 2.8k-line wizard in place
   was harder than writing the session fresh from the engine APIs, and a TUI that no longer compiled
   against `core` would have been dead weight. It remains in git history (`28d5522` and earlier).
   A latent bug went with it: custom content ids were sent in `Request.Packages`; they now go in
   `Request.Custom`.
5. One release, **v2.0.0**, carries the Windows removal and the GUI together. No interim release.
   The CHANGELOG and README say what changed and point Windows and terminal users at v1.x.

## Window
One shell, no stacked screens, no back button.
- **Sidebar**: filterable games list, Rescan, Add game. Footer: Resources, Custom content,
  Settings, and the Dark / Light / System toggle.
- **Main pane**: the selected game's detail, or Resources, or Custom content.
- **Game detail** has tabs: Folders, ReShade, Shaders, Add-ons, RenoDX, Review. The graphics
  API choice sits in the ReShade tab. The same surface serves a fresh install and an edit;
  there is no linear wizard and no separate edit hub.
- **Dock** at the bottom shows a live summary and the main action (**Install** or **Apply
  changes**), then progress with **Cancel**, then a per-folder result.
- **Apply** runs inline. Only one at a time, and the window stays on that game while it runs:
  picking another game shows a toast until it finishes or is canceled. (The design allowed browsing
  other games with Apply disabled; that needs the dock's state tied to one game and was not worth the
  risk of wiping an in-progress edit.) Cancel rolls back what that run wrote, using the registry.
- **Settings** is a modal and applies live. A changed cache location and the RenoDX list's hours
  need a restart. There is one refresh setting (`renodx_ttl_hours`); the shader catalog's age is
  not configurable.
  `config.yaml` stays hand-editable and the dialog writes the same file.
- **Custom content** is a read-only list with an Open folder button. It lists what the engine
  finds (a folder per shader pack or add-on under `shaders/` and `addons/`); there is no skipped-file
  warning, because the engine has no such concept.
- **Add game** uses the native folder dialog, with a path field as fallback and the name
  prefilled from the folder.
- **First run** has no onboarding. The pane says it is looking for games, then shows the
  list or an empty state with one Add game button. Errors are inline banners and toasts
  using the existing plain-language wording, not modal overlays.
- **Shortcuts** (mouse first): `/` or `Ctrl+F` filter, `Ctrl+R` rescan, `Ctrl+N` add game,
  `Ctrl+Enter` apply, `Esc` close a dialog. Shown in tooltips.
- **Flags**: `-version` and `-debug` stay; `-no-color` goes.

## Look
The look of slom with YARM's identity: a dark "gamer" tool that follows the desktop's dark or
light setting, with a Dark / Light / System switch, and the Omarchy theme when present.
- The accent is the **brand violet**, fixed; the desktop's accent is not used.
- Titles in Rajdhani, paths and versions in JetBrains Mono, both embedded. Body text is the
  system's own sans, not Inter: one fewer font to ship, and it matches the desktop.
- Colors are named by meaning in one palette per mode, never as literals in view code:
  violet = will be installed or selected, green = installed, amber = warning or blocker,
  cyan = information, red = removed or error.
- Wording is plain English, short, and says what will happen.

## Mapping the design to the engine
Where the design showed something the engine lacks, the control maps to what is real:
- Resources: Delete is disabled for an item an install uses (the engine would have allowed it
  after a warning); **Clean unused** is the engine's prune; there is no "clear everything" and no
  download sizes before a download (sizes show once cached).
- ReShade tab: the proxy DLL is Auto or an override; the Graphics API card only reports what was
  detected, since the engine cannot override it.
- Review: an edit lists what changes; a fresh install lists what is written. The backup suffix is
  `.yarm-bak`.
- Result: Installed, Uninstalled, Failed, Rolled back and Skipped. Adopting is its own action with
  its own confirmation, not part of an apply.
- Sidebar: no per-provider game counts there (Settings shows them), and Proton is not detected, so
  the "Windows build under Proton" pill appears for any game with a Windows executable.

## Testing
teatest is replaced by MyGo's `ui.NewTester`. Every `internal/` package, `internal/ui`
included, stays at or above 75% coverage. `docs/screenshot.png` and
`docs/screenshot-light.png` are generated by script, not asserted in tests.

## Docs
`CLAUDE.md` stays the entry point. This file records decisions, `GLOSSARY.md` the language,
`docs/MYGO.md` the MyGo patterns and test gotchas, and `docs/architecture/gui.md` is the GUI doc
(it replaced `tui.md`). The README follows slom's short shape: one sentence, a
screenshot, a one-line install, the build command.
