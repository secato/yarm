# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0-alpha.5] — 2026-09-14

### Fixed

- **GOG and Epic discovery tests now actually run on Windows.** Alpha.4 gated
  them behind a Linux-only build tag, which also hid their native-discovery
  coverage (Windows registry, manifest files) — the opposite of the goal. The
  real bug was a test-fixture helper embedding raw Windows paths into JSON
  string literals, producing invalid JSON; fixed by escaping the path first.

## [0.1.0-alpha.4] — 2026-09-14

### Fixed

- **Windows test compatibility.** Tests for Linux-specific game launcher integration
  (Heroic, Lutris) now skip on non-Linux platforms. Fixed cache test that used
  invalid Windows path characters.

## [0.1.0-alpha.3] — 2026-09-14

### Added

- **Demand-driven dependency resolution.** ReShade, packages, and add-ons are
  now resolved only when needed, showing an honest download list before
  confirming the install. Package search is faster and the wizard steps are
  clearer about what will be fetched.

### Changed

- **Platform column wider.** The games list now gives more space to platform
  names so longer ones (Battle.net) display fully.

### Fixed

- **Architecture selection improved.** yarm now defaults to x64 when both
  architectures are available, and the wizard's Paths step lets you override
  that choice.

## [0.1.0-alpha.2] — 2026-09-11

The first release, and an alpha. Everything below works and is covered by
tests on Linux and Windows, but yarm has not yet been used by anyone other
than its author — which is what the alpha is for. It records every file it
writes, so the way out of a bad install is the uninstall screen.

### Added

- **Game discovery.** Steam libraries are found automatically on Windows and
  Linux, wherever they live, by reading Steam's own `libraryfolders.vdf` and
  `appmanifest_*.acf`. Anything else — GOG, Epic, a folder unzipped
  somewhere — is added by path. Discovery is cached for the session so the
  games list is instant after the first scan.

- **Executable inspection.** yarm reads a game's PE headers to work out
  whether it is 32- or 64-bit and which graphics API it imports, then
  preselects the matching ReShade build and proxy DLL. Every choice can be
  overridden.

- **An install wizard.** ReShade version and flavor, effect packages,
  add-ons, RenoDX, and paths — each a step, each explaining what it picked
  for you and why. The package step opens on a hand-kept shortlist of what
  people actually install, with the full catalog one key away. Add-on
  dependencies are resolved and named ("AutoHDR needs this tone-mapping
  shader").

- **A review page before anything is written.** It shows what is already in
  the folder, what will happen to it, what still needs downloading, and how
  much. Nothing touches a game directory until that page is confirmed.

- **A three-phase install engine.** Plan, execute, record: every file yarm
  writes is recorded in `installs.json` with its hash and origin, a file it
  had to replace is backed up first, and a failure part-way through rolls
  the folder back to where it started.

- **Uninstall that gives the folder back.** It removes exactly the recorded
  files, restores what it replaced, and leaves presets, screenshots and
  edited configs alone. An install made by hand can be adopted and then
  managed like any other.

- **RenoDX**, which rewrites a game's shaders to improve its HDR. yarm reads
  RenoDX's own index, matches the mod to the game by Steam app id, offers
  that one first and the other ~200 behind search, and says when a mod has
  no build for the game's architecture or needs a newer ReShade than the
  one selected.

- **A shared download cache.** Installing the same shader pack into a fifth
  game copies files instead of downloading them again. The resources screen
  shows what is cached, what it costs on disk, which games use it, and
  deletes any of it.

- **Custom content.** Shaders and add-ons dropped into yarm's `custom/`
  folder appear in the wizard beside the catalog ones.

- **Warnings where they matter.** Something else already in ReShade's DLL
  slot is reported before the install starts, not after; the add-on build is
  flagged as anti-cheat-detectable in red on every screen it appears on; and
  an unsupported API is said out loud while you are still choosing rather
  than left to be discovered from a game that starts without ReShade.

- **A single binary** for Linux and Windows, with no runtime, no installer
  and nothing running in the background. Three flags: `-debug`, `-no-color`,
  `-version`.

### Known limitations

- **Vulkan and D3D8 are not supported.** yarm says so in the wizard rather
  than installing something that cannot work.
- **Steam is the only library detected automatically.** Everything else is
  added by path; providers for GOG, Epic, Heroic and Lutris are planned.
- **Windows is the less-exercised platform.** The build and the full test
  suite run on it in CI, but development happens on Linux.
- **Nothing yarm downloads is signed or checksummed**, because nothing
  upstream publishes signatures. Downloads are https-only on every redirect
  hop, archive contents are restricted to the file types a shader pack is
  made of, and every path is contained — but the trust is in the upstream
  repositories, and that is worth knowing before installing into a game.
- **No self-update.** Check the releases page.

[0.1.0-alpha.2]: https://github.com/secato/yarm/releases/tag/v0.1.0-alpha.2
