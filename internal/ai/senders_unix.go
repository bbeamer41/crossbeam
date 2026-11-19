// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package ai

import (
	"os"

	"golang.org/x/sys/unix"
)

func flock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

func funlock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
