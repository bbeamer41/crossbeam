#!/bin/sh
# Copyright (C) 2026 K5LEB
# SPDX-License-Identifier: GPL-3.0-or-later

# Write a signed apt archive. This script does not install the package.
# The private key stays in CROSSBEAM_APT_GPG_HOME, outside this repository.
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$root"

aptroot=${1:-"$root/apt"}
gnupg=${CROSSBEAM_APT_GPG_HOME:-"$HOME/.local/share/crossbeam/apt-gpg"}
version=$(sed -n 's/^const String = "\(.*\)"$/\1/p' internal/version/version.go)
if [ -z "$version" ]; then
	echo "version is missing" >&2
	exit 1
fi
for cmd in dpkg-scanpackages dpkg-deb dpkg gpg gzip; do
	if ! command -v "$cmd" >/dev/null 2>&1; then
		echo "$cmd is not installed" >&2
		exit 1
	fi
done
arch=$(dpkg --print-architecture)
case "$arch" in
amd64 | arm64) ;;
*)
	echo "unsupported architecture $arch" >&2
	exit 1
	;;
esac

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT
deb=$(./packaging/build-deb.sh "$stage")

pool=$aptroot/pool/main
dist=$aptroot/dists/stable
bindir=$dist/main/binary-$arch
rm -rf "$dist/main"/binary-*
mkdir -p "$pool" "$bindir"
find "$aptroot/pool" -name 'crossbeam_*.deb' -delete
install -m 0644 "$deb" "$pool/crossbeam_${version}_${arch}.deb"

(
	cd "$aptroot"
	dpkg-scanpackages --arch "$arch" pool >"$bindir/Packages"
)
gzip -n -c "$bindir/Packages" >"$bindir/Packages.gz"

sums() {
	name=$1
	hashcmd=$2
	echo "$name:"
	for rel in "main/binary-$arch/Packages" "main/binary-$arch/Packages.gz"; do
		file=$dist/$rel
		hash=$("$hashcmd" "$file" | awk '{print $1}')
		size=$(wc -c <"$file" | tr -d '[:space:]')
		printf ' %s %s %s\n' "$hash" "$size" "$rel"
	done
}

{
	echo "Origin: Crossbeam"
	echo "Label: Crossbeam"
	echo "Suite: stable"
	echo "Codename: stable"
	echo "Date: $(LC_ALL=C date -u -R)"
	echo "Architectures: $arch"
	echo "Components: main"
	echo "Description: Crossbeam packet-radio node"
	sums MD5Sum md5sum
	sums SHA1 sha1sum
	sums SHA256 sha256sum
	sums SHA512 sha512sum
} >"$dist/Release"

mkdir -p "$gnupg"
chmod 700 "$gnupg"
if ! GNUPGHOME="$gnupg" gpg --batch --list-secret-keys --with-colons 2>/dev/null | grep -q '^fpr:'; then
	cat >"$stage/keyparams" <<'EOF'
%no-protection
Key-Type: eddsa
Key-Curve: ed25519
Key-Usage: sign
Name-Real: K5LEB
Name-Email: 75401887+bbeamer41@users.noreply.github.com
Expire-Date: 0
%commit
EOF
	GNUPGHOME="$gnupg" gpg --batch --generate-key "$stage/keyparams"
fi
rm -f "$dist/Release.gpg" "$dist/InRelease"
GNUPGHOME="$gnupg" gpg --batch --yes --armor --detach-sign --output "$dist/Release.gpg" "$dist/Release"
GNUPGHOME="$gnupg" gpg --batch --yes --clearsign --output "$dist/InRelease" "$dist/Release"
GNUPGHOME="$gnupg" gpg --batch --armor --export >"$aptroot/crossbeam-archive-key.asc"
cat >"$aptroot/crossbeam.list" <<'EOF'
deb [signed-by=/etc/apt/keyrings/crossbeam.asc] https://raw.githubusercontent.com/bbeamer41/crossbeam/main/apt stable main
EOF
chmod -R u=rwX,go=rX "$aptroot"
echo "$aptroot"
