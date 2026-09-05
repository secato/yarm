# YARM — Yet Another ReShade Manager

[![CI](https://github.com/secato/yarm/actions/workflows/ci.yml/badge.svg)](https://github.com/secato/yarm/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/secato/yarm)](https://github.com/secato/yarm/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/secato/yarm)](https://goreportcard.com/report/github.com/secato/yarm)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

A terminal UI to install and uninstall [ReShade](https://reshade.me/), effect
packages and add-ons on a per-game basis, on Windows and Linux. YARM keeps a
shared download cache, remembers exactly which files it wrote into each game
folder, and can cleanly uninstall without touching anything it didn't create.

![demo placeholder](docs/demo.gif)

> **Status: early development.** Following the implementation plan in
> [`docs/plan/`](docs/plan/README.md) — not yet usable.

## Features

- Detects installed Steam games (more launchers planned) and lets you add
  any folder manually.
- Installs ReShade (normal or add-on support build) plus effect packages
  (SweetFX, standard shaders, etc.) and add-ons, downloaded from official
  sources and cached locally.
- Records every file it writes per game/executable so uninstall removes
  only what it installed — never touches your own files.
- Cache manager: list, sort and delete cached downloads; see how much space
  they use.
- Custom content: drop your own shaders or add-ons into a cache folder and
  YARM offers them in the install wizard.
- On Linux/Proton, automatically fetches and installs `d3dcompiler_47.dll`
  alongside ReShade — no manual DLL overrides needed for D3D9/10/11 games.

## Install

- **Windows / Linux**: download the latest zip/tar.gz from the
  [Releases](https://github.com/secato/yarm/releases) page and run the
  `yarm` binary.
- **Arch Linux**: `yay -S yarm-bin`
- **From source**: `go install github.com/secato/yarm/cmd/yarm@latest`

## Quick start

Run `yarm` with no arguments to launch the TUI. Key bindings are shown in the
help overlay (`?`).

## How it works

YARM keeps three things on disk (see [`docs/plan/03-data-and-storage.md`](docs/plan/03-data-and-storage.md)
for exact paths and layout):

- **Config** (`config.yaml`) — your preferences and manually added games.
- **State** (`installs.json`) — a manifest of what YARM installed, per game
  and executable, so uninstall is exact and reversible.
- **Cache** — downloaded ReShade builds, effect packages, add-ons and the
  Windows D3D compiler, reused across every game you install to.

## Linux / Proton notes

- `d3dcompiler_47.dll` is downloaded and copied into the game folder
  automatically — Wine's built-in one fails on some ReShade shaders.
- Vulkan and D3D8 games are not supported yet; the installer will say so.
- YARM never asks you to add Steam launch options.
- **Troubleshooting**: if a future Wine/Proton build stops picking up the
  game-folder DLL automatically, set a launch option:
  `WINEDLLOVERRIDES="d3dcompiler_47=n;dxgi=n,b" %command%`
  (adjust the second DLL name to whatever graphics API the game uses).

## Custom content

Drop your own shaders or add-ons into the cache's `custom/` folder and they
show up in the install wizard:

```
custom/
├── shaders/<name>/Shaders/*.fx  Textures/*
└── addons/<name>/*.addon32|*.addon64
```

See [`docs/plan/03-data-and-storage.md`](docs/plan/03-data-and-storage.md) §3.5 for details.

## Config reference

See [`docs/plan/03-data-and-storage.md`](docs/plan/03-data-and-storage.md) §3.2 for the full
`config.yaml` reference.

## FAQ

**Is this safe to use with online/anti-cheat-protected games?**
No. Never install ReShade (or any third-party DLL) into a game with an
active anti-cheat while playing online — it can get you banned. Use it only
for offline/single-player games or games without kernel-level anti-cheat.

## Credits

- [ReShade](https://reshade.me/) by crosire.
- Effect package and add-on catalogs sourced from the
  [reshade-shaders](https://github.com/crosire/reshade-shaders) `list` branch.
- Inspired by [LeShade](https://github.com/Ishidawg/LeShade) and
  [reshade-steam-proton](https://github.com/kevinlekiller/reshade-steam-proton-installer).
- Install/uninstall UX inspired by
  [DLSS5oneclick](https://github.com/faisalkindi/DLSS5oneclick).

## License

[MIT](LICENSE)
