#!/bin/sh
# Copyright (C) 2026 K5LEB
# SPDX-License-Identifier: GPL-3.0-or-later

# Install the Crossbeam package in a Debian virtual machine and print the
# result. This script does not install the package on the host.
set -eu

if [ $# -ne 1 ]; then
	echo "usage: packaging/vm-check.sh PACKAGE.deb" >&2
	exit 2
fi
deb=$1
if [ ! -f "$deb" ]; then
	echo "package is missing" >&2
	exit 1
fi
for cmd in qemu-system-x86_64 qemu-img xorriso mkfs.ext4 curl sha512sum; do
	if ! command -v "$cmd" >/dev/null 2>&1; then
		echo "$cmd is not installed" >&2
		exit 1
	fi
done

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
log=$work/serial.log
image_url=https://cloud.debian.org/images/cloud/trixie/latest
image_name=debian-13-genericcloud-amd64.qcow2
cache=${CROSSBEAM_VM_CACHE:-$root/.vm-cache}
mkdir -p "$cache"

cleanup() {
	if [ -n "${qpid:-}" ]; then
		kill "$qpid" 2>/dev/null || true
		wait "$qpid" 2>/dev/null || true
	fi
	if [ -f "$log" ]; then
		cp "$log" /tmp/crossbeam-vm-serial.log
	fi
	rm -rf "$work"
}
trap cleanup EXIT

image_matches() {
	(
		cd "$cache" || return 1
		sha512sum -c SHA512SUMS --ignore-missing
	)
}
fetch_image() {
	curl -fL --retry 3 -o "$cache/$image_name.partial" "$image_url/$image_name"
	mv "$cache/$image_name.partial" "$cache/$image_name"
}
curl -fsSL -o "$cache/SHA512SUMS" "$image_url/SHA512SUMS"
if [ ! -f "$cache/$image_name" ] || ! image_matches; then
	rm -f "$cache/$image_name"
	fetch_image
	image_matches
fi

mkdir -p "$work/share"
cp "$deb" "$work/share/crossbeam.deb"
rm -f "$work/share.img"
mkfs.ext4 -q -L CBSHARE -d "$work/share" "$work/share.img" 32M

cat >"$work/meta-data" <<'EOF'
instance-id: crossbeam-vm
local-hostname: crossbeam
EOF
cat >"$work/user-data" <<'EOF'
#cloud-config
hostname: crossbeam
disable_root: true
ssh_pwauth: false
package_update: false
write_files:
  - path: /usr/local/sbin/crossbeam-vm-check
    permissions: "0755"
    content: |
      #!/bin/sh
      set -eu
      mark() { printf '%s\n' "$1" > /dev/ttyS0; }
      fail() { mark "CROSSBEAM_VM_FAIL $1"; sync; poweroff; exit 1; }
      i=0
      while [ "$i" -lt 30 ]; do
        if [ -e /dev/disk/by-label/CBSHARE ]; then
          break
        fi
        i=$((i + 1))
        sleep 1
      done
      mkdir -p /mnt/share
      mount -o ro /dev/disk/by-label/CBSHARE /mnt/share || fail mount
      DEBIAN_FRONTEND=noninteractive dpkg -i /mnt/share/crossbeam.deb || fail dpkg
      enabled=$(systemctl is-enabled crossbeam.service 2>/dev/null || true)
      ver=$(/usr/bin/crossbeam version)
      pw=$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')
      printf '%s\n%s\n' "$pw" "$pw" | /usr/sbin/runuser -u crossbeam -- /usr/bin/crossbeam sysop password --state /var/lib/crossbeam >/tmp/crossbeam-pw.out 2>/tmp/crossbeam-pw.err || fail password
      systemctl start crossbeam.service || fail start
      pid=""
      i=0
      while [ "$i" -lt 20 ]; do
        pid=$(systemctl show -p MainPID --value crossbeam.service)
        if [ -n "$pid" ] && [ "$pid" != "0" ] && [ -d "/proc/$pid" ]; then
          break
        fi
        i=$((i + 1))
        sleep 1
      done
      if [ -z "$pid" ] || [ "$pid" = "0" ] || [ ! -d "/proc/$pid" ]; then
        fail pid
      fi
      sleep 2
      rss=$(awk '/^VmRSS:/ {print $2}' "/proc/$pid/status")
      hwm=$(awk '/^VmHWM:/ {print $2}' "/proc/$pid/status")
      kiss=$(/usr/sbin/runuser -u crossbeam -- /usr/bin/crossbeam settings get kiss.tnc --state /var/lib/crossbeam | tr -d '\r\n')
      link=$(/usr/sbin/runuser -u crossbeam -- /usr/bin/crossbeam settings get link.listen --state /var/lib/crossbeam | tr -d '\r\n')
      telnet=$(/usr/sbin/runuser -u crossbeam -- /usr/bin/crossbeam settings get telnet.listen --state /var/lib/crossbeam | tr -d '\r\n')
      fbb=$(/usr/sbin/runuser -u crossbeam -- /usr/bin/crossbeam settings get fbb.listen --state /var/lib/crossbeam | tr -d '\r\n')
      if [ -n "$kiss$link$telnet$fbb" ]; then
        fail ports
      fi
      printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}' | /usr/sbin/runuser -u crossbeam -- /usr/bin/crossbeam mcp --state /var/lib/crossbeam >/tmp/crossbeam-mcp.out 2>/tmp/crossbeam-mcp.err || fail mcp
      if ! grep -q '"protocolVersion":"2025-06-18"' /tmp/crossbeam-mcp.out; then
        fail mcp
      fi
      active=$(systemctl is-active crossbeam.service || true)
      osid=$(. /etc/os-release && printf '%s' "$VERSION_ID")
      mark "CROSSBEAM_VM_OK version=$ver os=$osid rss_kib=$rss hwm_kib=$hwm enabled=$enabled active=$active"
      sync
      systemctl stop crossbeam.service || true
      poweroff
runcmd:
  - [ /usr/local/sbin/crossbeam-vm-check ]
EOF
xorriso -as mkisofs -o "$work/seed.iso" -volid cidata -joliet -rock "$work/user-data" "$work/meta-data" >/dev/null

overlay=$work/overlay.qcow2
qemu-img create -f qcow2 -b "$cache/$image_name" -F qcow2 "$overlay" 8G >/dev/null

cpu=qemu64
accel=tcg
if [ -c /dev/kvm ] && [ -r /dev/kvm ] && [ -w /dev/kvm ]; then
	cpu=host
	accel=kvm
fi
qemu-system-x86_64 \
	-machine "type=pc,accel=${accel}" \
	-cpu "$cpu" \
	-m 2048 \
	-smp 2 \
	-display none \
	-vga none \
	-no-reboot \
	-serial file:"$log" \
	-netdev user,id=net0 \
	-device virtio-net-pci,netdev=net0 \
	-drive file="$overlay",if=virtio,format=qcow2 \
	-drive file="$work/share.img",if=virtio,format=raw,readonly=on \
	-drive file="$work/seed.iso",if=virtio,format=raw,readonly=on \
	>"$work/qemu.err" 2>&1 &
qpid=$!

i=0
while [ "$i" -lt 180 ]; do
	if [ -f "$log" ] && grep -q 'CROSSBEAM_VM_' "$log"; then
		break
	fi
	if ! kill -0 "$qpid" 2>/dev/null; then
		break
	fi
	i=$((i + 1))
	sleep 2
done
kill "$qpid" 2>/dev/null || true
wait "$qpid" 2>/dev/null || true
qpid=

if [ ! -f "$log" ] || ! grep -q 'CROSSBEAM_VM_OK ' "$log"; then
	echo "virtual machine check failed" >&2
	if [ -f "$log" ]; then
		grep 'CROSSBEAM_VM_' "$log" >&2 || true
	fi
	if [ -s "$work/qemu.err" ]; then
		tail -n 40 "$work/qemu.err" >&2 || true
	fi
	exit 1
fi
grep 'CROSSBEAM_VM_OK ' "$log"
