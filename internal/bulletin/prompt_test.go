// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package bulletin

import "testing"

func TestListIsNotAForward(t *testing.T) {
	for _, line := range []string{"LIST", "list", "L", "READ 1", "R 1", "?", "BYE"} {
		if isForwardStart(line) {
			t.Fatalf("%s started a forward", line)
		}
	}
	for _, line := range []string{"FB K5LEB ALL", "[CROSSBEAM-0.4.0-FHM$]", "FF", "FQ", "F> AB"} {
		if !isForwardStart(line) {
			t.Fatalf("%s did not start a forward", line)
		}
	}
}
