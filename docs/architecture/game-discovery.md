# Game discovery (`internal/platform`, `internal/game`)

## Providers

```go
type Provider interface {
    Name() string
    Discover(ctx context.Context) ([]game.Game, error)
}
```

- A provider that finds nothing (store not installed) returns an **empty
  slice, not an error**.
- `platform.DiscoverAll` runs every provider and joins their errors, so one
  broken provider never hides the others' games.
- `buildProviders` in `cmd/yarm/main.go` is the **only** place providers are
  registered. The store providers can each be disabled in `config.yaml`;
  `manual` is always on.

| Provider | Windows source | Linux source |
| --- | --- | --- |
| `steam` | registry → `libraryfolders.vdf` + `appmanifest_*.acf` | same files under the Steam roots |
| `gog` | per-game registry keys (Galaxy and offline installers) | Heroic, Lutris |
| `epic` | the launcher's manifest files | Heroic (via its bundled legendary's `installed.json`), Lutris |
| `battlenet` | the agent's `product.db` (protobuf) in ProgramData | the same `product.db` inside a wine prefix: Lutris-managed prefixes, or a fixed prefix path some distro installers use |
| `manual` | `manual_games` in `config.yaml` | same |

Per-OS code is split by build tags (`roots_linux.go`, `roots_windows.go`, …).

### Store-centric ids

The store providers produce the **same game id however the game was found**:
`gog:<product id>`, `epic:<app name>`, `battlenet:<uid>`. The same GOG game
seen in the registry, Heroic and Lutris is one game, not three
(`sources.Dedupe` keeps the first). Steam uses `steam:<appid>`; manual games
`manual:<sha1(root)[:12]>`.

### Launcher sources (`internal/platform/sources`)

On Linux, stores without a native client are reached through the launchers
that manage them. `sources/heroic` and `sources/lutris` are **read-only
readers** that normalize launcher metadata into one `sources.Entry`
(store, store id, title, root); the store providers filter by store and
convert with `Entry.Game`, which refuses empty ids and roots that are not
absolute or no longer exist.

`lutris` opens `pga.db` with the pure-Go `modernc.org/sqlite` driver in
`mode=ro`. CGO is off for static builds, and the database belongs to Lutris —
yarm must not write to it or leave `-wal`/`-shm` files beside it.
`lutristest` builds real `pga.db` fixtures through SQLite itself.

### Adding a provider

1. New package `internal/platform/<name>/` implementing `Provider`.
2. Per-OS discovery behind build tags; reuse `sources` readers on Linux if a
   launcher already records the store's games.
3. Append it in `buildProviders`, behind an `enabled` toggle in `config`
   (a `ProviderConfig`, on by default).
4. Fixtures under `testdata/` (or a generated fixture like `lutristest`) and
   tests against them — never against the real machine.

`internal/platform/steam` is the worked example.

## Executables (`internal/game`)

- **`game.Scan`** walks a game root up to depth 4 collecting `*.exe`. It does
  not descend into redistributable / anti-cheat / tool directories. Exes whose
  *names* look like non-games (uninstallers, setup, crash reporters) are still
  returned with `Skipped` set, so the UI can offer "show all" rather than hide
  them.
- **`game.HasNativeBuild`** sniffs for ELF / Mach-O binaries, only when no
  `.exe` was found — ReShade needs the Windows build, so this explains an
  empty list instead of guessing.
- **`game.Inspect`** (`pe.go`) parses the PE header for architecture (x86 /
  x64) and guesses the graphics API from imported DLLs. That picks the
  ReShade DLL (`ReShade32/64.dll`) and the proxy name via
  `API.RecommendedDLL`: `d3d9.dll`, `dxgi.dll` (D3D10/11/12),
  `opengl32.dll`.
  - It uses `ImportedSymbols`, because `debug/pe`'s `ImportedLibraries` is a
    stub.
  - It recovers from panics: malformed import directories (some packers)
    have crashed `debug/pe` before, and one bad exe must not take down the
    games list.
- **D3D8 and Vulkan are recognized but deliberately unsupported**
  (`API.Supported`).

## Folders, not executables

ReShade loads by directory, so the UI groups executables by folder
(`FolderGroup` in `internal/app/games_data.go`). A folder with several
candidate exes of different arch/API makes the wizard ask which one decides
the build. A game with a 32- and a 64-bit tree can be set up in one wizard
pass (one `folderOp` per folder in `internal/app/pipeline.go`).
