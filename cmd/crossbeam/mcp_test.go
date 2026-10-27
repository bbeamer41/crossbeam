// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPCommand(t *testing.T) {
	stdout, stderr, code := capture(nil)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "crossbeam mcp") || !strings.Contains(stdout, "docs/mcp.md") {
		t.Fatalf("help code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	_, stderr, code = capture([]string{"mcp", "extra"})
	if code != 2 || !strings.Contains(stderr, "usage: crossbeam mcp") {
		t.Fatalf("extra code=%d stderr=%q", code, stderr)
	}

	blocked := filepath.Join(t.TempDir(), "secret-state-name")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = capture([]string{"mcp", "--state", blocked})
	if code != 1 || stdout != "" || stderr != "crossbeam: the article store is not available\n" || strings.Contains(stderr, "secret-state-name") {
		t.Fatalf("blocked code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	dir := filepath.Join(t.TempDir(), "state")
	script := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"save_article","arguments":{"title":"Hello","author":"K5LEB","body":"The net meets.","created":"2026-10-18T15:04:05Z"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_articles"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"station"}}`,
	}, "\n") + "\n"
	stdout, stderr, code = captureIn(context.Background(), []string{"mcp", "--state", dir}, strings.NewReader(script))
	if code != 0 || stderr != "" {
		t.Fatalf("session code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines %d %q", len(lines), stdout)
	}
	if strings.Contains(lines[2], "The net meets.") || strings.Contains(lines[3], dir) || strings.Contains(stdout, "sqlite") {
		t.Fatalf("leaked %q", stdout)
	}
	if !strings.Contains(lines[1], `"row":1`) || strings.Contains(lines[1], "The net meets.") {
		t.Fatalf("save %s", lines[1])
	}
	if !strings.Contains(lines[2], "Hello") || !strings.Contains(lines[3], `"callsign":""`) || !strings.Contains(lines[3], `"version":"`+version+`"`) {
		t.Fatalf("list %s station %s", lines[2], lines[3])
	}
}
