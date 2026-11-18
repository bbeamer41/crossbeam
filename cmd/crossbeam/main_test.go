// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
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
	return captureIn(context.Background(), args, strings.NewReader(""))
}

func captureIn(ctx context.Context, args []string, stdin *strings.Reader) (string, string, int) {
	var stdout, stderr bytes.Buffer
	code := run(ctx, args, stdin, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func TestAIKeyAndLog(t *testing.T) {
	dir := t.TempDir()
	var out, errOut strings.Builder
	if code := run(context.Background(), []string{"ai", "key", "--state", dir}, strings.NewReader("sk-test-1\n"), &out, &errOut); code != 0 {
		t.Fatalf("key %d %s", code, errOut.String())
	}
	if strings.Contains(out.String()+errOut.String(), "sk-test-1") || !strings.Contains(out.String(), "AI key saved.") {
		t.Fatalf("key output %q %q", out.String(), errOut.String())
	}
	out.Reset()
	if code := run(context.Background(), []string{"ai", "log", "--state", dir}, nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), "empty") {
		t.Fatalf("log %d %q", code, out.String())
	}
	out.Reset()
	if code := run(context.Background(), []string{"ai", "key", "--clear", "--state", dir}, nil, &out, &errOut); code != 0 || !strings.Contains(out.String(), "removed") {
		t.Fatalf("clear %d %q", code, out.String())
	}
	if code := run(context.Background(), []string{"ai", "nope"}, nil, &out, &errOut); code != 2 {
		t.Fatalf("unknown %d", code)
	}
}
