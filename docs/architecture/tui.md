# The TUI (`internal/app`)

Bubble Tea v2. There is no CLI: running `yarm` opens this, and it is the only
way to drive the program.

## Shell and screens

`Model` (`app.go`) is the root. It owns the terminal size, the resolved
styles, a **stack** of screens and at most one overlay. Everything else is a
`Screen` (`screen.go`):

```go
type Screen interface {
    Init() tea.Cmd
    Update(msg tea.Msg, env Env) (Screen, tea.Cmd) // returns the next screen value
    View(env Env) string                            // body only; the shell draws title + status
    Title() string
    KeyBindings() []key.Binding                     // shown in the footer and `?` help
}
```

| Screen | File | Role |
| --- | --- | --- |
| `GamesScreen` | `games.go` | home: game table + detail panel |
| `WizardScreen` | `wizard.go` | install / edit flow |
| `ProgressScreen` | `progress.go` | runs an apply, streams progress, cancel |
| `ResultScreen` | `result.go` | what a flow did, per folder |
| `ResourcesScreen` | `resources_screen.go` | browse / download / delete cached content |
| `CustomScreen` | `custom_screen.go` | the user's `custom/` content |
| `SettingsScreen` | `settings_screen.go` | edits `config.yaml` (applies on next launch) |
| `AddFolderScreen` | `addfolder.go` | add a manual game |

Overlays (`overlay.go`) — help, confirm, error — take every key until
dismissed. `friendlyError` rewrites common failures (offline, rate-limited,
permission denied) into plain language without hiding the original text.

## Rules that are easy to get wrong

- **`Env` is passed in, never stored.** Size and theme have one source of
  truth, so a resize can't leave a screen holding a stale width.
- **A pushed screen has never seen a `WindowSizeMsg`.** Bubble Tea sends one
  only on a real resize, so `Model.sized()` hands the current size over on
  push and pop. Skip it and table-based screens render blank.
- **Screens are values.** `Update` returns the next screen; don't mutate
  through pointers that other copies share.
- **Navigation is by command**: `PushScreen`, `PopScreen`, `PopToRoot` (after
  an install, so the games list reloads), `ReportError`, `SetStatus`,
  `Confirm`. A screen implements `backHandler` to intercept `esc` (wizard
  steps back, progress cancels).
- **Width is measured in cells.** `clipTail` (`render.go`) counts runes and
  cleans with `safetext`, so **clip before styling** — clipping a styled
  string cuts through escape codes.
- **Styles resolve once** from the terminal's real background
  (`tea.RequestBackgroundColor` → `NewStyles(isDark)`), so rendering stays
  pure. `-no-color` forces monochrome.

## Async work (`async.go`)

The UI goroutine never blocks.

- `Async(ctx, work, onDone)` — one result, errors routed to the error overlay.
- `StreamJob(ctx, work)` + `WaitForActivity(ch)` — several messages over time
  (download progress, file copies). Re-issue `WaitForActivity` after every
  message except the last.
- **`StreamJob`'s `send` deliberately has no `ctx.Done()` escape.** On cancel,
  the final message ("canceled") must still arrive or the screen hangs on
  "canceling…". It is safe because the listener is always still waiting. Read
  the comment before changing it.
- Results a screen depends on must come back as that screen's own message
  type (e.g. `wizardDataLoadedMsg` carries its `err`), not via `errorMsg` —
  otherwise the screen never learns the load finished and stays "loading".

## Dependencies (`deps.go`)

`Deps` bundles the wiring game-focused screens need — `WizardDataLoader`,
`Installer`, `UninstallRunner`, `AdoptRunner`, the cache, dirs, config. It is
threaded from `Run` through `GamesScreen` and the folder picker to the
wizard; each screen passes it along
rather than building its own. The runners are small interfaces so tests use
fakes instead of the network and disk.

`RealInstaller` (`pipeline.go`) is the production `Installer`: load registry →
`NeededArtifacts` → resolve through the cache → `Planner.Plan` →
`Executor.Run` → clear the games cache. A single apply can carry several
`folderOp`s (install and/or uninstall per folder); `folderOutcome.Attempted`
separates "failed" from "never started" for the result screen.

## Session caches

- **`GamesCache`** — discovery runs once per process; navigation never
  rescans. The rescan key clears it.
- **Wizard data** (`wizard_data_memo.go`) — catalogs, custom content and the
  latest-version lookup are memoized for the session and **preloaded** at
  startup alongside the game scan, so the wizard opens instantly. A failed
  load resets the `sync.Once` so the next caller retries.

## The wizard (`wizard.go`)

One `Screen` holding a step index and one field group per step — not a stack
of screens — so the in-progress `install.Request` lives in exactly one place.

Steps: **Paths** (which folders) → **ReShade** (version, flavor) → **API**
(proxy DLL) → **Shaders** → **Add-ons** → **RenoDX** → **Review**. Every page
after the first shows a summary of earlier answers.

- **Edit mode** opens on a hub (`stepHub`) listing each section with what is
  installed, starting from the recorded install rather than config defaults —
  so editing one thing never silently resets the others.
- **Review** shows `install.Preflight` conflicts and what must still be
  downloaded (`install.NeededArtifacts`). It runs no planner and downloads
  nothing.
- Selections pass through `requires.go` (auto-select dependencies, explain
  why) and the shader list opens on `curated.go`'s shortlist.
