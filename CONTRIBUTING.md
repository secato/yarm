# Contributing to YARM

## Setup

- Go 1.25+ (see `go.mod`).
- `golangci-lint` for linting (see `.golangci.yml`).

## Workflow

```sh
make lint test
```

Both must pass before opening a PR. `make cover` prints per-package coverage;
every `internal/` package should stay at or above 75%.

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

Steam, GOG, Epic, Battle.net and manual entries exist today. To add another:

1. Implement the `platform.Provider` interface in a new
   `internal/platform/<name>/` package.
2. Add it to `buildProviders` in `cmd/yarm/main.go`, alongside `steam` and
   `manual`. That function is the single place providers are registered;
   `internal/platform` holds only the interface and `DiscoverAll`, not a
   registry.
3. Add fixtures under `testdata/` for whatever manifest format the launcher
   uses, and unit tests exercising provider discovery against them.

`internal/platform/steam` is the worked example: roots per OS in
`roots_*.go`, manifest parsing beside it, and every path it reads covered
by a fixture under `testdata/`.

## Tests

- Unit tests use the standard `testing` package, `net/http/httptest` for
  fake catalog/download servers, and `github.com/google/go-cmp` for diffs.
- TUI screens are driven headlessly with
  `github.com/charmbracelet/x/exp/teatest/v2` at a pinned terminal size and
  `colorprofile.NoTTY`, asserting on the rendered frame. There are no golden
  files: a screen's layout changes often enough that a committed frame would
  be re-blessed rather than read, so tests assert the few things that
  actually matter in the output.
- Never hit real network services in a normal test run. Tests that need the
  real internet (e.g. downloading a real ReShade release) must be gated
  behind `YARM_NETWORK_TESTS=1` and skipped otherwise.

## The screenshot

`docs/main-screen.png` is the games list in the README. Retake it whenever
the layout changes enough that it misleads: run yarm in a terminal around
160×40 with a warm cache, on a real library, and crop to the terminal.

## Releasing

Releases are cut by pushing a tag; nothing else publishes. `.goreleaser.yaml`
builds linux/windows archives and checksums, and
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
3. Watch the Release workflow. It creates the GitHub Release and uploads the
   archives and checksums. `install.sh` picks the new release up as soon as
   it is published (prereleases excepted).

### Linux install script

`install.sh` at the repo root is what the README tells Linux users to pipe
into `sh`. It depends on the release asset names (`yarm_<version>_linux_<arch>.tar.gz`
and `checksums.txt`) and on `yarm -version` printing the bare version as its
second word, so changing either `name_template` in `.goreleaser.yaml` or
`buildinfo.String` breaks it. It resolves "latest" through GitHub's
`/releases/latest` redirect, which skips prereleases.

### AUR

The `aurs:` block in `.goreleaser.yaml` is ready but dormant: AUR account
registration is closed, so there is no `AUR_KEY` secret. Without it the AUR
step would fail a stable tag, so it has `skip_upload: true`. Once an account
exists, add a passwordless deploy key
(`ssh-keygen -t ed25519 -f aur -N ""`) to the account, store the private key
as the `AUR_KEY` repo secret, and restore `skip_upload: auto`.
