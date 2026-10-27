// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package packaging_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbeamer41/crossbeam/internal/version"
)

func TestPackageBuilds(t *testing.T) {
	if _, err := exec.LookPath("dpkg-deb"); err != nil {
		t.Skip("dpkg-deb is not installed")
	}
	out := t.TempDir()
	cmd := exec.Command("/bin/sh", "build-deb.sh", out)
	cmd.Dir = "."
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build: %v\n%s", err, stderr.String())
	}
	deb := strings.TrimSpace(stdout.String())
	if !strings.HasSuffix(deb, "crossbeam_"+version.String+"_"+arch(t)+".deb") {
		t.Fatalf("deb %q stderr %s", deb, stderr.String())
	}
	info := run(t, "dpkg-deb", "-I", deb)
	if !strings.Contains(info, "Version: "+version.String) || !strings.Contains(info, "Architecture: "+arch(t)) {
		t.Fatalf("control %s", info)
	}
	if !strings.Contains(info, "Depends: systemd") {
		t.Fatalf("depends %s", info)
	}
	if !installedSize(info) {
		t.Fatalf("installed size %s", info)
	}
	list := run(t, "dpkg-deb", "-c", deb)
	for _, path := range []string{
		"./usr/bin/crossbeam",
		"./usr/lib/systemd/system/crossbeam.service",
		"./usr/share/doc/crossbeam/copyright",
	} {
		if !strings.Contains(list, path) {
			t.Fatalf("missing %s in %s", path, list)
		}
	}
	if strings.Contains(list, "usr/local") {
		t.Fatalf("package uses /usr/local\n%s", list)
	}
	root := t.TempDir()
	run(t, "dpkg-deb", "-x", deb, root)
	ctrl := t.TempDir()
	run(t, "dpkg-deb", "-e", deb, ctrl)
	bin := filepath.Join(root, "usr/bin/crossbeam")
	raw, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("/home/bbeam")) || bytes.Contains(raw, []byte("10.0.35.106")) {
		t.Fatal("binary contains a local path")
	}
	versionOut := run(t, bin, "version")
	if strings.TrimSpace(versionOut) != "crossbeam "+version.String {
		t.Fatalf("binary version %q", versionOut)
	}
	ldd := exec.Command("ldd", bin)
	lddOut, lddErr := ldd.CombinedOutput()
	if lddErr == nil || !bytes.Contains(lddOut, []byte("not a dynamic executable")) {
		t.Fatalf("ldd %v %s", lddErr, lddOut)
	}
	unit, err := os.ReadFile(filepath.Join(root, "usr/lib/systemd/system/crossbeam.service"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(unit, []byte("/usr/bin/crossbeam")) || bytes.Contains(unit, []byte("/usr/local/bin")) {
		t.Fatalf("unit %s", unit)
	}
	manual, err := os.ReadFile("../deploy/crossbeam.service")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(manual, []byte("/usr/local/bin/crossbeam")) {
		t.Fatal("manual unit changed")
	}
	postinst, err := os.ReadFile(filepath.Join(ctrl, "postinst"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(postinst)
	if !strings.Contains(text, "useradd") || strings.Contains(text, "systemctl enable") || strings.Contains(text, "systemctl start") {
		t.Fatalf("postinst %s", text)
	}
	if !strings.Contains(list, "-rwxr-xr-x") {
		t.Fatalf("mode %s", list)
	}
}

func installedSize(info string) bool {
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Installed-Size:") {
			continue
		}
		n := strings.TrimSpace(strings.TrimPrefix(line, "Installed-Size:"))
		return n != "" && n != "0"
	}
	return false
}

func arch(t *testing.T) string {
	t.Helper()
	return strings.TrimSpace(run(t, "dpkg", "--print-architecture"))
}

func run(t *testing.T, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %s", name, err, out)
	}
	return string(out)
}
