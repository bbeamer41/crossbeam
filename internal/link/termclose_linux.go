// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build linux

package link

import (
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// clientLeft reports that the terminal client closed the socket.
// Shutting down only the write side leaves the socket up so the reply
// can still be read. Poll is the difference: a full close raises POLLHUP,
// and a write shutdown raises POLLRDHUP without it.
func clientLeft(conn net.Conn) bool {
	sc, ok := conn.(syscall.Conn)
	if !ok {
		return true
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return true
	}
	left := true
	cerr := raw.Control(func(fd uintptr) {
		pfd := []unix.PollFd{{
			Fd:     int32(fd),
			Events: unix.POLLIN | unix.POLLRDHUP,
		}}
		n, err := unix.Poll(pfd, 0)
		if err != nil || n == 0 {
			left = false
			return
		}
		left = pfd[0].Revents&unix.POLLHUP != 0
	})
	if cerr != nil {
		return true
	}
	return left
}
