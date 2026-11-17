# Testing

```sh
mise run test            # go test -race -coverprofile=cover.out ./...
mise run cover           # per-package coverage table
mise run lint            # golangci-lint: gofumpt, govet, staticcheck, revive (exported), errcheck, misspell, unused
mise run test-network    # adds the real-internet tests (YARM_NETWORK_TESTS=1)
```

`mise run check` must pass before a commit, and every `internal/` package
stays at or above **75% coverage**. CI runs the tests with `-race` on Ubuntu,
plus lint.

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
- **Fakes at the interface seams.** The `core` and `ui` tests swap `Installer`,
  `UninstallRunner`, `AdoptRunner`, `WizardDataLoader` and `GamesLoader` for
  fakes (`internal/core/testfixtures_test.go`, `internal/ui/ui_test.go`).

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

## Window tests

`internal/core` is tested without a window: the `Session`, the apply runner, the report and
resources are plain Go over temp dirs and fakes.

`internal/ui` is driven headlessly with MyGo's `ui.NewTester` at 1180×860: `Click("text")`
finds the first element with that text or label, `HasText` and `Texts` read what is on
screen. `testApp` builds an `App` with no window and runs background work inline, so a click
and its consequences are over when `Click` returns. There are **no golden files**: layouts
change often enough that a committed picture gets re-blessed rather than read, so tests assert
the few strings and model values that matter.

Two ways to look at the result:

- `YARM_SHOTS=<dir> go test ./internal/ui` writes a PNG of each board (`Tester.Image()`).
- `mise run screenshots` regenerates `docs/screenshot.png` and `docs/screenshot-light.png`.

Switches slide and dialogs fade, so `shot` advances the tester's clock before it draws.
