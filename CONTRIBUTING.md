# Contributing to YARM

## Setup

- Go 1.26+ (see `go.mod`).
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

## How the code is organized

See [docs/architecture/](docs/architecture/README.md): the layering rules,
the install engine, game providers (including
[adding one](docs/architecture/game-discovery.md#adding-a-provider)), the TUI,
and [testing conventions](docs/architecture/testing.md).

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
