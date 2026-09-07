# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- The wizard's ReShade step says where a preselected build came from when
  it was not the safe default: `defaults.reshade_flavor` in the config, or
  the install already in the folder. A config written before that default
  changed would silently preselect the add-on build with nothing on screen
  explaining why.

### Changed

- Custom shaders and add-ons now live in the data directory
  (`~/.local/share/yarm/custom`, `%LOCALAPPDATA%\yarm\custom`) instead of
  under the cache. Everything else in the cache can be deleted and
  re-downloaded; custom content is placed by hand and cannot be, and
  `~/.cache` is a directory the XDG spec, cleanup tools and users all treat
  as disposable. An existing `cache/custom` is moved on the next launch,
  and `yarm paths` now prints the folder.

- Releases build for Linux and Windows only. ReShade injects into Windows
  games, which macOS does not run; the darwin binary built fine but could
  only ever disappoint whoever downloaded it.
