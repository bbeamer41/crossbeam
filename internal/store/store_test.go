// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/settings"
)

func TestPasswordAndModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := openStore(t, dir)
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("state mode %o", info.Mode().Perm())
	}
	dbInfo, err := os.Stat(filepath.Join(dir, "crossbeam.db"))
	if err != nil {
		t.Fatal(err)
	}
	if dbInfo.Mode().Perm() != 0o600 {
		t.Fatalf("db mode %o", dbInfo.Mode().Perm())
	}
	ctx := context.Background()
	has, err := s.HasPassword(ctx)
	if err != nil || has {
		t.Fatalf("has %v err %v", has, err)
	}
	if err := s.SetPassword(ctx, strings.Repeat("a", 11)); err == nil {
		t.Fatal("11 characters was accepted")
	}
	if err := s.SetPassword(ctx, strings.Repeat("a", 73)); err == nil {
		t.Fatal("73 bytes was accepted")
	}
	if err := s.SetPassword(ctx, "test-password\n1"); err == nil {
		t.Fatal("control character was accepted")
	}
	const password = "test-password-1"
	if err := s.SetPassword(ctx, password); err != nil {
		t.Fatal(err)
	}
	ok, err := s.CheckPassword(ctx, password)
	if err != nil || !ok {
		t.Fatalf("check %v %v", ok, err)
	}
	ok, err = s.CheckPassword(ctx, "test-password-2")
	if err != nil || ok {
		t.Fatalf("wrong password %v %v", ok, err)
	}
	session, err := s.CreateSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.LookupSession(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPassword(ctx, "test-password-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LookupSession(ctx, session.Token); !errors.Is(err, ErrNoSession) {
		t.Fatalf("old session: %v", err)
	}
	ok, err = s.CheckPassword(ctx, password)
	if err != nil || ok {
		t.Fatal("old password still works")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "state"))
	ctx := context.Background()
	got, err := s.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got[settings.KeyPublic] != "false" || got[settings.KeyCallsign] != "" {
		t.Fatalf("%v", got)
	}
	next, err := s.Apply(ctx, map[string]string{settings.KeyName: "Home station"})
	if err != nil {
		t.Fatal(err)
	}
	if next[settings.KeyName] != "Home station" {
		t.Fatal(next)
	}
	_, err = s.Apply(ctx, map[string]string{settings.KeyPublic: "yes"})
	if err == nil || !strings.Contains(err.Error(), "true or false") {
		t.Fatalf("%v", err)
	}
	got, err = s.Settings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got[settings.KeyPublic] != "false" || got[settings.KeyName] != "Home station" {
		t.Fatalf("rejected patch changed settings: %v", got)
	}
	_, err = s.Apply(ctx, map[string]string{"no.such": "x"})
	if err == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestArticles(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "state"))
	ctx := context.Background()
	if err := s.SetPassword(ctx, "test-password-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Apply(ctx, map[string]string{settings.KeyCallsign: "K5LEB"}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 18, 15, 4, 5, 0, time.UTC)
	first, err := s.SaveArticle(ctx, ArticleIn{
		Title: "Net night",
		Body:  "The net meets Thursday.",
		Now:   now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Article.Author != "K5LEB" || !first.Article.Created.Equal(now) {
		t.Fatalf("%+v", first.Article)
	}
	second, err := s.SaveArticle(ctx, ArticleIn{
		Title:   "Another",
		Author:  "K5LEB",
		Created: now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveArticle(ctx, ArticleIn{
		ID:      "note-1",
		Title:   "With id",
		Author:  "K5LEB",
		Created: now.Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveArticle(ctx, ArticleIn{
		ID:      "note-1",
		Title:   "Again",
		Author:  "K5LEB",
		Created: now.Format(time.RFC3339),
	})
	if !errors.Is(err, ErrDuplicateID{}) {
		t.Fatalf("duplicate: %v", err)
	}
	updated, err := s.SaveArticle(ctx, ArticleIn{
		RowID:   first.RowID,
		Title:   "Net night notes",
		Author:  "K5LEB",
		Created: now.Format(time.RFC3339),
		Body:    "The net meets Thursday.",
	})
	if err != nil || updated.Article.Title != "Net night notes" {
		t.Fatalf("%v %+v", err, updated)
	}
	_, err = s.SaveArticle(ctx, ArticleIn{
		RowID:   second.RowID,
		ID:      "note-1",
		Title:   "Clash",
		Author:  "K5LEB",
		Created: now.Format(time.RFC3339),
	})
	if !errors.Is(err, ErrDuplicateID{}) {
		t.Fatalf("update clash: %v", err)
	}
	loaded, err := s.Article(ctx, first.RowID)
	if err != nil || loaded.Article.Title != "Net night notes" {
		t.Fatalf("%v %+v", err, loaded)
	}
	list, err := s.ListArticles(ctx)
	if err != nil || len(list) != 3 {
		t.Fatalf("%v %d", err, len(list))
	}
	_, err = s.SaveArticle(ctx, ArticleIn{Title: "No author", Now: now})
	if err == nil {
		// callsign is set, so author is filled
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE articles SET created = 'nope' WHERE row_id = ?`, first.RowID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Article(ctx, first.RowID); err == nil || !strings.Contains(err.Error(), "RFC3339") {
		t.Fatalf("corrupt created: %v", err)
	}
	if _, err := s.Prepare(ctx, ArticleIn{Title: "Bad", Author: "K5LEB", Created: "yesterday"}); err == nil {
		t.Fatal("corrupt created was prepared")
	}
}

func TestAuthorRequiredWhenCallsignEmpty(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "state"))
	_, err := s.SaveArticle(context.Background(), ArticleIn{Title: "Hello", Now: time.Now()})
	if err == nil || !strings.Contains(err.Error(), "author is empty") {
		t.Fatalf("%v", err)
	}
}

func openStore(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
