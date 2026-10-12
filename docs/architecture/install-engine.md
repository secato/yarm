# The install engine (`internal/install`)

Everything that touches a game folder goes through this package. It is
deliberately split into phases that never mix.

## Request

`install.Request` (`install.go`) is what the user asked for: game, target
executable, ReShade version and flavor (`normal` / `addon`), proxy DLL name,
and the package / add-on / custom / RenoDX ids. `Request.Validate` rejects
incoherent requests up front — for example add-ons or RenoDX without the
`addon` flavor, since the normal build never loads them.

Two fields are shaped to make mistakes hard:

- `NoBackup` is negated so the zero value is the safe one: a caller that
  forgets it keeps backups.
- `RenoDX` is a single id, not a slice. Two RenoDX mods in one folder fight
  over the same swap chain, so the type forbids it.

ReShade loads by **directory**, not by executable: whatever process loads
the proxy DLL beside it gets ReShade. `Request.ExeDir()` / `install.ExeDir()`
is therefore the unit everything is placed relative to.

## Phase 1 — Plan (`planner.go`)

`Planner.Plan(req, artifacts)` turns a request plus already-resolved cache
directories into a `Plan`: every destination file and its `Action`.

| Action | Meaning |
| --- | --- |
| `create` | nothing there yet |
| `replace` | a file a previous yarm install recorded |
| `skip` | identical file already in place |
| `backup` | foreign file; save it as `.yarm-bak`, then overwrite |
| `discard` | foreign file; overwrite without a copy (`Overwrite` + `NoBackup`) — the only irreversible action |
| `keep` | foreign file left alone because `Overwrite` is off |

Planning does **no downloads and no writes**, only stats and hashes. That
keeps the whole decision table unit-testable against temp dirs.

Things worth knowing:

- **Upgrades** — when the registry already has an install for this exe, files
  it recorded that the new plan no longer produces go into `Plan.Removed`.
  Each is checked with `deletableShape` first (see below); one that fails is
  left alone with a warning.
- **Unresolved groups fall back to the manifest.** The pipeline only resolves
  artifact groups that actually changed (`needed.go`). A group it skipped is
  planned from the previous manifest, and those files classify as `skip`, so
  no source is ever read. An edit that only removes an add-on downloads
  nothing and works offline.
- **Identical shared files are planned once.** Two packages shipping
  byte-identical files at the same destination (iMMERSE and METEOR both
  vendor `MartysMods/mmx_*.fxh`) are deduplicated under the first origin;
  differing content at one destination is still an error.

## Phase 2 — Execute (`executor.go`)

`Executor.Run` applies a plan in a fixed order, journaling every change:

1. Move files in `Plan.Removed` aside as `.yarm-old` (not deleted yet).
2. Get foreign files out of the way — renamed to `.yarm-bak`, or deleted for
   `discard`.
3. Copy everything, hashing as it goes (`fsutil.CopyHashed`).
4. Write `ReShade.ini` only if the game has none (`ini.go`). An existing ini is
   the user's configuration and is never overwritten. The generated one uses
   CRLF, like ReShade's own installer.
5. Write the manifest. Until this point nothing claims the install.
6. Only then delete the `.yarm-old` files.

Any failure rolls back the journal: created files are deleted, `.yarm-bak`
and `.yarm-old` files are restored, and no manifest is written. A
half-installed ReShade is worse than none, because the game may refuse to
start and nothing records why. The two suffixes differ so rollback can never
confuse "the user's original" with "our superseded file".

`safeDest` resolves every destination and refuses anything that escapes the
game root, since destinations are built from catalog content.

## Phase 3 — Record (`internal/state`)

`installs.json` in the data directory: games keyed by id, one `Install` per
executable, each listing every `File` (slash-separated path, SHA-256, size,
`Origin`) and every `Backup`.

`Origin` says which part of the install produced a file: `reshade`,
`d3dcompiler`, `ini`, `adopted`, or `package:<id>`, `addon:<id>`,
`renodx:<id>`, `custom:<id>`.

- `state.Save` is atomic (`fsutil.AtomicWrite`) and validates before writing.
- `state.Load` treats a **corrupt registry as an error**, unlike the cache
  index. It is the only record of what yarm put into game folders; silently
  discarding it would orphan every install.
- `Registry.Validate` rejects anything yarm could not have written: unclean
  or absolute paths, unknown origins, bad hashes, relative game roots, more
  than 20 000 files in one install.
- Optional fields (like `renodx`) are added without bumping `SchemaVersion`.
  Bump it only for incompatible changes; a newer schema makes older yarm
  refuse to load (`ErrSchemaTooNew`).

## Uninstall (`uninstall.go`)

`Uninstaller.Run` removes exactly the files the manifest lists, and for each
one, only if:

- **its hash still matches.** A changed file is the user's now (an edited
  shader, a tweaked ini) and is kept and reported.
- **it passes `deletableShape`.** Each origin may only name files yarm
  actually writes in that place: `reshade` → a `.dll` in the exe dir,
  `package:`/`custom:` → under `reshade-shaders/Shaders` or `Textures`,
  `addon:`/`renodx:` → `.addon32`/`.addon64` in the exe dir, and so on. A
  forged manifest entry pointing at a savegame fails here whatever its hash.
- **it is not user data** — `ReShadePreset.ini` and `ReShade.log` survive
  unless `RemoveUserData` is set.

The game root itself is checked first: it must be a directory that still
contains the recorded exe (or at least its directory). A root that vanished
entirely just drops the record. Backups are restored, then directories left
empty are pruned (`os.Remove` only removes empty directories, so game folders
are never touched).

## Preflight (`preflight.go`)

Runs **before** anything is downloaded, for the wizard's review page. It
checks only the names known in advance — the proxy DLL, `d3dcompiler_47.dll`,
`ReShade.ini` — because that is where real conflicts happen: `dxgi.dll` is
the name every other injector wants too. It answers the one question the
user must decide up front: overwrite or not.

## Needed artifacts (`needed.go`)

`NeededArtifacts(req, prev)` works out which artifact groups must be resolved
by hashing what the previous install put on disk. Unchanged groups are
skipped (see the planner fallback above).

**`prev == nil` means a fresh install and needs everything.** Pass `nil`, not
a pointer to a zero `Install` — the latter diffs against an empty manifest and
silently skips groups like `d3dcompiler_47.dll`. `findPrevInstall` in
`internal/app/pipeline.go` exists to get this right.

## Adopt (`adopt.go`)

`ScanUnmanaged` recognizes a ReShade install yarm did not create: a known
proxy DLL name **and** `ReShade.ini` next to the exe. `Adopt` hashes those
files and records them with `OriginAdopted`; nothing on disk changes. The
version is recorded as `AdoptedVersion` ("unknown"), which is display-only —
uninstall works from hashes.

## Inspect (`inspect.go`)

`InspectRuntime` reads ReShade's own `ReShade.log`, `ReShade.ini` and the
active preset to report the version that last ran and which techniques are
enabled. Read-only, best-effort, used for the game detail panel.
