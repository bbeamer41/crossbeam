// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbeamer41/crossbeam/internal/article"
	"github.com/bbeamer41/crossbeam/internal/store"
)

func TestPageCommands(t *testing.T) {
	stdout, stderr, code := capture(nil)
	if code != 0 || !strings.Contains(stdout, "page publish") || !strings.Contains(stdout, "page ingest") || !strings.Contains(stdout, "docs/pages.md") {
		t.Fatalf("help code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	_, stderr, code = capture([]string{"page"})
	if code != 2 || !strings.Contains(stderr, "page publish|ingest") {
		t.Fatalf("usage code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = capture([]string{"page", "nope"})
	if code != 2 || !strings.Contains(stderr, "unknown command") {
		t.Fatalf("unknown code=%d stderr=%q", code, stderr)
	}

	dir := filepath.Join(t.TempDir(), "state")
	_, stderr, code = capture([]string{"page", "publish", "--state", dir})
	if code != 1 || stderr != "crossbeam: nomad.pages is empty\n" {
		t.Fatalf("empty code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = capture([]string{"page", "publish", "--state", dir, "extra"})
	if code != 2 || !strings.Contains(stderr, "usage") {
		t.Fatalf("extra code=%d stderr=%q", code, stderr)
	}

	_, stderr, code = capture([]string{"settings", "set", "--state", dir, "nomad.pages", "var/pages"})
	if code != 1 || !strings.Contains(stderr, "absolute path") {
		t.Fatalf("relative code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = capture([]string{"settings", "set", "--state", dir, "nomad.pages", "/var/lib/../etc"})
	if code != 1 || !strings.Contains(stderr, "clean absolute path") {
		t.Fatalf("parent code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = capture([]string{"settings", "set", "--state", dir, "nomad.pages", "/var/lib/nomadnetwork/storage/pages/"})
	if code != 0 || stderr != "" {
		t.Fatalf("set code=%d stderr=%q", code, stderr)
	}
	stdout, stderr, code = capture([]string{"settings", "get", "--state", dir, "nomad.pages"})
	if code != 0 || stdout != "/var/lib/nomadnetwork/storage/pages\n" {
		t.Fatalf("get code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	missing := filepath.Join(t.TempDir(), "missing-pages")
	_, stderr, code = capture([]string{"settings", "set", "--state", dir, "nomad.pages", missing})
	if code != 0 {
		t.Fatalf("set missing %d %s", code, stderr)
	}
	stdout, stderr, code = capture([]string{"article", "save", "--state", dir, "--title", "Net tonight", "--author", "K5LEB", "--id", "NIGHT", "--created", "2026-10-18T15:04:05Z", "--body", "Meet at 7."})
	if code != 0 || stdout != "saved 1\n" {
		t.Fatalf("save code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = capture([]string{"page", "publish", "--state", dir})
	if code != 1 || stdout != "" || stderr != "crossbeam: the page directory is not available\n" || strings.Contains(stderr, missing) {
		t.Fatalf("down code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("pages directory was created")
	}
	stdout, stderr, code = capture([]string{"article", "list", "--state", dir})
	if code != 0 || !strings.Contains(stdout, "Net tonight") {
		t.Fatalf("article left code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	pages := t.TempDir()
	_, stderr, code = capture([]string{"settings", "set", "--state", dir, "nomad.pages", pages})
	if code != 0 {
		t.Fatal(stderr)
	}
	stdout, stderr, code = capture([]string{"page", "publish", "--state", dir})
	if code != 0 || stdout != "wrote 1\n" || stderr != "" {
		t.Fatalf("publish code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	info, err := os.Lstat(filepath.Join(pages, "cb-1.mu"))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v %v", info, err)
	}
	stdout, stderr, code = capture([]string{"page", "ingest", "--state", dir})
	if code != 0 || stdout != "read 1\n" || stderr != "" {
		t.Fatalf("ingest code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = capture([]string{"article", "list", "--state", dir})
	if code != 0 || strings.Count(stdout, "Net tonight") != 1 {
		t.Fatalf("duplicated code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	if err := os.WriteFile(filepath.Join(pages, "hello.mu"), []byte("not a page\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = capture([]string{"page", "ingest", "--state", dir})
	if code != 0 || stdout != "read 1\n" || stderr != "" {
		t.Fatalf("skip code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	quiet := filepath.Join(t.TempDir(), "state-quiet")
	quietPages := t.TempDir()
	setPages(t, quiet, quietPages)
	if err := os.WriteFile(filepath.Join(quietPages, "hello.mu"), []byte("not a page\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = capture([]string{"page", "ingest", "--state", quiet})
	if code != 0 || stdout != "read 0\n" || stderr != "" {
		t.Fatalf("quiet code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	blocked := filepath.Join(t.TempDir(), "state-block")
	blockPages := t.TempDir()
	setPages(t, blocked, blockPages)
	savePlain(t, blocked, "ONE", "First", "One.")
	savePlain(t, blocked, "TWO", "Second", "Two.")
	if err := os.Mkdir(filepath.Join(blockPages, "cb-1.mu"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = capture([]string{"page", "publish", "--state", blocked})
	if code != 1 || stdout != "wrote 1\n" || stderr != "crossbeam: 1 pages could not be written\n" {
		t.Fatalf("partial code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	conflict := filepath.Join(t.TempDir(), "state-conflict")
	conflictPages := t.TempDir()
	setPages(t, conflict, conflictPages)
	savePlain(t, conflict, "ONE", "First", "One.")
	second := savePlain(t, conflict, "TWO", "Second", "Two.")
	text, err := second.Article.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conflictPages, "cb-1.mu"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh, err := (article.Article{ID: "NEW", Title: "Third", Author: "K5LEB", Created: second.Article.Created, Body: "Three."}).RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conflictPages, "third.mu"), []byte(fresh), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = capture([]string{"page", "ingest", "--state", conflict})
	if code != 1 || stdout != "read 1\n" || stderr != "crossbeam: 1 pages could not be imported\n" {
		t.Fatalf("import code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	notDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = capture([]string{"page", "publish", "--state", notDir})
	if code != 1 || stdout != "" || stderr != "crossbeam: the pages could not be published\n" || strings.Contains(stderr, notDir) || strings.Contains(strings.ToLower(stderr), "sqlite") {
		t.Fatalf("open code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = capture([]string{"page", "ingest", "--state", notDir})
	if code != 1 || stderr != "crossbeam: the pages could not be imported\n" {
		t.Fatalf("ingest open code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func setPages(t *testing.T, state, pages string) {
	t.Helper()
	_, stderr, code := capture([]string{"settings", "set", "--state", state, "nomad.pages", pages})
	if code != 0 {
		t.Fatal(stderr)
	}
}

func savePlain(t *testing.T, state, id, title, body string) store.SavedArticle {
	t.Helper()
	st, err := store.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	saved, err := st.SaveArticle(context.Background(), store.ArticleIn{
		ID: id, Title: title, Author: "K5LEB", Created: "2026-10-18T15:04:05Z", Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	return saved
}
