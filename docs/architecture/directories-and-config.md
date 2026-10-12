# Directories and configuration

## Where things live (`internal/paths`)

`paths.Resolve()` returns three directories:

| | Linux | Windows | `YARM_HOME` set |
| --- | --- | --- | --- |
| Config | `$XDG_CONFIG_HOME/yarm` | `%LOCALAPPDATA%\yarm` | `$YARM_HOME/config` |
| Data | `$XDG_DATA_HOME/yarm` | `%LOCALAPPDATA%\yarm` | `$YARM_HOME/data` |
| Cache | `$XDG_CACHE_HOME/yarm` | `%LOCALAPPDATA%\yarm\cache` | `$YARM_HOME/cache` |

On Windows, XDG's config and data dirs are both `%LOCALAPPDATA%`, so yarm
keeps one app folder with the cache as a subfolder. `YARM_HOME` exists for
tests, portable installs and development (`YARM_HOME=/tmp/yarm-dev ./bin/yarm`).
`cache_dir` in `config.yaml` overrides the cache location only.

| File | Dir | Notes |
| --- | --- | --- |
| `config.yaml` | Config | user-edited; created with defaults on first run |
| `installs.json` | Data | the install registry — **losing it orphans installs** |
| `yarm.log` | Data | `slog` text log; startup logs the resolved dirs |
| `custom/` | Data | the user's own shaders and add-ons |
| everything in [the cache](content-sources.md#the-cache-internalcache) | Cache | re-downloadable |

### Why `custom/` is under Data, not Cache

Everything in the cache can be downloaded again; hand-placed shaders cannot.
The XDG spec and cleanup tools treat `~/.cache` as disposable. So
`paths.Custom()` is `<data>/custom`, deliberately unaffected by `cache_dir`,
and `paths.MigrateCustom` moves an old `<cache>/custom` on launch (a no-op
once the destination exists).

## Startup (`cmd/yarm`)

1. Parse flags: `-debug` (or `YARM_DEBUG=1`), `-no-color` (or `NO_COLOR=1`),
   `-version`. Stdlib `flag`; no cobra. A leftover argument is a usage error
   (exit 2) — usually someone reaching for a subcommand that no longer exists.
2. Check first run (no `config.yaml` yet) **before** anything creates files.
3. Resolve and create dirs, open the log, load config, migrate `custom/`,
   sweep `.part` files from interrupted downloads.
4. Wire providers, cache, catalog client and `app.Deps`; call `app.Run`.

**Nothing is written to stderr while the TUI runs** — it owns the alternate
screen and stray output corrupts it. Failures go to `yarm.log`, and `main`
prints the error to stderr after Bubble Tea has released the terminal
(exit 1).

## `config.yaml` (`internal/config`)

```yaml
cache_dir: ""            # optional cache override
manual_games:            # games added by path
  - name: My Game
    path: /games/mygame
steam:
  enabled: true
  extra_library_paths: []
gog:       { enabled: true }
epic:      { enabled: true }
battlenet: { enabled: true }
defaults:
  reshade_flavor: normal # "normal" or "addon"
  packages: [standard]
renodx_ttl_hours: 168
```

- `Default()` picks the **normal** ReShade flavor: the add-on build is what
  anti-cheat detects, so it has to be the user's explicit choice.
- A section missing from an older file keeps its provider **enabled**.
- Renamed keys are migrated on read and dropped on the next save
  (`catalog_ttl_hours` → `renodx_ttl_hours`).
- `Save` is atomic; `Validate` checks what struct tags can't.
- The settings screen writes the file; changes apply on the **next launch**.
