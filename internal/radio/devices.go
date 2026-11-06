// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package radio

import (
	"os"
	"sort"
	"strings"
)

const deviceDir = "/dev/serial/by-id"

// Devices lists plugged-in USB serial ports by their stable names.
// A missing directory means nothing is plugged in.
func Devices() []string {
	return deviceNames(deviceDir)
}

func deviceNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || name == "." || name == ".." || strings.Contains(name, "/") || strings.Contains(name, "..") {
			continue
		}
		if entry.IsDir() {
			continue
		}
		names = append(names, dir+"/"+name)
	}
	sort.Strings(names)
	return names
}
