// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package page

import (
	"os"
	"syscall"
)

// openPage does not follow a symlink swapped in after Lstat, and it does
// not block when that name is now a pipe.
func openPage(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
