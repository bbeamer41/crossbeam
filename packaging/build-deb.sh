#!/bin/sh
# Copyright (C) 2026 K5LEB
# SPDX-License-Identifier: GPL-3.0-or-later

# Build a Debian package. This script does not install it.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"

out=${1:-"$root/dist"}
version=$(sed -n 's/^const String = "\(.*\)"$/\1/p' internal/version/version.go)
if [ -z "$version" ]; then
	echo "version is missing" >&2
	exit 1
fi
if ! command -v dpkg-deb >/dev/null 2>&1 || ! command -v dpkg >/dev/null 2>&1; then
	echo "dpkg-deb is not installed" >&2
	exit 1
fi
arch=$(dpkg --print-architecture)
case "$arch" in
amd64) goarch=amd64 ;;
arm64) goarch=arm64 ;;
*)
	echo "unsupported architecture $arch" >&2
	exit 1
	;;
esac

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

CGO_ENABLED=0 GOOS=linux GOARCH="$goarch" go build -trimpath -ldflags='-s -w' -o "$work/crossbeam" ./cmd/crossbeam
got=$("$work/crossbeam" version)
if [ "$got" != "crossbeam $version" ]; then
	echo "built version $got" >&2
	exit 1
fi

pkg=$work/pkg
mkdir -p "$pkg/usr/bin" "$pkg/usr/lib/systemd/system" "$pkg/usr/share/doc/crossbeam" "$pkg/DEBIAN"
install -m 0755 "$work/crossbeam" "$pkg/usr/bin/crossbeam"
install -m 0644 packaging/crossbeam.service "$pkg/usr/lib/systemd/system/crossbeam.service"
install -m 0644 packaging/copyright "$pkg/usr/share/doc/crossbeam/copyright"
install -m 0755 packaging/postinst "$pkg/DEBIAN/postinst"
install -m 0755 packaging/prerm "$pkg/DEBIAN/prerm"
install -m 0755 packaging/postrm "$pkg/DEBIAN/postrm"

installed=$(du -sk "$pkg/usr" | awk '{print $1}')
if [ -z "$installed" ] || [ "$installed" -eq 0 ]; then
	echo "installed size is empty" >&2
	exit 1
fi

cat >"$pkg/DEBIAN/control" <<EOF
Package: crossbeam
Version: $version
Section: hamradio
Priority: optional
Architecture: $arch
Maintainer: K5LEB <75401887+bbeamer41@users.noreply.github.com>
Homepage: https://github.com/bbeamer41/crossbeam
Installed-Size: $installed
Depends: systemd
Description: packet-radio node and bulletin bridge
 Crossbeam stores one article and can post it as a packet bulletin
 and as a NomadNet page. The package does not enable the service.
EOF

mkdir -p "$out"
deb="$out/crossbeam_${version}_${arch}.deb"
rm -f "$deb"
dpkg-deb --root-owner-group -Zgzip --build "$pkg" "$deb" >&2
echo "$deb"
