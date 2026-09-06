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

See [`docs/plan/02-architecture.md`](docs/plan/02-architecture.md) for the
package layout and dependency rules. The short version:

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

See [`docs/plan/02-architecture.md`](docs/plan/02-architecture.md) and
[`docs/plan/04-external-sources.md`](docs/plan/04-external-sources.md) for
how the Steam provider is structured.

## Tests

- Unit tests use the standard `testing` package, `net/http/httptest` for
  fake catalog/download servers, and `github.com/google/go-cmp` for diffs.
- TUI screens use `github.com/charmbracelet/x/exp/teatest/v2` golden tests.
- Never hit real network services in a normal test run. Tests that need the
  real internet (e.g. downloading a real ReShade release) must be gated
  behind `YARM_NETWORK_TESTS=1` and skipped otherwise.

## Recording the demo

`docs/demo.tape` is a [vhs](https://github.com/charmbracelet/vhs) script that
drives the real TUI against a scratch `YARM_HOME`, so it does not depend on
what is installed on the recording machine:

```sh
yay -S vhs                       # or go install github.com/charmbracelet/vhs@latest
go build -o /tmp/yarm-demo ./cmd/yarm
vhs docs/demo.tape               # writes docs/demo.gif
```

Run yarm once beforehand so the catalog cache is warm — otherwise most of
the recording is a spinner.
