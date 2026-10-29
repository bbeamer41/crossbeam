// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package page

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/article"
	"github.com/bbeamer41/crossbeam/internal/store"
)

func TestPublishErrHidesSQL(t *testing.T) {
	err := publishErr(errors.New("sql: database is closed"))
	if err == nil || err.Error() != "the pages could not be published" {
		t.Fatalf("%v", err)
	}
}

func TestFileName(t *testing.T) {
	name, err := FileName(1)
	if err != nil || name != "cb-1.mu" {
		t.Fatalf("name %q %v", name, err)
	}
	if _, err := FileName(0); err == nil {
		t.Fatal("row 0 was accepted")
	}
}

func TestPublishAndIngest(t *testing.T) {
	// Umask is process-wide. This test must not call t.Parallel.
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	st := testStore(t)
	ctx := context.Background()
	pages := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	index := filepath.Join(pages, "index.mu")
	script := "#!/bin/sh\ntouch " + marker + "\n"
	if err := os.WriteFile(index, []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(index, 0o755); err != nil {
		t.Fatal(err)
	}

	saved := saveArticle(t, st, "NIGHT", "Net tonight", "Meet at 7.")
	replaced := filepath.Join(pages, "cb-1.mu")
	if err := os.WriteFile(replaced, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(replaced, 0o755); err != nil {
		t.Fatal(err)
	}

	written, failed, err := Publish(ctx, st, pages)
	if err != nil || written != 1 || failed != 0 {
		t.Fatalf("publish written=%d failed=%d err=%v", written, failed, err)
	}
	text, err := saved.Article.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(replaced)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != text {
		t.Fatalf("page %q", got)
	}
	info, err := os.Lstat(replaced)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %v %v", info, err)
	}
	entries, err := os.ReadDir(pages)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range entries {
		if len(ent.Name()) > 0 && ent.Name()[0] == '.' {
			t.Fatalf("temporary file %s remained", ent.Name())
		}
	}
	indexInfo, err := os.Lstat(index)
	if err != nil || indexInfo.Mode().Perm() != 0o755 {
		t.Fatalf("index mode %v %v", indexInfo, err)
	}
	indexText, err := os.ReadFile(index)
	if err != nil || string(indexText) != script {
		t.Fatalf("index changed %q %v", indexText, err)
	}

	read, failed, err := Ingest(ctx, st, pages)
	if err != nil || read != 1 || failed != 0 {
		t.Fatalf("ingest read=%d failed=%d err=%v", read, failed, err)
	}
	kept, err := st.Article(ctx, saved.RowID)
	if err != nil || !kept.Article.Created.Equal(saved.Article.Created) || kept.Article.Body != saved.Article.Body || kept.Article.ID != saved.Article.ID {
		t.Fatalf("round trip %+v %v", kept, err)
	}
	rows, err := st.ListArticles(ctx)
	if err != nil || len(rows) != 1 || rows[0].RowID != saved.RowID {
		t.Fatalf("rows %+v %v", rows, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("index.mu was executed")
	}

	edited := saved.Article
	edited.Title = "Net moved"
	edited.Body = "Meet at 8."
	next, err := edited.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replaced, []byte(next), 0o644); err != nil {
		t.Fatal(err)
	}
	read, failed, err = Ingest(ctx, st, pages)
	if err != nil || read != 1 || failed != 0 {
		t.Fatalf("edit read=%d failed=%d err=%v", read, failed, err)
	}
	loaded, err := st.Article(ctx, saved.RowID)
	if err != nil || loaded.Article.Title != "Net moved" || loaded.Article.Body != "Meet at 8." || loaded.Article.ID != "NIGHT" {
		t.Fatalf("updated %+v %v", loaded, err)
	}
	rows, err = st.ListArticles(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("duplicate rows %+v %v", rows, err)
	}
}

func TestIngestWithoutAnOwnedName(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	pages := t.TempDir()
	first := saveArticle(t, st, "", "Local notes", "Bring the radio.")
	text, err := first.Article.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(pages, "notes.mu")
	if err := os.WriteFile(foreign, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	read, failed, err := Ingest(ctx, st, pages)
	if err != nil || read != 1 || failed != 0 {
		t.Fatalf("first read=%d failed=%d err=%v", read, failed, err)
	}
	info, err := os.Lstat(foreign)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("foreign mode %v %v", info, err)
	}
	read, failed, err = Ingest(ctx, st, pages)
	if err != nil || read != 1 || failed != 0 {
		t.Fatalf("second read=%d failed=%d err=%v", read, failed, err)
	}
	rows, err := st.ListArticles(ctx)
	if err != nil || len(rows) != 1 || rows[0].RowID != first.RowID {
		t.Fatalf("rows %+v %v", rows, err)
	}

	other := article.Article{
		Title: "Other notes", Author: "K5LEB", Created: first.Article.Created, Body: "Different.",
	}
	otherText, err := other.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pages, "other.mu"), []byte(otherText), 0o644); err != nil {
		t.Fatal(err)
	}
	read, failed, err = Ingest(ctx, st, pages)
	if err != nil || read != 2 || failed != 0 {
		t.Fatalf("two pages read=%d failed=%d err=%v", read, failed, err)
	}
	rows, err = st.ListArticles(ctx)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %+v %v", rows, err)
	}
}

func TestIngestSkipsUnsafeFiles(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	pages := t.TempDir()
	kept := saveArticle(t, st, "KEEP", "Kept", "Stay.")
	text, err := kept.Article.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	secretArt := article.Article{
		ID: "SECRET", Title: "Secret", Author: "K5LEB", Created: kept.Article.Created, Body: "hidden",
	}
	secret, err := secretArt.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.mu")
	if err := os.WriteFile(outside, []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(pages, "alias.mu")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pages, ".hidden.mu"), []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pages, "a..b.mu"), []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(pages, "box.mu"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(pages, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pages, "nested", "page.mu"), []byte(secret), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pages, "hello.mu"), []byte("not a page\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	big := make([]byte, article.MaxMessageBytes+1)
	if err := os.WriteFile(filepath.Join(pages, "big.mu"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pages, "kept.mu"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}

	read, failed, err := Ingest(ctx, st, pages)
	if err != nil || read != 1 || failed != 0 {
		t.Fatalf("ingest read=%d failed=%d err=%v", read, failed, err)
	}
	rows, err := st.ListArticles(ctx)
	if err != nil || len(rows) != 1 || rows[0].Article.ID != "KEEP" {
		t.Fatalf("rows %+v %v", rows, err)
	}
}

func TestIngestConflictCountsAsFailed(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	pages := t.TempDir()
	first := saveArticle(t, st, "ONE", "First", "One.")
	second := saveArticle(t, st, "TWO", "Second", "Two.")
	text, err := second.Article.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pages, "cb-1.mu"), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := article.Article{
		ID: "NEW", Title: "Third", Author: "K5LEB", Created: first.Article.Created, Body: "Three.",
	}
	freshText, err := fresh.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pages, "third.mu"), []byte(freshText), 0o644); err != nil {
		t.Fatal(err)
	}
	read, failed, err := Ingest(ctx, st, pages)
	if err != nil || read != 1 || failed != 1 {
		t.Fatalf("ingest read=%d failed=%d err=%v", read, failed, err)
	}
	loaded, err := st.Article(ctx, first.RowID)
	if err != nil || loaded.Article.ID != "ONE" {
		t.Fatalf("row 1 changed %+v %v", loaded, err)
	}
}

func TestDeletedArticleLeavesThePage(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	pages := t.TempDir()
	saved := saveArticle(t, st, "NIGHT", "Net tonight", "Meet at 7.")
	if _, _, err := Publish(ctx, st, pages); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteArticle(ctx, saved.RowID); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(pages, "cb-1.mu")
	if _, err := os.Lstat(name); err != nil {
		t.Fatal(err)
	}
	gone, err := st.ListArticles(ctx)
	if err != nil || len(gone) != 0 {
		t.Fatalf("deleted rows %+v %v", gone, err)
	}
	read, failed, err := Ingest(ctx, st, pages)
	if err != nil || read != 1 || failed != 0 {
		t.Fatalf("ingest read=%d failed=%d err=%v", read, failed, err)
	}
	rows, err := st.ListArticles(ctx)
	if err != nil || len(rows) != 1 || rows[0].Article.ID != "NIGHT" || rows[0].Article.Title != "Net tonight" {
		t.Fatalf("rows %+v %v", rows, err)
	}
	if _, err := os.Lstat(name); err != nil {
		t.Fatal(err)
	}
}

func TestPublishLeavesArticlesWhenPagesAreUnavailable(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	saved := saveArticle(t, st, "NIGHT", "Net tonight", "Meet at 7.")

	written, failed, err := Publish(ctx, st, "")
	if !errors.Is(err, ErrPagesOff) || written != 0 || failed != 0 {
		t.Fatalf("empty written=%d failed=%d err=%v", written, failed, err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	written, failed, err = Publish(ctx, st, missing)
	if !errors.Is(err, ErrPagesUnavailable) || written != 0 || failed != 0 {
		t.Fatalf("missing written=%d failed=%d err=%v", written, failed, err)
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatal("missing directory was created")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Publish(ctx, st, file); !errors.Is(err, ErrPagesUnavailable) {
		t.Fatalf("file: %v", err)
	}
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Publish(ctx, st, link); !errors.Is(err, ErrPagesUnavailable) {
		t.Fatalf("symlink dir: %v", err)
	}
	entries, err := os.ReadDir(real)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink target %v %v", entries, err)
	}
	base := t.TempDir()
	realPages := filepath.Join(base, "pages")
	if err := os.Mkdir(realPages, 0o755); err != nil {
		t.Fatal(err)
	}
	unclean := realPages + "/../pages"
	if _, _, err := Publish(ctx, st, unclean); !errors.Is(err, ErrPagesUnavailable) {
		t.Fatalf("unclean: %v", err)
	}
	entries, err = os.ReadDir(realPages)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unclean write %v %v", entries, err)
	}
	if _, _, err := Publish(ctx, st, "pages"); !errors.Is(err, ErrPagesUnavailable) {
		t.Fatalf("relative: %v", err)
	}
	rows, err := st.ListArticles(ctx)
	if err != nil || len(rows) != 1 || rows[0].RowID != saved.RowID {
		t.Fatalf("rows %+v %v", rows, err)
	}
}

func TestPublishCountsOneBlockedPage(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	pages := t.TempDir()
	saveArticle(t, st, "ONE", "First", "One.")
	saveArticle(t, st, "TWO", "Second", "Two.")
	if err := os.Mkdir(filepath.Join(pages, "cb-1.mu"), 0o755); err != nil {
		t.Fatal(err)
	}
	written, failed, err := Publish(ctx, st, pages)
	if err != nil || written != 1 || failed != 1 {
		t.Fatalf("publish written=%d failed=%d err=%v", written, failed, err)
	}
	info, err := os.Lstat(filepath.Join(pages, "cb-1.mu"))
	if err != nil || !info.IsDir() {
		t.Fatalf("blocked path %v %v", info, err)
	}
	info, err = os.Lstat(filepath.Join(pages, "cb-2.mu"))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("second page %v %v", info, err)
	}
}

func TestPublishDoesNotFollowASymlink(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	pages := t.TempDir()
	saved := saveArticle(t, st, "NIGHT", "Net tonight", "Meet at 7.")
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(pages, ".cb-tmp-1")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(pages, "cb-1.mu")); err != nil {
		t.Fatal(err)
	}
	written, failed, err := Publish(ctx, st, pages)
	if err != nil || written != 1 || failed != 0 {
		t.Fatalf("publish written=%d failed=%d err=%v", written, failed, err)
	}
	got, err := os.ReadFile(secret)
	if err != nil || string(got) != "keep" {
		t.Fatalf("secret %q %v", got, err)
	}
	info, err := os.Lstat(secret)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("secret mode %v %v", info, err)
	}
	final := filepath.Join(pages, "cb-1.mu")
	info, err = os.Lstat(final)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o644 {
		t.Fatalf("page %v %v", info, err)
	}
	text, err := saved.Article.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	pageText, err := os.ReadFile(final)
	if err != nil || string(pageText) != text {
		t.Fatalf("page text %q %v", pageText, err)
	}
	if _, err := os.Lstat(filepath.Join(pages, ".cb-tmp-1")); !os.IsNotExist(err) {
		t.Fatal("temporary symlink remained")
	}
}

func TestIngestDoesNotBlockOnAPipe(t *testing.T) {
	st := testStore(t)
	pages := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret.mu")
	outside := article.Article{
		ID: "SECRET", Title: "Secret", Author: "K5LEB", Created: time.Date(2026, 10, 18, 15, 4, 5, 0, time.UTC), Body: "hidden",
	}
	text, err := outside.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	pipe := filepath.Join(pages, "pipe.mu")
	if err := syscall.Mkfifo(pipe, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(pipe, filepath.Join(pages, "linked.mu")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(pages, "alias.mu")); err != nil {
		t.Fatal(err)
	}
	type result struct {
		read, failed int
		err          error
	}
	done := make(chan result, 1)
	go func() {
		read, failed, err := Ingest(context.Background(), st, pages)
		done <- result{read, failed, err}
	}()
	select {
	case res := <-done:
		if res.err != nil || res.read != 0 || res.failed != 0 {
			t.Fatalf("ingest read=%d failed=%d err=%v", res.read, res.failed, res.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ingest blocked")
	}
	rows, err := st.ListArticles(context.Background())
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows %+v %v", rows, err)
	}
}

func TestPublishReportsNoPathWhenTheDirectoryIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write a read-only directory")
	}
	st := testStore(t)
	pages := t.TempDir()
	saveArticle(t, st, "NIGHT", "Net tonight", "Meet at 7.")
	if err := os.Chmod(pages, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(pages, 0o755) })
	written, failed, err := Publish(context.Background(), st, pages)
	if err != nil || written != 0 || failed != 1 {
		t.Fatalf("publish written=%d failed=%d err=%v", written, failed, err)
	}
}

func testStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func saveArticle(t *testing.T, st *store.Store, id, title, body string) store.SavedArticle {
	t.Helper()
	saved, err := st.SaveArticle(context.Background(), store.ArticleIn{
		ID: id, Title: title, Author: "K5LEB", Created: "2026-10-18T15:04:05Z", Body: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	return saved
}
