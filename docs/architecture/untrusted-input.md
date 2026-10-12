# Untrusted input

Almost everything yarm reads comes from somewhere it does not control:
crosire's ini files, the RenoDX index, GitHub and reshade.me, launcher
databases, Steam manifests, ReShade's own logs inside game folders, and its
own cache and registry files, which any process on the machine can edit.
Whatever yarm downloads ends up as a DLL loaded into a game, and whatever it
reads may end up deciding what gets deleted.

The rule: **bound and clean at the parse boundary**, so everything
downstream — the TUI, the log, `installs.json` — holds data that is already
safe.

| Threat | Defense | Where |
| --- | --- | --- |
| Tampered downloads in transit | https only, on every redirect hop; plain http only to loopback (tests) | `fetch.EnsureSecureURL`, `CheckRedirect` |
| Swapped binary at a pinned URL | SHA-256 of installer and DLL | `artifacts/d3dcompiler.go` |
| Half-written downloads trusted later | `.part` file, renamed only when complete and verified | `fetch` |
| Zip slip | reject (don't sanitize) `..`, absolute, drive-letter and UNC entries | `archive.SafePath` |
| Zip bombs | declared and written byte limits, entry count, regular files only | `archive.Budget` |
| Oversized catalogs or cache files | capped reads (`maxCatalogBytes`, `fsutil.ReadFileMax`) | `catalog/client.go`, readers in `platform/*`, `install/inspect.go` |
| Terminal escapes in names (game titles, package descriptions, log banners) | strip control / escape characters where parsed | `safetext.Clean` in `catalog`, `platform`, `install/inspect.go`; `clipTail` |
| A catalog entry steering filenames | extension allowlist; ids slugified | `cache.urlExt`, `catalog.Slugify` |
| Destinations escaping the game root | resolved and checked before any write | `install.safeDest` |
| Forged or corrupted `installs.json` | full validation on load and save; per-origin shape check before every delete; hash must still match | `state.Registry.Validate`, `install.deletableShape`, `Uninstaller.Run` |
| Cache index pointing outside the cache | refuse deletes outside the root | `cache.withinRoot` |
| Steam `installdir` with path components | must be a plain folder name | `steam.safeInstallDir` |
| Writing into launcher-owned files | read-only opens (`pga.db` with `mode=ro`) | `sources/lutris` |

When adding a parser for a new external format: cap its size, run display
strings through `safetext.Clean`, and treat any path it yields as hostile
until it has been checked against the root it must stay inside.
