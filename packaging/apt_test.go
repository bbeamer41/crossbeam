// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package packaging_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbeamer41/crossbeam/internal/version"
)

func TestUpdateAptScript(t *testing.T) {
	if _, err := exec.LookPath("dpkg-scanpackages"); err != nil {
		t.Skip("dpkg-scanpackages is not installed")
	}
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is not installed")
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		t.Skip("apt-get is not installed")
	}
	gpgHome := t.TempDir()
	out := t.TempDir()
	cmd := exec.Command("/bin/sh", "update-apt.sh", out)
	cmd.Dir = "."
	cmd.Env = append(os.Environ(), "CROSSBEAM_APT_GPG_HOME="+gpgHome)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("update-apt: %v\n%s", err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != out {
		t.Fatalf("archive %q stderr %s", stdout.String(), stderr.String())
	}
	if keyMaterial(t, out) {
		t.Fatal("archive contains a private key")
	}
	aptUpdate(t, out)
}

func TestPublishedArchive(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg is not installed")
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		t.Skip("apt-get is not installed")
	}
	archive, err := filepath.Abs(filepath.Join("..", "apt"))
	if err != nil {
		t.Fatal(err)
	}
	packages := filepath.Join(archive, "dists", "stable", "main", "binary-"+arch(t), "Packages")
	raw, err := os.ReadFile(packages)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "Package: crossbeam\n") || !strings.Contains(text, "Version: "+version.String+"\n") {
		t.Fatalf("packages %s", text)
	}
	if strings.Count(text, "Package: crossbeam\n") != 1 {
		t.Fatalf("package count %s", text)
	}
	filename := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "Filename: ") {
			filename = strings.TrimPrefix(line, "Filename: ")
		}
	}
	if filename == "" {
		t.Fatal("filename missing")
	}
	if _, err := os.Stat(filepath.Join(archive, filename)); err != nil {
		t.Fatal(err)
	}
	list, err := os.ReadFile(filepath.Join(archive, "crossbeam.list"))
	if err != nil {
		t.Fatal(err)
	}
	wantList := "deb [signed-by=/etc/apt/keyrings/crossbeam.asc] https://raw.githubusercontent.com/bbeamer41/crossbeam/main/apt stable main\n"
	if string(list) != wantList {
		t.Fatalf("list %q", list)
	}
	if keyMaterial(t, archive) {
		t.Fatal("archive contains a private key")
	}
	fpr := fingerprint(t, filepath.Join(archive, "crossbeam-archive-key.asc"))
	install, err := os.ReadFile(filepath.Join("..", "docs", "install.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(install)
	if !strings.Contains(doc, fpr) || !strings.Contains(doc, "apt update") || !strings.Contains(doc, "apt install crossbeam") || !strings.Contains(doc, "apt upgrade") {
		t.Fatalf("install doc missing archive instructions")
	}
	if strings.Contains(doc, "dpkg -i") || strings.Contains(doc, "vm-check") || strings.Contains(doc, "CROSSBEAM_VM_OK") {
		t.Fatal("install doc still describes a local package install")
	}
	verifyRelease(t, archive)
	aptUpdate(t, archive)
}

func fingerprint(t *testing.T, key string) string {
	t.Helper()
	out := run(t, "gpg", "--batch", "--show-keys", "--with-colons", key)
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "fpr:") {
			fields := strings.Split(line, ":")
			if len(fields) > 9 && fields[9] != "" {
				return fields[9]
			}
		}
	}
	t.Fatalf("fingerprint %s", out)
	return ""
}

func verifyRelease(t *testing.T, archive string) {
	t.Helper()
	home := t.TempDir()
	key := filepath.Join(archive, "crossbeam-archive-key.asc")
	cmd := exec.Command("gpg", "--batch", "--import", key)
	cmd.Env = append(os.Environ(), "GNUPGHOME="+home)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("import %v %s", err, out)
	}
	for _, name := range []string{"InRelease", "Release.gpg"} {
		check := exec.Command("gpg", "--batch", "--verify", filepath.Join(archive, "dists", "stable", name))
		if name == "Release.gpg" {
			check = exec.Command("gpg", "--batch", "--verify", filepath.Join(archive, "dists", "stable", name), filepath.Join(archive, "dists", "stable", "Release"))
		}
		check.Env = append(os.Environ(), "GNUPGHOME="+home)
		if out, err := check.CombinedOutput(); err != nil {
			t.Fatalf("verify %s: %v %s", name, err, out)
		}
	}
}

func keyMaterial(t *testing.T, archive string) bool {
	t.Helper()
	found := false
	err := filepath.Walk(archive, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || strings.HasSuffix(path, ".deb") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(raw, []byte("PGP PRIVATE KEY")) {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func aptUpdate(t *testing.T, archive string) {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{
		"etc/apt/sources.list.d",
		"etc/apt/apt.conf.d",
		"etc/apt/preferences.d",
		"etc/apt/keyrings",
		"var/lib/apt/lists/partial",
		"var/cache/apt/archives/partial",
		"var/lib/dpkg",
	} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "var/lib/dpkg/status"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc/apt/sources.list"), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(root, "etc/apt/keyrings/crossbeam.asc")
	raw, err := os.ReadFile(filepath.Join(archive, "crossbeam-archive-key.asc"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	list := fmt.Sprintf("deb [signed-by=%s] file://%s stable main\n", key, archive)
	if err := os.WriteFile(filepath.Join(root, "etc/apt/sources.list.d/crossbeam.list"), []byte(list), 0o644); err != nil {
		t.Fatal(err)
	}
	conf := "APT::Sandbox::User \"root\";\nDebug::NoLocking \"true\";\nAcquire::Languages \"none\";\n"
	if err := os.WriteFile(filepath.Join(root, "etc/apt/apt.conf.d/local"), []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	status := filepath.Join(root, "var/lib/dpkg/status")
	update := exec.Command("apt-get", "-o", "Dir="+root, "-o", "Dir::State::status="+status, "update")
	out, err := update.CombinedOutput()
	if err != nil || bytes.Contains(out, []byte("Err:")) || bytes.Contains(out, []byte("NO_PUBKEY")) {
		t.Fatalf("apt-get update: %v\n%s", err, out)
	}
	show := exec.Command("apt-cache", "-o", "Dir="+root, "-o", "Dir::State::status="+status, "show", "crossbeam")
	got, err := show.CombinedOutput()
	if err != nil {
		t.Fatalf("apt-cache: %v\n%s\nupdate %s", err, got, out)
	}
	if !bytes.Contains(got, []byte("Version: "+version.String+"\n")) {
		t.Fatalf("show %s\nupdate %s", got, out)
	}
	if bytes.Contains(got, []byte("/home/bbeam")) || bytes.Contains(got, []byte("10.0.35.106")) {
		t.Fatalf("show %s", got)
	}
}
