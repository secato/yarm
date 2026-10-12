# Testing

```sh
make test            # go test -race -coverprofile=cover.out ./...
make cover           # per-package coverage table
make lint            # golangci-lint: gofumpt, govet, staticcheck, revive (exported), errcheck, misspell, unused
make test-network    # adds the real-internet tests (YARM_NETWORK_TESTS=1)
```

`make lint test` must pass before a commit, and every `internal/` package
stays at or above **75% coverage**. CI runs the tests with `-race` on Ubuntu
and Windows, plus lint.

## Conventions

- Standard `testing`, `github.com/google/go-cmp` for diffs.
- **No real network in a normal run.** Catalog and download tests use
  `httptest` servers — `fetch` allows plain http to loopback for exactly this.
  Tests that need the real internet live in `network_test.go` files
  (`internal/catalog`, `internal/artifacts`) and skip unless
  `YARM_NETWORK_TESTS=1`.
- **No real machine state.** Everything takes its directories as values:
  temp dirs for game folders, `state`, `cache` and `config`; providers take
  explicit roots (`steam.NewWithRoots`) instead of probing the host.
- **Both OS branches everywhere.** Pass `artifacts.TargetOS` explicitly
  rather than relying on the OS running the test.
- **Fakes at the interface seams.** The TUI tests swap `Installer`,
  `UninstallRunner`, `AdoptRunner`, `WizardDataLoader` and `GamesLoader` for
  fakes (`internal/app/testfixtures_test.go`).

## Fixtures (`testdata/`)

| Dir | What |
| --- | --- |
| `pe/` | tiny PE images per arch/API, including a malformed import table; regenerate with `gen.go` |
| `steam/` | `libraryfolders.vdf` and `appmanifest_*.acf` |
| `catalog/` | `EffectPackages.ini`, `Addons.ini`, `renodx-metadata.json` snapshots |
| `7z/` | a small 7z archive standing in for the Firefox installer `d3dcompiler_47.dll` is extracted from |

Lutris fixtures are built at test time by
`internal/platform/sources/lutris/lutristest`, through SQLite itself, so the
readers see a genuine `pga.db`.

## TUI tests

Screens are driven headlessly with `charmbracelet/x/exp/teatest/v2` at a
pinned terminal size and `colorprofile.NoTTY`, asserting on the rendered
frame. There are **no golden files**: layouts change often enough that a
committed frame gets re-blessed rather than read, so tests assert the few
strings that actually matter. Many screen tests also call `Update` directly
with a message and inspect the returned screen and command.
