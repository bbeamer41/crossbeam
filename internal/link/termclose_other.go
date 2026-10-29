// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !linux

package link

import "net"

func clientLeft(conn net.Conn) bool {
	return true
}
