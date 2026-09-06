# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The wizard understands dependencies between catalog entries. Checking
  something that needs another package now selects that package too,
  across steps — ticking the AutoHDR add-on ticks the tone-mapping shader
  it needs, without which it silently does nothing. Requirements with
  alternatives (AutoHDR takes Lilium's pack *or* Pumbo's) are satisfied by
  whichever is already selected. The dependency's row says what pulled it
  in; unchecking it by hand is allowed but turns the dependent's row into
  a warning and adds a "Check" block to the review page. Upstream records
  none of this — it is prose inside PackageDescription — so the table is
  hand-kept in `internal/app/requires.go`, and only ids the loaded catalog
  actually offers are ever selected.

### Changed

- The install wizard's Shaders and Add-ons steps open on a curated
  shortlist instead of upstream's whole catalog (43 effect packages, 24
  add-ons, most of them niche). `a` widens either list to everything and
  back; the footer says how much is hidden. Nothing is removed — an item
  that is selected, or that a recorded install already uses, stays visible
  whichever view is on, and custom content is never filtered.
  - Shortlisted shaders: Standard effects, SweetFX, iMMERSE, METEOR,
    qUINT, AstrayFX, prod80 Color effects, OtisFX, CobraFX, Insane-Shaders,
    FXShaders, LumeniteFX, Legacy effects.
  - Shortlisted add-ons: Swap chain override, ShaderToggler, REST, AutoHDR,
    IGCS Connector, Display Commander, OBS Capture, and RenoDX — which
    upstream publishes no download for, so it stays greyed with its
    repository URL rather than disappearing.
  - New `insane` and `lumenite` package aliases for `defaults.packages`.

- A manual-only add-on now says why it is greyed out rather than just
  showing a URL: `manual install only: <repository>`.

- The install wizard keeps its steps, but every page after the first now
  shows a `✓` summary of what the earlier ones decided (`✓ ReShade 6.8.0
  (addon)   ✓ dxgi.dll   ✓ Standard effects`), directly under the
  breadcrumb. Paging was never the problem — not being able to see what
  you already chose was.
  - Step 1 shows the two ReShade builds as side-by-side panes,
    "ReShade (normal)" and "ReShade (addon)", over the same version list —
    the split the resources browser already shows. `←`/`→` chooses the
    build, `↑`/`↓` the version, and the cursor is shared, so switching
    build keeps the version. The hidden `tab` flavor toggle is gone. On a
    narrow terminal the panes fold to the focused build, with a strip
    naming both.
  - The Add-ons step still only appears when the chosen version is an
    add-on build, which is the only one that can load them.
  - The API step is no longer skipped when the graphics API is detected:
    it marks the matching option `← detected DirectX 12`, so skipping the
    question no longer means hiding the answer.
  - The wizard names the *folder* ReShade attaches to, not one executable
    inside it (the old Review page labeled a section "Folder" and then
    printed an executable path).
  - Review shrank to what the earlier steps could not already show: the
    download list and the options. What was chosen is in the header
    summary rather than repeated.

- Along the way, also still in effect: the "Exe" step was removed
  outright — which folder to target is resolved by whichever screen
  opens the wizard; "Packages" was
  renamed "Shaders", matching how the ReShade community refers to them,
  and lost its per-row "cached" badge as noise; the version list is
  capped to the 10 most recent (was unbounded, ~40 entries), still
  finding an edited install's version if it has aged out of that window,
  and the resources browser's ReShade panes are capped the same way (was
  top 3); "overwrite existing files" became a checkbox toggled with
  `space` rather than its own `o` key.

### Fixed

- A list row's trailing note is no longer clipped independently of the
  row's own text, which could render a line wider than the terminal; and
  the review page is clipped to the height it was given rather than
  pushing the footer (anti-cheat warning included) off a short screen.

- A confirm dialog (uninstall, delete a cached resource, adopt an
  unmanaged install) no longer treats Enter as "yes" — only `y` confirms
  now; Enter joins `n`/Esc as "no". A stray Enter left over from whatever
  the previous screen was doing could otherwise fire a destructive action
  with no warning, since the dialog's own text never mentioned Enter did
  anything at all. It now reads `n/esc/enter no`.
- The wizard's Shaders/Add-ons steps (a real catalog runs ~40 packages)
  now window around the cursor instead of printing every row
  unconditionally — a list longer than the available height was
  overrunning the shell's own footer with no sign anything was cut off.
  A "↑ N more above"/"↓ N more below" line appears when scrolled.
- The resources browser's 4-pane layout now folds to a single, full-width
  pane with a tab strip (still `←`/`→` to switch) below the width where
  every pane could keep its 20-column floor — at 60 columns (a `tmux
  split-window -h` on an 80-column terminal) the four panes previously
  needed 80 columns between them and overflowed.
- A terminal below 40×8 — where none of the app's layouts can render
  honestly — now shows a plain "terminal too small" message instead of
  whatever truncated or overlapping output the layouts happened to
  produce.
- Every list that can grow now scrolls instead of printing past the
  bottom of the window:
  - The game detail screen scrolls by folder around the cursor, saying
    how many folders are hidden above and below; a folder with many
    executables lists the first six and counts the rest.
  - The games list's side panel is clipped to its own box (lipgloss
    `Height` sets a minimum, not a maximum, so a tall game detail used
    to push the panel's bottom border off-screen).
  - The custom-content screen and settings' manual-games list both
    window around their cursor.
  - The wizard's own lists (versions, shaders, add-ons, downloads)
    window to whatever height the header and footer leave.

- The resources browser (`c`) is refined:
  - The ReShade pane is split into two — "ReShade (normal)" and "ReShade
    (addon)" — since the two flavors are separate downloads with
    independent cached state; each now lists just the bare version number
    instead of repeating "(normal)"/"(addon)" on every row.
  - A row that has not been downloaded no longer prints a redundant "(not
    downloaded)" tag — the dimmed color already says so. Cached, custom
    and in-use rows still get their tag (size, "custom", "in use"), since
    those are facts the color alone can't carry.
  - Every pane now sorts cached (and custom) items first, so what's
    already on disk doesn't get lost below a long list of what isn't.

- The anti-cheat warning for the add-on build now shows once, at the very
  bottom of a folder's whole block (games list side panel and
  `GameDetailScreen` alike) — after its executables, not sandwiched
  between the ReShade status and the executables list where it could
  scroll out of view above a long package or technique list.
- `GameDetailScreen`'s "update ReShade" is renamed "edit install", on its
  own key (`e`) rather than reusing `i` (which now only ever means
  "install", i.e. there is nothing here yet) — a shortcut that no longer
  means two different things depending on state.
- Installing, editing or uninstalling no longer requires drilling into
  the detail screen first: `GamesScreen` now offers `i`/`e`/`u` directly
  on the highlighted game, for a game with exactly one folder (the common
  case). A game with more than one folder still needs the detail screen,
  since there is no single install to act on without saying which.

- The cache manager (`c`) is replaced by a resources browser: three panes
  (ReShade versions, effect packages/shaders, add-ons) that answer
  "what's available", not just "what's cached" — the two used to be
  separate questions (browse the catalog in the wizard vs. manage what's
  on disk in the cache screen), and now they are one screen.
  - The ReShade pane shows the 3 most recent catalog versions in both
    flavors (6 rows), plus anything else still cached from outside that
    window so it stays deletable rather than becoming invisible. Shaders
    and add-ons show every catalog entry plus any custom-provided content
    from `cache/custom`, each row marked cached (✓, green), custom (★, a
    distinct color) or not yet downloaded (dim), with its size once
    cached, and "in use" when any recorded install (any game) actually
    references it — cross-referenced against `installs.json` directly, so
    deleting something still in use asks for confirmation with an explicit
    warning rather than doing it quietly.
  - `←`/`→` (or `h`/`l`) move between panes, `↑`/`↓` within one, `d`
    downloads the highlighted item ahead of any install (reusing the same
    `cache.Ensure*` machinery the wizard already uses — an add-on
    downloads whichever architectures it actually publishes, not a
    duplicate fetch for an architecture-neutral one), `x` deletes it (with
    confirmation), and `R` refreshes a cached package the same way the old
    cache screen's "refresh" did.

- `GameDetailScreen` now selects by folder, not by executable: ReShade's
  unit is the folder, so pressing `i`/`u`/`a` (renamed from `m`, see
  below) always acts on the current folder's one install, and there is
  nothing to select within a folder holding several executables (its own
  bug report: selecting a sibling executable used to look actionable when
  it never was). The folder cursor (`↑`/`↓`) only appears at all when a
  game actually has more than one folder — the common single-folder case
  renders exactly as before, just without a pointless per-executable
  cursor.
  - Per-folder rendering is also simplified and consistently indented:
    `Release/` as a bare header, its `ReShade - <one-line summary>` and
    `Executables` indented under it, and each executable on one line
    (`F10.exe · x86 · unknown`) instead of two — both the games list side
    panel and `GameDetailScreen` share this through `reshadeStatusText`
    and the same folder-block layout.
  - `t` ("show all exes") is gone: skipped executables (installers, crash
    handlers) are simply never shown in this view — the toggle was solving
    a problem the per-folder redesign no longer has.
  - Pressing `i` on a folder with an unmanaged install now opens the same
    adopt confirmation `a` does, instead of attempting a fresh install
    over files that are already there and would only conflict with it.
  - `m` ("track this install") is renamed `a` ("adopt ReShade") — a
    shortcut that actually matches the word it stands for — and its
    confirm dialog explains what adopting actually does in more detail:
    which files get recorded, that nothing on disk changes, and what
    becomes possible afterward (update/uninstall through yarm).
  - The add-on build is flagged with a red warning wherever it appears —
    the wizard's version and review steps, and any "ReShade - ..." status
    line for an add-on install or an unmanaged folder with add-on files —
    since some anti-cheat systems treat it as a cheat-tool signature and
    using it in an online or competitive game can get an account banned.

### Added

- ReShade's own runtime files are now read for information yarm never
  wrote itself, verified against ReShade's own source
  (github.com/crosire/reshade): `internal/install/inspect.go`'s
  `InspectRuntime` reads the DLL's own log (`ReShade.log`, truncated fresh
  on every launch — confirmed via `dll_log.cpp`'s `CREATE_ALWAYS`) for a
  best-effort "last seen running" version when the recorded one is the
  `unknown (adopted)` placeholder; follows `ReShade.ini`'s
  `[GENERAL]/PresetPath` to the active preset and reads its `Techniques`
  list (a small dedicated ini reader, since ReShade's preset dialect keeps
  that list in an unnamed section before any `[EffectFile.fx]` header, and
  escapes a literal comma as two in a row — both of which the existing
  `internal/catalog` ini parser does not handle) to show which effects are
  actually enabled; and lists available `reshade-shaders/Shaders/*.fx`
  files. All three degrade to nothing when the underlying file does not
  exist yet (no log until the game has run once, no preset until the
  in-game overlay has saved one).
  - The "ReShade" section (games list side panel and `GameDetailScreen`)
    now shows all of this: the proxy DLL alongside which graphics APIs it
    covers (`dxgi.dll (D3D10 / D3D11 / D3D12)`, reusing the wizard's own
    `dllOptions` descriptions), packages and add-ons as an indented list
    (one per line, capped with "+N more") instead of a single comma-joined
    line, enabled effects the same way, and an available-effects count.

- ReShade status and actions are now resolved per folder, not per
  executable — fixing a real correctness gap, not just a display one.
  ReShade intercepts by directory: Control's `Control.exe`,
  `Control_DX11.exe` and `Control_DX12.exe` share one folder and can only
  ever have one install between them, but `GameDetailScreen` was deciding
  "install" vs. "update" and resolving `u`/`i`'s target from whichever
  executable was highlighted — selecting a sibling executable and
  installing would have written a second `installs.json` entry over the
  same on-disk files, leaving both entries claiming files only one of them
  could safely own.
  - `FolderGroup.installedExe()` finds the executable an install is
    actually recorded against; `startInstall`/`startUninstall` resolve
    through it regardless of which executable is highlighted, and the
    executables table marks every executable in an installed (or
    unmanaged) folder with the same ✓/⚠, not just the one literally named
    in `installs.json`.

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
