# YARM architecture

How yarm is put together, and the decisions that hold it together. Read this
before changing anything that writes into a game folder.

| Document | Covers |
| --- | --- |
| [install-engine.md](install-engine.md) | Plan → execute → record, uninstall, adopt, the safety checks around deleting |
| [content-sources.md](content-sources.md) | Catalogs, the download cache, artifact normalization, RenoDX, hand-kept tables |
| [game-discovery.md](game-discovery.md) | Providers (Steam, GOG, Epic, Battle.net, manual), launcher sources, PE inspection |
| [tui.md](tui.md) | The Bubble Tea shell, screens, navigation, async work, the wizard |
| [directories-and-config.md](directories-and-config.md) | Config/data/cache dirs, `YARM_HOME`, `config.yaml`, what lives where |
| [untrusted-input.md](untrusted-input.md) | Every place yarm parses something it doesn't control, and how it's bounded |
| [testing.md](testing.md) | Test conventions, fixtures, network gating, coverage |

## The one constraint

**yarm writes into directories it does not own.** A game folder holds the
game's files, other mods, and the user's own configs. Every design choice
follows from that:

- **Plan before write.** What will happen to each file is decided before
  anything changes on disk.
- **Record every file.** `installs.json` lists each file yarm wrote, with its
  SHA-256.
- **Back up anything replaced.** A foreign file in the way is saved as
  `<name>.yarm-bak` unless the user explicitly says not to.
- **Delete only what the manifest lists**, only if its hash still matches,
  and only if it is shaped like something yarm writes.

## Layering

```
cmd/yarm          flags, directory resolution, logging, wiring → app.Run
   │
internal/app      the TUI, plus the glue that spans packages (RealInstaller)
   │
   ├── install     plan / execute / uninstall / adopt / preflight / inspect
   ├── cache       download cache (Ensure* per artifact kind)
   │     ├── artifacts   normalize downloads into cache entries
   │     ├── fetch       https-only downloads, retries, .part files
   │     └── archive     zip extraction with path and size limits
   ├── catalog     upstream lists (EffectPackages.ini, Addons.ini, versions, RenoDX)
   ├── platform    Provider interface + steam, gog, epic, battlenet, manual
   │     └── sources     heroic, lutris readers shared by the store providers
   ├── game        Game/Executable model, PE inspection, executable scan
   ├── state       installs.json
   ├── config      config.yaml
   ├── paths       config/data/cache directory resolution
   └── fsutil, safetext, buildinfo   small leaf helpers
```

Two rules:

- **Nothing imports `internal/app`.** It is the top layer. Code that combines
  several packages — `RealInstaller` in `internal/app/pipeline.go` joins
  cache + catalog + install — lives there rather than in any one of them.
- **No globals.** Every package takes its dependencies as structs or small
  interfaces, so tests substitute temp dirs and `httptest` servers. The
  install target OS is a value (`artifacts.TargetOS`), not a `runtime.GOOS`
  read, so both Linux and Windows branches run in tests on either OS.

`cmd/yarm/main.go` holds no logic beyond wiring. `buildProviders` there is
the single place game providers are registered.

## The install flow end to end

1. **Discovery** — `platform.DiscoverAll` runs every provider; `game.Scan`
   finds executables in each game; `internal/app/games_data.go` groups them by
   folder, attaches recorded installs, detects unmanaged ReShade
   (`install.ScanUnmanaged`) and reads runtime info (`install.InspectRuntime`).
2. **Wizard** — collects a `install.Request`. Its review page shows
   `install.Preflight` (what is in the way) and `install.NeededArtifacts`
   (what must still be downloaded). Nothing is downloaded or written yet.
3. **Pipeline** (`RealInstaller.Install`) — resolves only the needed
   artifacts through the cache (downloading if absent), then
   `install.Planner.Plan`, then `install.Executor.Run`.
4. **Record** — the executor writes `installs.json` last; a failure before
   that rolls everything back.
