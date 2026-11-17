# Installing yarm

## Linux

```sh
curl -fsSL https://raw.githubusercontent.com/secato/yarm/main/install.sh | sh
```

[`install.sh`](../install.sh) downloads the latest release for your
architecture (x86_64 or arm64), checks it against the release's
`checksums.txt`, and installs `yarm` to `~/.local/bin`, or to `/usr/local/bin`
when run as root. It also adds YARM to your application menu: a desktop entry
and an icon under `~/.local/share` (`/usr/local/share` as root). It needs
`curl`, `tar` and `sha256sum` (or `shasum`).

**Updating:** run the same command again. It does nothing when the installed
version is already the latest; otherwise it swaps the binary in with an atomic
rename, so it is safe even while yarm is running.

### Options

| Variable / flag | Effect |
| --- | --- |
| `YARM_VERSION=v1.0.0` | install a specific release instead of the latest |
| `YARM_INSTALL_DIR=/some/dir` | install somewhere else |
| `--uninstall` | remove the binary, the desktop entry and the icon |

Environment variables go on the `sh` side of the pipe:

```sh
curl -fsSL https://raw.githubusercontent.com/secato/yarm/main/install.sh | YARM_INSTALL_DIR=~/bin sh
```

Flags go after `sh -s --`:

```sh
curl -fsSL https://raw.githubusercontent.com/secato/yarm/main/install.sh | sh -s -- --uninstall
```

Uninstalling removes only the binary, the menu entry and the icon. yarm's data directory
(`~/.local/share/yarm`) is kept on purpose: `installs.json` there is what lets
yarm remove ReShade from your games later.

If the install directory is not on your `PATH`, the script says so; add it in
your shell profile.

## From source

Requires Go (see `go.mod` for the version) and Linux:

```sh
go install github.com/secato/yarm/cmd/yarm@latest
```

Or from a clone, with version information stamped in:

```sh
mise run build    # → bin/yarm (or plain `go build ./cmd/yarm`)
```

## Requirements

Linux with glibc, x86_64 or arm64, and a desktop session (X11 or Wayland) with
working GPU drivers: the window is drawn on the GPU, so the binary needs the
system's graphics and windowing libraries at run time. musl distributions are not
supported. Steam Deck works in Desktop Mode.

YARM follows the desktop's dark or light setting (and the Omarchy theme). Set
`YARM_THEME=dark` or `light` to force one for a run.

## Where yarm keeps its files

| | |
| --- | --- |
| Config (`config.yaml`) | `~/.config/yarm/` |
| Data (`installs.json`, `yarm.log`, `custom/`) | `~/.local/share/yarm/` |
| Download cache | `~/.cache/yarm/` |

XDG variables are honored. Setting `YARM_HOME` puts all three under
one folder (`config/`, `data/`, `cache/`), which is handy for a portable copy.
