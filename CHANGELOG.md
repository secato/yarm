# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Removed

- The command line interface. yarm is a terminal application, and a second
  way to drive it was a second surface to document, test and keep honest
  for no one's benefit: the subcommands existed so each build step could be
  demonstrated before the TUI could. `games ls`, `catalog ls`, `cache
  ls|clean`, `fetch`, `install`, `uninstall`, `installs`, `paths` and
  `version` are gone, and with them cobra as a dependency — `main.go` went
  from 1249 lines to 277.

  What is left is `-debug`, `-no-color` and `-version`, parsed with stdlib
  `flag`, because a binary that ignores `--version` or `--help` is a
  nuisance to package. `--verbose` is gone too: it streamed the log to
  stderr, which corrupts the alternate screen the TUI draws in. The
  resolved directories `yarm paths` used to print are now logged at
  startup, so `yarm.log` still answers the question it answered.

### Added

- `X` on the resources browser clears the whole download cache, which used
  to be `yarm cache clean` and had no equivalent in the interface. The
  confirmation counts what will go and how much it frees, warns when some
  of it backs a recorded install, and says plainly that installed games
  keep their files and that custom content is not in the cache. `x` still
  deletes the single row under the cursor.

- The install result screen states how much was written, not just how many
  files — the only place an install's footprint on disk is reported.

- The wizard's ReShade step says where a preselected build came from when
  it was not the safe default: `defaults.reshade_flavor` in the config, or
  the install already in the folder. A config written before that default
  changed would silently preselect the add-on build with nothing on screen
  explaining why.

### Fixed

- On Windows, a rejected archive entry left its partial file on disk. The
  extractor removed the file while its own handle was still open, which
  Unix permits and Windows refuses — so the truncated artifact the entry
  budget exists to refuse survived, silently, on the platform yarm is
  mainly for. The handle is closed before the unlink now.

- The status bar is clipped to the window instead of wrapping. On screens
  with many key bindings it ran past the terminal width, and the extra
  rows made the frame taller than the window — which scrolled the header
  off the top. A screen with more bindings than fit now loses the tail of
  the list rather than the layout; `?` still lists all of them.

### Changed

- Custom shaders and add-ons now live in the data directory
  (`~/.local/share/yarm/custom`, `%LOCALAPPDATA%\yarm\custom`) instead of
  under the cache. Everything else in the cache can be deleted and
  re-downloaded; custom content is placed by hand and cannot be, and
  `~/.cache` is a directory the XDG spec, cleanup tools and users all treat
  as disposable. An existing `cache/custom` is moved on the next launch.

- Releases build for Linux and Windows only. ReShade injects into Windows
  games, which macOS does not run; the darwin binary built fine but could
  only ever disappoint whoever downloaded it.
