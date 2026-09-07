# Contributing to YARM

## Setup

- Go 1.25+ (see `go.mod`).
- `golangci-lint` for linting (see `.golangci.yml`).

## Workflow

```sh
make lint test
```

Both must pass before opening a PR. `make cover` prints per-package coverage;
`internal/` non-TUI packages should stay at or above 75%.

## Commit messages

This repo uses [Conventional Commits](https://www.conventionalcommits.org/):
`feat:`, `fix:`, `chore:`, `docs:`, `test:`, `refactor:`, etc. Keep the
subject line under ~70 characters; explain *why* in the body when it isn't
obvious from the diff.

## Project layout

The package layout and dependency rules:

- `internal/app` (the TUI) is the only package allowed to depend on
  everything else. Nothing else imports `app`.
- Packages take their dependencies as interfaces/structs passed in — no
  globals — so they're testable with temp dirs and `httptest`.
- Cross-platform code goes through `internal/fsutil` and `internal/paths`;
  never concatenate paths with strings, always `filepath`.

## Adding a game-library provider

Steam is the only provider in v1. To add another (GOG/Heroic/Epic/Lutris):

1. Implement the `platform.Provider` interface in a new
   `internal/platform/<name>/` package.
2. Register it alongside `steam` and `manual` in `internal/platform`.
3. Add fixtures under `testdata/` for whatever manifest format the launcher
   uses, and unit tests exercising provider discovery against them.

`internal/platform/steam` is the worked example: roots per OS in
`roots_*.go`, manifest parsing beside it, and every path it reads covered
by a fixture under `testdata/`.

## Tests

- Unit tests use the standard `testing` package, `net/http/httptest` for
  fake catalog/download servers, and `github.com/google/go-cmp` for diffs.
- TUI screens use `github.com/charmbracelet/x/exp/teatest/v2` golden tests.
- Never hit real network services in a normal test run. Tests that need the
  real internet (e.g. downloading a real ReShade release) must be gated
  behind `YARM_NETWORK_TESTS=1` and skipped otherwise.

## The screenshot

`docs/main-screen.png` is the games list in the README. Retake it whenever
the layout changes enough that it misleads: run yarm in a terminal around
160×40 with a warm cache, on a real library, and crop to the terminal.

## Releasing

Releases are cut by pushing a tag; nothing else publishes. `.goreleaser.yaml`
builds linux/windows/darwin archives, checksums and the AUR `PKGBUILD`, and
`.github/workflows/release.yml` runs it for a pushed `v*` tag only — after
re-running build and tests on the tag itself, since the binaries come from
the tag rather than from whatever branch it points at.

Check the config and build everything locally without publishing:

```sh
go install github.com/goreleaser/goreleaser/v2@latest
goreleaser check
make snapshot            # goreleaser release --snapshot --clean → dist/
```

A snapshot in a repo with no tags reports the version as `0.0.1-next` and
leaves `Commit`/`Date` empty in `yarm version`; that is the no-tags
fallback, not a config problem. Tag first to see real values.

To release:

1. Move everything under `## [Unreleased]` in `CHANGELOG.md` into a
   version heading, and commit.
2. `git tag v0.1.0 && git push origin v0.1.0`.
3. Watch the Release workflow. It creates the GitHub Release, uploads the
   archives and checksums, and pushes the `PKGBUILD` to the AUR.

### AUR, one time

The AUR is one git repo per package holding a `PKGBUILD`. `yarm-bin`
packages the prebuilt release binary; a from-source `yarm` package can come
later. GoReleaser pushes on each tag, and the first push creates the
package. Before the first non-prerelease tag:

1. Create an account on [aur.archlinux.org](https://aur.archlinux.org).
2. Generate a **passwordless** deploy key: `ssh-keygen -t ed25519 -f aur -N ""`.
3. Add `aur.pub` to the account's SSH keys.
4. Put the private key in the repo secret `AUR_KEY`.

Until that secret exists, keep tags prereleases (`v0.1.0-rc1`): `skip_upload:
auto` skips the AUR step for those, so the rest of the release still works.

Users then install with `yay -S yarm-bin`.
