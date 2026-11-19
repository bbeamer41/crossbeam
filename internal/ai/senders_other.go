// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package ai

import (
	"os"
	"sync"
)

var senderFileMu sync.Mutex

func flock(f *os.File) error {
	senderFileMu.Lock()
	return nil
}

func funlock(f *os.File) error {
	senderFileMu.Unlock()
	return nil
}
