#!/bin/sh
# Install or update yarm from the GitHub release archives.
#
#   curl -fsSL https://raw.githubusercontent.com/secato/yarm/main/install.sh | sh
#
# Safe to run again: it installs the latest release when that differs from
# the yarm already there, and does nothing when it is the same. The binary
# is swapped in with a rename, so an update never leaves a half-written
# yarm behind, even while one is running.
#
# Environment:
#   YARM_VERSION      release tag to install (e.g. v1.0.0); default: latest
#   YARM_INSTALL_DIR  where the binary goes; default: ~/.local/bin, or
#                     /usr/local/bin when run as root
#
# Pass --uninstall to remove the binary. yarm's own data (installs.json,
# backups, custom shaders) is left alone: it records what yarm put into game
# folders, and deleting it would make those installs impossible to remove.

set -eu

REPO="secato/yarm"

say() { printf 'yarm-install: %s\n' "$*"; }
die() {
	printf 'yarm-install: error: %s\n' "$*" >&2
	exit 1
}

have() { command -v "$1" >/dev/null 2>&1; }

# fetch URL FILE
fetch() {
	curl -fsSL --proto '=https' -o "$2" "$1"
}

# The /releases/latest page redirects to the newest non-prerelease tag.
# Reading the redirect instead of calling the GitHub API avoids its
# unauthenticated rate limit, which shared CI and NAT addresses hit easily.
latest_tag() {
	url=$(curl -fsSLI --proto '=https' -o /dev/null -w '%{url_effective}' \
		"https://github.com/$REPO/releases/latest") || die "could not reach github.com"
	tag=${url##*/}
	case $tag in
	v[0-9]*) printf '%s\n' "$tag" ;;
	*) die "could not determine the latest release (got '$url')" ;;
	esac
}

default_dir() {
	if [ "$(id -u)" -eq 0 ]; then
		printf '/usr/local/bin\n'
	else
		printf '%s/.local/bin\n' "$HOME"
	fi
}

# Prints the version an installed yarm reports, or nothing. Release builds
# print "yarm 1.0.0 (commit, date)".
installed_version() {
	[ -x "$1" ] || return 0
	"$1" -version 2>/dev/null | awk 'NR == 1 { print $2 }' || true
}

sha256() {
	if have sha256sum; then
		sha256sum "$1" | awk '{ print $1 }'
	else
		shasum -a 256 "$1" | awk '{ print $1 }'
	fi
}

uninstall() {
	dir=${YARM_INSTALL_DIR:-$(default_dir)}
	if [ -e "$dir/yarm" ]; then
		rm -f "$dir/yarm"
		say "removed $dir/yarm"
	else
		say "nothing to remove at $dir/yarm"
	fi
	say "yarm's data directory was kept; it is what lets yarm uninstall ReShade from your games"
}

main() {
	case ${1:-} in
	--uninstall)
		uninstall
		return
		;;
	"") ;;
	*) die "unknown argument '$1' (the only option is --uninstall)" ;;
	esac

	[ "$(uname -s)" = Linux ] || die "this script is for Linux; on Windows, download the zip from https://github.com/$REPO/releases"

	case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) die "no release build for $(uname -m)" ;;
	esac

	have curl || die "curl is required"
	have tar || die "tar is required"
	have sha256sum || have shasum || die "sha256sum or shasum is required"

	tag=${YARM_VERSION:-$(latest_tag)}
	case $tag in
	v*) ;;
	*) tag="v$tag" ;;
	esac
	version=${tag#v}

	dir=${YARM_INSTALL_DIR:-$(default_dir)}
	target="$dir/yarm"

	current=$(installed_version "$target")
	if [ "$current" = "$version" ]; then
		say "yarm $version is already installed at $target"
		return
	fi

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT INT TERM

	archive="yarm_${version}_linux_${arch}.tar.gz"
	base="https://github.com/$REPO/releases/download/$tag"

	say "downloading yarm $version ($arch)"
	fetch "$base/$archive" "$tmp/$archive" || die "download failed: $base/$archive"
	fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "download failed: $base/checksums.txt"

	want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || die "$archive is not listed in checksums.txt"
	[ "$(sha256 "$tmp/$archive")" = "$want" ] || die "checksum mismatch for $archive"

	tar -xzf "$tmp/$archive" -C "$tmp" yarm || die "could not extract yarm from $archive"

	mkdir -p "$dir"
	# Stage next to the target, then rename: a rename within one filesystem
	# is atomic, and unlike writing over the file it works while yarm is
	# running (no "text file busy").
	cp "$tmp/yarm" "$dir/.yarm.new.$$"
	chmod 755 "$dir/.yarm.new.$$"
	mv -f "$dir/.yarm.new.$$" "$target" || {
		rm -f "$dir/.yarm.new.$$"
		die "could not write $target"
	}

	if [ -n "$current" ]; then
		say "updated $target from $current to $version"
	else
		say "installed yarm $version to $target"
	fi

	case ":$PATH:" in
	*":$dir:"*) ;;
	*) say "note: $dir is not on your PATH; add it in your shell profile to run 'yarm' directly" ;;
	esac
}

# Everything runs from here, so a download cut short while piping into sh
# cannot execute half a script.
main "$@"
