# Directories and configuration

## Where things live (`internal/paths`)

`paths.Resolve()` returns three directories:

| | Default | `YARM_HOME` set |
| --- | --- | --- |
| Config | `$XDG_CONFIG_HOME/yarm` | `$YARM_HOME/config` |
| Data | `$XDG_DATA_HOME/yarm` | `$YARM_HOME/data` |
| Cache | `$XDG_CACHE_HOME/yarm` | `$YARM_HOME/cache` |

`YARM_HOME` exists for tests, portable installs and development (`YARM_HOME=/tmp/yarm-dev ./bin/yarm`).
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

1. Parse flags: `-debug` (or `YARM_DEBUG=1`) and `-version`. Stdlib `flag`; no cobra. A leftover argument is a usage error
   (exit 2) — usually someone reaching for a subcommand that no longer exists.
2. Check first run (no `config.yaml` yet) **before** anything creates files.
3. Resolve and create dirs, open the log, load config, migrate `custom/`,
   sweep `.part` files from interrupted downloads.
4. Wire providers, cache, catalog client and `core.Deps`; call `ui.Run`.

**Nothing is written to stderr while the window is up** — a desktop launcher
discards it and a terminal one would interleave it with the toolkit's own
messages. Failures go to `yarm.log`, and `main` prints the error to stderr
after the window has closed (exit 1).

`YARM_THEME=dark|light` overrides the saved look for one run.

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
- The Settings dialog writes the file. Changes apply **right away**: switching a
  provider or a library path rebuilds the loader and rescans. The two that cannot
  — a new cache location and the RenoDX list's hours — apply on the next launch,
  and the dialog says so.
- `theme` (`dark`, `light` or `system`) is what the Dark / Light / System switch saves.
