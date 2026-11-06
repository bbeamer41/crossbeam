// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package radio

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadRPRT(t *testing.T) {
	if err := readRPRT(bufio.NewReader(strings.NewReader("\nextra\nRPRT 0\n"))); err != nil {
		t.Fatal(err)
	}
	err := readRPRT(bufio.NewReader(strings.NewReader("RPRT 11\n")))
	if err == nil || err.Error() != "RPRT 11" {
		t.Fatalf("%v", err)
	}
	if err := readRPRT(bufio.NewReader(strings.NewReader(""))); err == nil {
		t.Fatal("empty reply was accepted")
	}
}
