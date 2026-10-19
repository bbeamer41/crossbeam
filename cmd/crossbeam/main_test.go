// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	t.Run("description", func(t *testing.T) {
		for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}} {
			stdout, stderr, code := capture(args)
			if code != 0 {
				t.Fatalf("%v exited %d", args, code)
			}
			if stderr != "" {
				t.Fatalf("%v wrote to stderr: %q", args, stderr)
			}
			if !strings.Contains(stdout, "crossbeam "+version) || !strings.Contains(stdout, "docs/article.md") {
				t.Fatalf("%v stdout %q", args, stdout)
			}
		}
	})
	t.Run("version", func(t *testing.T) {
		for _, args := range [][]string{{"version"}, {"--version"}} {
			stdout, stderr, code := capture(args)
			if code != 0 || stderr != "" || stdout != "crossbeam "+version+"\n" {
				t.Fatalf("%v code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
			}
		}
	})
	t.Run("unknown", func(t *testing.T) {
		for _, args := range [][]string{{"nope"}, {"version", "extra"}} {
			stdout, stderr, code := capture(args)
			if code != 2 || stdout != "" || !strings.Contains(stderr, "unknown command") {
				t.Fatalf("%v code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
			}
		}
	})
}

func capture(args []string) (string, string, int) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}
