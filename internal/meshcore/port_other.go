// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !linux

package meshcore

import "io"

// Open reports that the companion serial port is not available.
func Open(string) (io.ReadWriteCloser, error) {
	return nil, ErrDown
}
