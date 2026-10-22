// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package page

import "os"

func openPage(path string) (*os.File, error) {
	return os.Open(path)
}
