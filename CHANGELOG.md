# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

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

- Catalog (`internal/catalog`): INI section parser, effect package and add-on
  catalogs, ReShade version discovery (reshade.me latest marker + GitHub tags,
  numeric semver sort, floor at 5.0.0), custom content scanning under
  `cache/custom`, and package id slugs with a hand-kept alias map. Catalogs are
  cached under `cache/catalog` with a TTL from `catalog_ttl_hours` and fall back
  to the cached copy at any age when the network is unavailable. Hidden
  `yarm catalog ls [--json]` debug command. (step 2)

### Changed

- Executable scan depth raised from 3 to 4. Source 2 and some Unreal layouts
  place the game binary at `game/bin/<platform>/<name>.exe`, which a depth-3
  walk missed entirely.
