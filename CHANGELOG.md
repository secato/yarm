# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Adopt an unmanaged ReShade install: when a game folder already has a
  known proxy DLL (`dxgi.dll` and friends) plus `ReShade.ini` next to an
  executable yarm did not put there itself — installed by hand, or by
  another tool — the games list and detail screen now say so ("found,
  untracked") instead of showing it as plain uninstalled.
  - `internal/install/adopt.go`: `ScanUnmanaged` probes a directory for a
    proxy DLL plus `ReShade.ini` and gathers the full detail (DLL name,
    shader/texture/add-on files, `d3dcompiler_47.dll` presence); `Adopt`
    hashes every file found and writes an `installs.json` entry for it —
    nothing on disk changes, only the manifest gains an entry, so a later
    update or uninstall through yarm works exactly as if yarm had
    installed it in the first place. Adopted shader/texture/add-on files
    are recorded under a new `state.OriginAdopted` origin, since their
    catalog provenance (which package or add-on they came from) genuinely
    is not known; the ReShade version is recorded as `"unknown (adopted)"`
    for the same reason — both display-only, and neither affects
    uninstall, which works from the recorded file hashes, not the version.
  - `GameDetailScreen`: pressing `m` opens a confirm dialog describing what
    was found (DLL name, file count), then records it and shows a result
    screen; the games list reloads afterward so the newly tracked install
    shows up immediately.
  - Proven with a byte-identical round-trip test: adopting an install, then
    uninstalling it through yarm, leaves the game directory exactly as it
    was before the manual install ever happened.

- Folder-grouped ReShade status, replacing a redundant per-executable one:
  ReShade intercepts by directory (the proxy DLL sits beside whichever
  executable loads it), so a folder with several executables — e.g.
  ELDEN RING's `Game/` holding both `eldenring.exe` and the EAC launcher
  stub `start_protected_game.exe` — was showing the same "found,
  untracked" line once per executable, and a tracked install's version,
  flavor, packages and add-ons were nowhere visible at all.
  - `internal/app/games_data.go`: new `FolderGroup` groups a game's
    executables by directory and carries the one ReShade status (recorded
    install, unmanaged finding, or neither) that applies to all of them;
    `GameEntry.Groups` replaces the per-executable `Unmanaged` flag.
  - Both the games list's side panel and `GameDetailScreen` now show a
    "ReShade" section — version, flavor, DLL, package and add-on ids, or
    the unmanaged DLL name and a hint to press `m` — *before* the
    "Executables" list, per request, with each executable showing just its
    path, architecture and API (now rendered as "DirectX 12" rather than
    the raw `d3d12` tag).
  - `m` (adopt) now ties the install to the folder's primary executable —
    the first one not flagged as an installer/launcher/crash handler —
    rather than whichever executable happened to be highlighted, so
    pressing it on the EAC stub still records the install against the
    actual game executable.

- Edit an existing ReShade install: re-running the wizard (`i`, now
  labeled "update ReShade" once something is tracked) on an already-
  installed executable used to always start from the configured defaults —
  normal/addon flavor, default packages — silently discarding whatever was
  actually installed. It now starts every step (flavor, version, DLL,
  packages, add-ons) from the recorded install instead, so installing
  normal, then later switching to the add-on build and picking a few
  add-ons, is just running the wizard again rather than an unsupported
  path. The install engine already diffed and cleaned up a changed
  package/add-on selection correctly (`docs/plan/05-install-engine.md`'s
  upgrade handling); the missing piece was purely the wizard's own
  starting selections. An adopted install's unrecorded version falls back
  to the catalog's latest.

- Reliability and friendliness polish (part of step 8; README/demo GIF are
  tracked separately):
  - A one-time welcome banner on first launch (detected as "config.yaml did
    not exist yet", checked before bootstrap creates anything): what Steam
    found, and a hint to press `a` to add a folder yourself. Dismissed by
    the first keypress, which still does whatever it would normally do.
  - `catalog.StatusError`, a typed error for a non-200 catalog response
    (previously just a formatted string), with a `RateLimited()` check for
    GitHub's two rate-limit statuses (403, 429).
  - `internal/app/friendlyerror.go`: rewrites a DNS failure, a network-level
    error, a GitHub rate limit, or a permission error into a plain-language
    explanation for the error overlay, and now also for a game whose
    executables could not even be scanned — shown distinctly from an empty
    or native-build game, in both the games list's status column and the
    detail views, rather than just looking empty. Anything unrecognized
    passes through as the original error text unchanged.
  - Coverage on every non-TUI package now clears the plan's 75% target
    (`internal/fsutil` 68.7% → 76.1%, `internal/paths` 72.7% → 100%,
    `internal/buildinfo` 0% → 100%). `paths`'s per-OS directory resolution
    was split into `osdirs_unix.go` / `osdirs_windows.go`, matching the
    `roots_{linux,windows,darwin}.go` / `freespace_{unix,windows}.go`
    convention already used elsewhere — its Windows branch had been stuck
    at a permanent 50% on any single-OS run since only one platform's
    branch can ever execute in one process, not a gap either CI leg alone
    could close.

  Verified live: a real DNS failure and a real 0-permission directory both
  produce the friendly message (not just the constructed-error unit tests);
  a fresh `YARM_HOME` shows the welcome banner with the real Steam count
  from this machine's library; a manually chmod'd game folder shows
  "Permission denied" (with the exact path) in the games list, the side
  panel and the detail screen alike.

- Cache manager, custom content and settings screens (step 7):
  - `CacheScreen`: table of cached artifacts (name, kind, version, size,
    downloaded, last used); `s` cycles size/date/name sort; `d` deletes with
    the shared confirm overlay; `R` re-fetches a package's `DownloadURL`
    straight from its own `package.json` (no live catalog needed) so it
    picks up an upstream change even within the same day's cache key; the
    header shows the cache's total size and free disk space.
  - `internal/fsutil.FreeSpace`: `statfs(2)` on Linux/macOS,
    `GetDiskFreeSpaceEx` on Windows, behind one cross-platform signature.
  - `cache.Cache` gained `HasReShade`, `HasPackage`, `HasAddon` and
    `HasD3DCompiler` — cheap existence checks a UI can call for a "cached"
    badge without downloading anything to find out.
  - `CustomScreen`: read-only view of `cache/custom`'s two folders and what
    `catalog.ScanCustom` found in them; `o` prints the highlighted folder's
    absolute path to the status bar.
  - `SettingsScreen`: edits a working copy of `config.Config` — default
    flavor (toggle), cache directory and catalog TTL (inline text edit),
    manual game folders (add, reusing `AddFolderScreen`'s own path
    validation; remove) — and writes it back with `s`. Every field takes
    effect on the next launch, not the running session: the cache
    directory and the game providers are both resolved once at startup, so
    live-reloading either was judged not worth the added complexity for a
    local, restart-anytime tool.
  - `c`/`x`/`s` on the games screen open the three screens.

  A real, reproducible rendering bug found while building this and fixed
  for all three affected screens (two from step 5, `GamesScreen`'s filter
  and `AddFolderScreen`'s path input; none from step 7's own new fields):
  `bubbles/textinput`'s placeholder renderer sizes its internal buffer
  from the field's configured `Width()`, not from the placeholder string —
  left at the zero value, it renders only the placeholder's first
  character and stops, no matter how long the hint text is. Every
  placeholder-bearing text field now calls `SetWidth`.

  The same "a failure bypasses this screen's own state" bug class from
  step 6 (`StreamJob`'s send/cancel race) turned up twice more while
  building the cache screen, and was fixed the same way in the screens
  that had it: `GamesScreen.load` and `WizardScreen.Init` both used the
  generic `Async` helper, whose error path routes straight to the shell's
  overlay without ever reaching the screen — leaving `loading` stuck
  `true` and the title permanently claiming "scanning…" (or every wizard
  step permanently showing "loading catalog data…") once the error dialog
  closed. All five loaders that can fail now resolve to a message the
  owning screen's own `Update` always sees, success or not.

  Verified live against the real cache (built up over earlier steps: a
  ReShade build, two packages, `d3dcompiler_47.dll`) and the real
  `~/.cache/yarm`: the cache screen listed all four real entries with
  correct sizes and cycled through all three real sort orders; canceling a
  delete left the cache untouched; the custom screen showed the real
  `cache/custom` paths; settings correctly toggled the flavor, edited the
  TTL, added a real folder through the reused add-folder flow, and wrote a
  `config.yaml` that round-trips through `config.Load` with the new value.

- Install wizard, progress screen, result screen and uninstall confirm
  (step 6):
  - `WizardScreen` walks Exe → Version → API/DLL → Packages → Add-ons →
    Review (Add-ons skipped for the normal flavor), building an
    `install.Request` as it goes. Required packages are locked selected,
    manual-only add-ons are shown greyed with their repository URL and
    cannot be selected, and "cached" badges come from small existence
    checks (`cache.Cache.HasReShade/HasPackage/HasAddon/HasD3DCompiler`)
    rather than a network round trip.
  - `ProgressScreen` runs the resolve-then-install pipeline in a
    background goroutine and narrates it back over a channel — the
    "waitForActivity" pattern docs/plan/02-architecture.md §2.3 names —
    since a `tea.Cmd` can only ever deliver one message. `esc` cancels a
    running install via its own `context.CancelFunc`; the final result
    always arrives, canceled or not.
  - `ResultScreen` summarizes what an install or uninstall did and returns
    to (and reloads) the games list via a new `PopToRoot` navigation
    command — the wizard and progress screens underneath it are stale
    once something has changed, so there is nowhere useful to pop back to
    one level at a time.
  - `i` on the game detail screen opens the wizard on the highlighted
    executable; `u` confirms and runs an uninstall.
  - `RealInstaller`/`RealUninstaller` (in `internal/app`, not
    `internal/install`) wire the cache, catalog and install engine
    together for the TUI — combining those packages is the app layer's
    job per the documented dependency rule, so this intentionally
    duplicates a little of what `cmd/yarm`'s `install` command already
    does for the CLI rather than reaching across that boundary.
  - `cache.Cache` gained `HasReShade`, `HasPackage`, `HasAddon` and
    `HasD3DCompiler` — existence checks a UI can call for a "cached"
    badge without downloading anything to find out.

  A real bug found by testing: `WizardScreen.buildRequest` kept whatever
  add-ons had been selected even after the user stepped back and switched
  to the normal flavor, which `install.Request.Validate` then rejected
  outright — the request that would have reached the install engine could
  never have installed at all. Add-on selections are now dropped from the
  built request whenever the flavor does not support them.

  A second bug, in the new streaming helper itself: `StreamJob`'s per-message
  send raced a `ctx.Done()` escape hatch against the channel send, so a
  message — including the final result — could be silently dropped if it
  was sent at the exact moment the context was canceled. That escape hatch
  is gone; `send` now always blocks until read, which is safe because
  Bubble Tea always starts listening the moment `Init` hands back the
  listening command.

  Verified against a scratch copy of a real game, driven through the built
  binary via a pty end to end: install (with real catalog data — 41
  ReShade versions, 43 packages, 24 add-ons, correct "cached" badges) →
  games list correctly shows `✓ 6.8.0 addon` after `PopToRoot` reloads it →
  uninstall via the confirm dialog → `diff -r` against the pristine copy is
  byte-identical, and `installs.json` is empty again.

- Games discovery: Steam provider (`libraryfolders.vdf` + `appmanifest_*.acf`
  parsing, per-OS install roots, non-game skip list) and manual provider over
  `manual_games` in the config; executable scanning with directory and filename
  skip rules; PE inspection for architecture and graphics-API guessing.
  `yarm games ls [--json]` lists it all. (step 1)
- Project bootstrap: `yarm` root command with `version` and `paths`;
  `internal/paths` (XDG dirs, `YARM_HOME` override, first-run creation),
  `internal/config` (YAML load/save with defaults and validation),
  `internal/fsutil` (atomic write, hashed copy, directory size),
  `internal/buildinfo`; slog file logging; LICENSE, README, CONTRIBUTING,
  Makefile, golangci-lint config and CI workflow. (step 0)

- Native-build detection: a game whose scan yields no Windows executables is
  checked for an ELF/Mach-O binary and flagged as a native build, which
  `yarm games ls` reports instead of showing an empty entry. ReShade proxies a
  DLL through the Windows loader (real or Wine's), so native builds are not
  installable targets.

- An oversized catalog response is now rejected rather than silently truncated,
  which would have parsed as a valid but incomplete package list.

- Catalog (`internal/catalog`): INI section parser, effect package and add-on
  catalogs, ReShade version discovery (reshade.me latest marker + GitHub tags,
  numeric semver sort, floor at 5.0.0), custom content scanning under
  `cache/custom`, and package id slugs with a hand-kept alias map. Catalogs are
  cached under `cache/catalog` with a TTL from `catalog_ttl_hours` and fall back
  to the cached copy at any age when the network is unavailable. Hidden
  `yarm catalog ls [--json]` debug command. (step 2)

- Fetch, cache and artifacts (step 3):
  - `internal/fetch`: downloads with retries and exponential backoff (5xx,
    429 and transport errors only), progress reporting, and optional SHA-256
    verification. Downloads land in a `.part` file and are renamed into place
    only once complete and verified.
  - `internal/archive`: zip reading that rejects any entry which would escape
    the destination directory, plus top-directory stripping and Shaders/
    Textures discovery.
  - `internal/artifacts`: ReShade DLLs out of the zip-appended setup
    executable, effect packages normalized to `Shaders/` + `Textures/` +
    `package.json`, add-on binaries from direct files or zips, and
    `d3dcompiler_47.dll` from the pinned Firefox installer via sevenzip.
  - `internal/cache`: `index.json` with schema versioning, `Ensure*` helpers
    that download on demand and reuse what is present, and listing, sorting,
    sizing and deletion.
  - `yarm cache ls [--json] [--sort] [--desc]` and `yarm cache clean [--yes]`;
    hidden `yarm fetch reshade|package|d3dcompiler`.
  - Opt-in integration tests behind `YARM_NETWORK_TESTS=1` that verify the
    pinned Firefox installers and the ReShade setup archive against the live
    hosts.

- Archive extraction limits: per-entry size, a 2 GiB total expansion budget
  and a 20,000 entry cap per archive, enforced against bytes actually written
  rather than the sizes an archive declares. An entry delivering more than it
  declared is an error and its partial file is removed, and archive entries are
  always written as regular files so a symlink entry cannot be used to escape
  the destination.
- `fetch.Client` caps a single download at 2 GiB without trusting
  `Content-Length`, so a host that streams without end cannot fill the disk.
  An oversized response is not retried.
- `artifacts.NeedsD3DCompiler(TargetOS)` expresses the Linux-only
  d3dcompiler_47.dll rule as a target-OS predicate, with
  `artifacts.CurrentTargetOS()` the single place `runtime.GOOS` is read.

- Install engine (step 4):
  - `internal/state`: the `installs.json` registry — every file yarm writes into
    a game directory, with its hash, plus the originals it displaced. Saved
    atomically with a schema version. A corrupt registry is an error rather than
    something to start fresh from: it is the only record able to remove those
    files.
  - `internal/install`: `Planner` resolves the full file set and classifies each
    destination (create / replace / skip / backup / keep) against the game
    directory and the registry, without writing anything; `Executor` applies a
    plan and rolls back completely on any failure; `Uninstaller` removes only
    files whose content still matches what was installed.
  - `ReShade.ini` is generated when absent and never overwritten. Files an
    upgrade no longer needs are moved aside rather than deleted, so a failed
    upgrade restores the install it was replacing.
  - Uninstall keeps files the user has edited since installation, and preserves
    `ReShadePreset.ini` and `ReShade.log` unless explicitly asked.
  - Hidden `yarm install` / `yarm uninstall` commands and `yarm installs`.

- TUI foundation (step 5):
  - `internal/app`: root model with a screen stack, modal overlays (help,
    confirm, error), a central keymap, a palette resolved from the terminal's
    reported background via `lipgloss.LightDark`, an async helper, and
    `WindowSizeMsg`-driven layout. Built on Bubble Tea v2.
  - Games screen: table of discovered games with source and install status, a
    detail panel for the selected game, `/` filter, `r` rescan and `a` add
    folder with live path validation.
  - Game detail screen: executables with architecture, guessed API and install
    status, and a toggle for the ones the scanner flagged as installers or
    crash handlers.
  - `yarm` with no arguments now launches the TUI; `--no-color` (and
    `NO_COLOR`) disable color.
  - `teatest/v2` tests at a fixed 100x30 with the color profile disabled,
    plus direct model tests for navigation and key routing.

### Changed

- Executable scan depth raised from 3 to 4. Source 2 and some Unreal layouts
  place the game binary at `game/bin/<platform>/<name>.exe`, which a depth-3
  walk missed entirely.
