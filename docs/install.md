# Installing yarm

## Linux

```sh
curl -fsSL https://raw.githubusercontent.com/secato/yarm/main/install.sh | sh
```

[`install.sh`](../install.sh) downloads the latest release for your
architecture (x86_64 or arm64), checks it against the release's
`checksums.txt`, and installs `yarm` to `~/.local/bin`, or to `/usr/local/bin`
when run as root. It needs `curl`, `tar` and `sha256sum` (or `shasum`).

**Updating:** run the same command again. It does nothing when the installed
version is already the latest; otherwise it swaps the binary in with an atomic
rename, so it is safe even while yarm is running.

### Options

| Variable / flag | Effect |
| --- | --- |
| `YARM_VERSION=v1.0.0` | install a specific release instead of the latest |
| `YARM_INSTALL_DIR=/some/dir` | install somewhere else |
| `--uninstall` | remove the binary |

Environment variables go on the `sh` side of the pipe:

```sh
curl -fsSL https://raw.githubusercontent.com/secato/yarm/main/install.sh | YARM_INSTALL_DIR=~/bin sh
```

Flags go after `sh -s --`:

```sh
curl -fsSL https://raw.githubusercontent.com/secato/yarm/main/install.sh | sh -s -- --uninstall
```

Uninstalling removes only the binary. yarm's data directory
(`~/.local/share/yarm`) is kept on purpose: `installs.json` there is what lets
yarm remove ReShade from your games later.

If the install directory is not on your `PATH`, the script says so; add it in
your shell profile.

## Windows

Download `yarm_<version>_windows_amd64.zip` from
[Releases](https://github.com/secato/yarm/releases), unpack it and run
`yarm.exe`. There is nothing to install. Use **Windows Terminal** — the legacy
`cmd.exe` console cannot draw the interface.

## From source

Requires Go (see `go.mod` for the version):

```sh
go install github.com/secato/yarm/cmd/yarm@latest
```

Or from a clone, with version information stamped in:

```sh
make build    # → bin/yarm
```

## Requirements

Any modern terminal with UTF-8 and a font that covers box-drawing characters
and arrows.

## Where yarm keeps its files

| | Linux | Windows |
| --- | --- | --- |
| Config (`config.yaml`) | `~/.config/yarm/` | `%LOCALAPPDATA%\yarm\` |
| Data (`installs.json`, `yarm.log`, `custom/`) | `~/.local/share/yarm/` | `%LOCALAPPDATA%\yarm\` |
| Download cache | `~/.cache/yarm/` | `%LOCALAPPDATA%\yarm\cache\` |

XDG variables are honored on Linux. Setting `YARM_HOME` puts all three under
one folder (`config/`, `data/`, `cache/`), which is handy for a portable copy.
