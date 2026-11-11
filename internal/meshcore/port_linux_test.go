// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build linux

package meshcore

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOpenRejectsPlainFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-radio")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path)
	if !errors.Is(err, ErrDown) || strings.Contains(err.Error(), "not-a-radio") {
		t.Fatalf("%v", err)
	}
}

func TestOpenRejectsPtmx(t *testing.T) {
	f, err := os.Open("/dev/ptmx")
	if err != nil {
		t.Skip(err)
	}
	f.Close()
	_, err = Open("/dev/ptmx")
	if !errors.Is(err, ErrDown) || strings.Contains(err.Error(), "ptmx") {
		t.Fatalf("%v", err)
	}
}

func TestOpenPty(t *testing.T) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skip(err)
	}
	defer master.Close()
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatal(err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatal(err)
	}
	name := "/dev/pts/" + strconv.Itoa(n)
	port, err := Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer port.Close()
	second, err := Open(name)
	if second != nil {
		second.Close()
	}
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("second open %v", err)
	}
	f, ok := port.(*os.File)
	if !ok {
		t.Fatalf("%T", port)
	}
	term, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS2)
	if err != nil {
		t.Fatal(err)
	}
	// The kernel stores the numeric rate here. B115200 is only the c_cflag symbol.
	if term.Ispeed != 115200 || term.Ospeed != 115200 {
		t.Fatalf("speed %d %d", term.Ispeed, term.Ospeed)
	}
	if term.Cflag&unix.CSIZE != unix.CS8 || term.Cflag&unix.PARENB != 0 || term.Cflag&crtscts != 0 {
		t.Fatalf("cflag %x", term.Cflag)
	}
	if term.Lflag&(unix.ICANON|unix.ECHO) != 0 || term.Oflag&unix.OPOST != 0 {
		t.Fatalf("lflag %x oflag %x", term.Lflag, term.Oflag)
	}
	if term.Cc[unix.VMIN] != 1 || term.Cc[unix.VTIME] != 0 {
		t.Fatalf("cc %d %d", term.Cc[unix.VMIN], term.Cc[unix.VTIME])
	}
}
