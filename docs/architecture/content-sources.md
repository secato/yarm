# Content sources

Where the things yarm installs come from, and how they reach the cache.

```
catalog  ── what exists (lists, versions, URLs)
   │
cache    ── Ensure*(): is it here? if not, fetch + normalize + index
   ├── fetch      the download itself
   ├── artifacts  turn the download into a normalized cache entry
   └── archive    safe zip extraction
```

## Catalogs (`internal/catalog`)

| Source | What | Caching |
| --- | --- | --- |
| crosire's `EffectPackages.ini` (`list` branch of reshade-shaders) | effect packages | static snapshot |
| crosire's `Addons.ini` (same branch) | add-ons | static snapshot |
| GitHub tags of `crosire/reshade` | ReShade versions | static snapshot |
| `reshade.me` front page | which version is "latest" | never cached |
| RenoDX release metadata | RenoDX mods | TTL (`renodx_ttl_hours`, default 7 days) |
| `<data>/custom/` | user's own shaders and add-ons | scanned each time |

The static catalogs are trusted **indefinitely** once fetched and refreshed
only on request (`Client.RefreshCatalog`, the rescan key), so starting yarm
never waits on the network. Every read falls back to the cached copy — even
an expired one — when the network fails, so yarm keeps working offline.

`ini.go` is a small ini parser for crosire's files; `slug.go` turns package
names into stable ids (`Slugify`, de-duplicated per load).

### RenoDX (`renodx.go`)

RenoDX publishes its own JSON index rather than appearing in crosire's lists.
`ParseRenoDX` accepts known schema versions only (`UnsupportedSchemaError`
otherwise) and appends a few hand-kept utility mods the index doesn't cover.
`RenoMod.ArtifactFor` is an **exact** architecture match — a wrong-arch add-on
does nothing, so there is no 64-bit fallback like `Addon.SourceFor` has.

## Hand-kept tables (`internal/app`)

Upstream carries neither popularity nor dependency data, so two small tables
are maintained by hand. Neither should be derived from the catalog.

- **`curated.go`** — the shortlist the wizard opens on. Add a pack when people
  ask for it by name. Everything else stays one keypress away (show all).
- **`requires.go`** — "X needs Y" edges (AutoHDR needs a tone-mapping shader,
  TFAA needs iMMERSE's LAUNCHPAD, …). Upstream only states these in prose
  inside descriptions; a five-entry table beats a prose parser.

## The cache (`internal/cache`)

One root, shared across games — installing a pack into a fifth game copies
from the cache, it does not download again.

| Dir | Contents | Key |
| --- | --- | --- |
| `reshade/<version>/<flavor>/` | `ReShade32.dll`, `ReShade64.dll` | version + flavor |
| `packages/<id>/<date>-<sha7>/` | normalized `Shaders/`, `Textures/`, `package.json` | download date + zip hash |
| `addons/<id>/<date>-<arch>/` | `.addon32` / `.addon64` | date + arch |
| `renodx/<id>/<date>-<arch>/` | the RenoDX add-on | date + arch |
| `d3dcompiler/<arch>/` | `d3dcompiler_47.dll` | arch (pinned hash) |
| `downloads/` | in-flight files, deleted after normalizing | — |
| `catalog/` | catalog snapshots + `.meta.json` sidecars | — |

Each `Ensure*` method returns the entry's directory, downloading and
normalizing only when it is missing. Package and add-on keys carry a date
because their upstream URLs are unversioned (branch archives, RenoDX's
rolling `snapshot` tag): a dateless key would pin whatever was fetched first,
forever.

The index (`index.go`) is bookkeeping only: a corrupt index is rebuilt, not an
error (contrast with `installs.json`). `List` re-measures sizes from disk and
drops vanished entries. `Delete` / `Clean*` refuse any path outside the cache
root (`withinRoot`), however the index came to hold it. `touch` throttles
last-used writes to once an hour.

## Artifacts (`internal/artifacts`)

- **ReShade** — the setup `.exe` is a .NET PE with a zip appended; Go's
  `archive/zip` reads it directly. Both DLLs must be present.
- **Packages** — repo zips vary (a `<repo>-<branch>/` wrapper, or bare `.fx`
  at the top). `NormalizePackage` maps every layout to `Shaders/` +
  `Textures/` and writes `package.json` with the catalog's install rules
  (`EffectFiles`, `DenyEffectFiles`), so the install engine never needs the
  catalog.
- **Add-ons** — a bare binary or a zip; only the architecture's files are kept.
- **`d3dcompiler_47.dll`** — needed only for Linux/Proton installs
  (`NeedsD3DCompiler`), extracted from a pinned Firefox installer (a 7z
  self-extractor) and verified by both installer and DLL SHA-256.
- **`TargetOS`** — the OS an install targets, passed as a value so tests run
  both branches anywhere. `CurrentTargetOS` is the one `runtime.GOOS` read.

## Fetch (`internal/fetch`)

- **https only**, on every redirect hop (`EnsureSecureURL`, `CheckRedirect`).
  Package and add-on URLs come verbatim from a downloaded ini, so this is the
  boundary that stops a hostile catalog choosing `http`. Plain http to
  loopback is allowed so tests can use `httptest`.
- Downloads land in `<dest>.part` and are renamed only once complete and
  verified; `cache.CleanPartials` sweeps leftovers at startup.
- Retries, progress callbacks, optional SHA-256 verification.

## Archive (`internal/archive`)

Zip extraction that **rejects** rather than sanitizes: `SafePath` refuses any
entry that would escape the destination (`..`, absolute, drive letters, UNC),
after normalizing backslashes. `Budget` enforces size and entry-count limits
against both declared and actually-written bytes, and every entry is written
as a regular file (no symlinks).
