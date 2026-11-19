// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/bbeamer41/crossbeam/internal/ai"
	"github.com/bbeamer41/crossbeam/internal/settings"
	"github.com/bbeamer41/crossbeam/internal/store"
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

func TestAISendersCommand(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	stdout, stderr, code := capture([]string{"ai", "senders", "list", "--state", dir})
	if code != 0 || !strings.Contains(stdout, "Pending") || !strings.Contains(stdout, "Approved") || !strings.Contains(stdout, "Denied") || !strings.Contains(stdout, "(none)") {
		t.Fatalf("list %d %q %q", code, stdout, stderr)
	}
	stdout, stderr, code = capture([]string{"ai", "senders", "approve", "MeshCore:A1B2", "--state", dir})
	if code != 0 || !strings.Contains(stdout, "Approved meshcore:a1b2.") {
		t.Fatalf("approve %d %q %q", code, stdout, stderr)
	}
	stdout, stderr, code = capture([]string{"ai", "senders", "deny", "reticulum:0123456789abcdef0123456789abcdef", "--state", dir})
	if code != 0 || !strings.Contains(stdout, "Blocked reticulum:0123456789abcdef0123456789abcdef.") {
		t.Fatalf("deny %d %q %q", code, stdout, stderr)
	}
	stdout, stderr, code = capture([]string{"ai", "senders", "list", "--state", dir})
	if code != 0 || !strings.Contains(stdout, "meshcore a1b2") || !strings.Contains(stdout, "reticulum 0123456789abcdef0123456789abcdef") {
		t.Fatalf("listed %q", stdout)
	}
	stdout, stderr, code = capture([]string{"ai", "senders", "revoke", "meshcore:a1b2", "--state", dir})
	if code != 0 || !strings.Contains(stdout, "Revoked meshcore:a1b2.") {
		t.Fatalf("revoke %d %q %q", code, stdout, stderr)
	}
	_, stderr, code = capture([]string{"ai", "senders", "revoke", "meshcore:a1b2", "--state", dir})
	if code != 1 || !strings.Contains(stderr, "not approved") {
		t.Fatalf("second revoke %d %q", code, stderr)
	}
	_, stderr, code = capture([]string{"ai", "senders", "approve", "radio:W5ABC", "--state", dir})
	if code != 2 || !strings.Contains(stderr, "meshcore:") {
		t.Fatalf("bad id %d %q", code, stderr)
	}
	if _, _, code = capture([]string{"help"}); code != 0 {
		t.Fatal(code)
	}
	stdout, _, code = capture([]string{"help"})
	if !strings.Contains(stdout, "ai senders") {
		t.Fatalf("help %q", stdout)
	}
}

func TestAITestWarnsOnPublicURL(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Apply(context.Background(), map[string]string{
		settings.KeyAIURL:     "http://203.0.113.5:11434",
		settings.KeyAITimeout: "5",
	}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	_, stderr, code := capture([]string{"ai", "test", "hi", "--state", dir})
	if code == 0 {
		t.Fatal("a public address answered")
	}
	if !strings.Contains(stderr, ai.URLWarningText) {
		t.Fatalf("warning missing %q", stderr)
	}
	st, err = store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Apply(context.Background(), map[string]string{settings.KeyAIURL: "http://127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	_, stderr, _ = capture([]string{"ai", "test", "hi", "--state", dir})
	if strings.Contains(stderr, ai.URLWarningText) {
		t.Fatalf("loopback warned: %q", stderr)
	}
}
