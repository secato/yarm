<div align="center">

<img src="docs/logo.png" alt="YARM" width="220">

# YARM — Yet Another ReShade Manager

**Install ReShade in your games from the terminal, and take it back out just as easily.**

[![CI](https://github.com/secato/yarm/actions/workflows/ci.yml/badge.svg)](https://github.com/secato/yarm/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/secato/yarm)](https://github.com/secato/yarm/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/secato/yarm)](https://goreportcard.com/report/github.com/secato/yarm)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

</div>

YARM finds your games, installs [ReShade](https://reshade.me/) into the ones you
choose along with the shaders and add-ons you want, and keeps track of every
file it put there — so uninstalling gives you the folder back exactly as it was.

It is a single binary with no runtime, no installer and no launcher running in
the background. It runs on **Windows and Linux** (including Steam Deck and
Proton), where it does the fiddly Proton-specific parts for you, and it works
over SSH just as well as it does locally.

<div align="center">

<img src="docs/main-screen.png" alt="The games list, with a game's install details in the side panel" width="900">

</div>

> **Status: pre-release.** Everything described here works, but there is no
> tagged release yet.

## What it does for you

**Finds your games.** Steam libraries are detected automatically, wherever they
live. Anything else — GOG, Epic, a folder you unzipped somewhere — you add by
path, once.

**Installs ReShade the right way for each game.** YARM looks at the game's
executable to work out whether it is 32- or 64-bit and which graphics API it
uses, then picks the matching ReShade build and proxy DLL for you. You can
override any of it if you know better.

**Gives you the shaders people actually use.** The wizard opens on a curated
shortlist — SweetFX, iMMERSE, qUINT, METEOR, LumeniteFX and the rest — instead
of a wall of a hundred packages. One key widens it to the full catalog when you
want something specific.

**Handles dependencies for you.** Some add-ons need a particular shader to work
at all. Pick the AutoHDR add-on and YARM selects the tone-mapping shader it
needs, and tells you why it did.

**Downloads once, not once per game.** Everything is cached and shared, so
installing the same shader pack into a fifth game copies files rather than
re-downloading them. You can see what is cached, what it costs in disk space,
and which games are using it.

**Uninstalls cleanly.** YARM records every file it writes. Uninstall removes
exactly those and nothing else — your presets, screenshots and edited configs
stay. If it had to replace a file that was already there, it kept a copy and
puts it back.

**Tells you before it overwrites anything.** If something else already occupies
the DLL slot ReShade needs — another injector, or a ReShade you installed by
hand — YARM says so before it starts, not after.

**Takes over installs you did by hand.** Point it at a game you set up
yourself and it can adopt that install and manage it from then on.

**Lets you bring your own.** Drop your own shaders or add-ons into a folder and
they appear in the wizard next to the catalog ones.

**Warns you about anti-cheat.** The add-on build of ReShade is detectable.
YARM says so, in red, on every screen where it matters.

## Installing YARM

| | |
| --- | --- |
| **Windows / Linux** | Download the zip or tar.gz from [Releases](https://github.com/secato/yarm/releases) and run `yarm`. There is nothing to install. |
| **Arch Linux** | `yay -S yarm-bin` |
| **From source** | `go install github.com/secato/yarm/cmd/yarm@latest` |

## Using it

Run `yarm`. Every screen lists its keys along the bottom, and `?` opens the
full help.

| Key | Does |
| --- | --- |
| `↑` `↓` `←` `→` | move (arrows only — every letter is an action) |
| `enter` | open, or advance the wizard a step |
| `esc` | back a step, or back a screen |
| `i` / `e` / `u` | install, edit an install, uninstall |
| `a` | adopt a hand-made install (games list) · show the whole catalog (wizard) |
| `c` / `x` / `s` | resources · custom content · settings |
| `/` `r` | filter · rescan |
| `q` | quit |

Nothing is written to a game folder until you confirm on the review page, which
lists every file that is about to change.

If you would rather not use the UI, `yarm games ls`, `yarm cache ls` and
`yarm installs` print the same information (with `--json` where it helps), and
`--no-color` — or `NO_COLOR=1` — turns off styling.

## On Linux and Proton

- `d3dcompiler_47.dll` is downloaded and installed for you. Wine's own copy
  fails on some ReShade shaders, and this is the step most manual guides get
  wrong.
- You do **not** need to add Steam launch options or set DLL overrides.
- Vulkan and D3D8 games are not supported yet. YARM will tell you rather than
  install something that cannot work.
- If a future Proton version stops picking up the DLL on its own, this launch
  option fixes it — replace `dxgi` with whichever API the game uses:
  `WINEDLLOVERRIDES="d3dcompiler_47=n;dxgi=n,b" %command%`

## Adding your own shaders and add-ons

Anything you put in the cache's `custom/` folder shows up in the wizard beside
the catalog packages:

```
custom/
├── shaders/<name>/Shaders/*.fx  Textures/*
└── addons/<name>/*.addon32|*.addon64
```

`yarm paths` prints where that folder is on your system.

## A word of warning

**Never install ReShade — or any third-party DLL — into a game with an active
anti-cheat while playing online.** It can get your account banned. Use it for
single-player and offline games, or games you are certain allow it. The add-on
build is the most detectable of all, which is why YARM never selects it for you.

## Documentation

The design documents live in [`docs/plan/`](docs/plan/README.md) and cover the
architecture, the exact on-disk layout of the config, state and cache, the
upstream sources YARM reads, and how the install engine works.
[`CONTRIBUTING.md`](CONTRIBUTING.md) covers building and testing.

## Credits

- [ReShade](https://reshade.me/) by crosire.
- Effect package and add-on catalogs from the
  [reshade-shaders](https://github.com/crosire/reshade-shaders) `list` branch.
- Inspired by [LeShade](https://github.com/Ishidawg/LeShade) and
  [reshade-steam-proton](https://github.com/kevinlekiller/reshade-steam-proton-installer).
- Install/uninstall UX inspired by
  [DLSS5oneclick](https://github.com/faisalkindi/DLSS5oneclick).

## License

[MIT](LICENSE)
