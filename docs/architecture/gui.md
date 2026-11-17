# The window (`internal/ui`, `internal/core`)

YARM is a native desktop window built with [MyGo](https://github.com/egoist/mygo):
GPU drawn, no web view, no cgo. There is no CLI: running `yarm` opens the window and
that is the only way to drive the program. MyGo patterns and test gotchas are in
[../MYGO.md](../MYGO.md); the look is in [Look](#the-look).

## Two packages

| Package | Holds | Imports MyGo |
| --- | --- | --- |
| `internal/core` | everything the window decides: the games list model, the edit `Session`, the apply pipeline (`RealInstaller`), resources, custom content, catalog loading, plain-language errors | no |
| `internal/ui` | drawing and turning clicks into `core` calls | the only package that does |

`core` knows nothing about windows, so every rule in it is tested without one. `ui` must not
hold a rule: if a click needs a decision (what is the request, what is in the way, what
changed, can this be deleted), the decision is a `core` method and the window calls it.
Nothing imports `internal/ui`; `cmd/yarm` wires `core.Deps` into `ui.Run`.

## The shell

One window, no stacked screens, no back button.

- **Sidebar** (`sidebar.go`): search, Rescan, Add game; the games in three groups
  (Installed, Not installed, Added by you); Resources, Custom content, Settings; the
  Dark / Light / System switch.
- **Main pane** (`detail.go`): the selected game, or Resources (`resources.go`), or Custom
  content (`custom.go`). With no games it is the loading or empty state.
- **Game page**: a header, six tabs — Folders, ReShade, Shaders, Add-ons, RenoDX, Review —
  and the **dock** (`dock.go`). The same page serves a fresh install and an edit; there is no
  wizard and no separate edit screen.
- **Dialogs** (`modal`): Settings (`settings.go`, applies live), Add a game (`addgame.go`),
  Uninstall and Adopt confirmations (`dock.go`). Esc closes one.

## `core.Session`

A `Session` is one game's edit: the chosen folders, ReShade version and build, proxy DLL,
shader packs, add-ons and RenoDX mod, plus what follows from them. It starts from the recorded
install when there is one (editing one thing never resets the others) and from the config
defaults when there is not.

| Question | Method |
| --- | --- |
| What does the list show | `PackageRows`, `AddonRows`, `RenoDXRows` |
| Tick or untick (pulls in what a pack needs) | `TogglePackage`, `ToggleAddon`, `ChooseRenoDX` |
| What request does each folder get | `Ops`, `Request` |
| What must still be downloaded | `Missing`, `Needed` |
| What is in the way | `Conflicts`, `HasForeign` |
| What does applying change | `Changes`, `Dirty`, `Headline` |
| What is wrong with the choices | `Unmet` |

Custom content ids (`custom:…`) go in `Request.Custom`, never in `Packages` or `Addons`: the
pipeline resolves them from a different place.

**Planning never downloads or writes.** The Review tab uses `Preflight` and `NeededArtifacts`
through the session; the planner runs inside the pipeline after the downloads, as in
[install-engine.md](install-engine.md#phase-1--plan-plannergo).

## Applying

`core.RunOps` runs one `Op` per folder (an install, or an uninstall — an edit that moves an
install is both) in order and stops at the first failure or cancellation. Each op is atomic on
its own; the batch is not, so the outcomes record exactly how far it got and `core.NewReport`
turns them into rows: installed, uninstalled, failed, rolled back, skipped.

The dock shows the plan, then progress with Cancel, then the report. Only one apply runs at a
time; picking another game is refused while it does. If something that is not yarm's holds a
file where ReShade goes, **Install** takes the person to the Review tab first rather than
replacing it on a default.

## Background work

`App.bg(work, done)` runs `work` off the UI thread and `done` back on it, then redraws. Results
travel in the closures' captures, never through shared state. The three seams — `spawn`,
`onMain`, `invalidate` — are fields so tests run everything inline. Dialogs that block (the
native folder picker) go through `App.pick` and `pickThen` for the same reason.

Nothing may write to stderr while the window is up. Log through `slog`.

## Rules that are easy to get wrong

- **A click needing a decision is a `core` method.** See above.
- **`pal` is set at the top of every frame** from the look; read colors from it, never as
  literals. Add a color to both palettes.
- **Element values are valid for one build pass.** Do not keep them in `App`.
- **Key repeated things** (`c.Key("game-"+id)`), or state leaks between rows.
- **The tester finds the first element with a text or label.** The sidebar repeats names
  ("Resources", "Steam", a game's name); give a control a distinct `Label`.
- **Advance the clock before a screenshot** (`tt.Advance`): switches slide and dialogs fade.
- **Anything slow is `bg`.** `LoadResources`, scans, downloads, the folder dialog.

## The look

Slom's dark "gamer" tool with YARM's identity. One palette per mode (`theme.go`), named by
meaning: **violet** will be installed or is selected, **green** installed, **amber** a warning
or something in the way, **cyan** information, **red** removed or failed. The accent is the
brand violet and does not follow the desktop. Dark or light follows, in order, `YARM_THEME`,
the saved Dark / Light / System choice, Omarchy's active theme (`internal/desktop`), then the
system's appearance. Rajdhani (titles, caps) and JetBrains Mono (paths, versions) are embedded
with their OFL licenses in `internal/ui/assets/`; body text is the system's sans.

## Session caches

- **`GamesCache`**: discovery runs once per process; picking a game never rescans. Rescan
  (Ctrl+R) and the Settings changes that alter providers clear it.
- **Catalog data** (`core.Memoize`): catalogs, custom content and the latest-version lookup
  are memoized and loaded alongside the game scan, so a game opens instantly. A failed load
  resets the `sync.Once` so the next caller retries; the window then shows what is on disk and
  an offline banner.
