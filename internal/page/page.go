// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package page writes articles into a NomadNet pages directory and
// reads those pages back. NomadNet is a separate program. This package
// only reads and writes files. It does not run a page and it does not
// speak Reticulum.
package page

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/bbeamer41/crossbeam/internal/article"
	"github.com/bbeamer41/crossbeam/internal/store"
)

// Page files are mode 0644. NomadNet runs a page whose mode is executable,
// so a page written here stays a normal file.
const pageMode os.FileMode = 0o644

var (
	// ErrPagesOff means nomad.pages is empty. Articles stay in the store.
	ErrPagesOff = errors.New("nomad.pages is empty")
	// ErrPagesUnavailable means the directory cannot be used. Articles stay
	// in the store until a later publish.
	ErrPagesUnavailable = errors.New("the page directory is not available")
)

var (
	ownedName = regexp.MustCompile(`^cb-([1-9][0-9]{0,18})\.mu$`)
	anyName   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}\.mu$`)
)

// FileName is the page file for one stored row. It is a single path segment.
func FileName(rowID int64) (string, error) {
	if rowID <= 0 {
		return "", errors.New("article row is not valid")
	}
	name := fmt.Sprintf("cb-%d.mu", rowID)
	if !ownedName.MatchString(name) {
		return "", errors.New("article row is not valid")
	}
	return name, nil
}

// Publish writes every stored article into dir. A missing directory
// returns ErrPagesUnavailable and leaves the articles in the store.
// The count is how many pages were written. failed is how many articles
// could not be written.
func Publish(ctx context.Context, st *store.Store, dir string) (written, failed int, err error) {
	if err := usable(dir); err != nil {
		return 0, 0, err
	}
	rows, err := st.AllArticles(ctx)
	if err != nil {
		return 0, 0, publishErr(err)
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return written, failed, err
		}
		if err := writePage(dir, row); err != nil {
			failed++
			continue
		}
		written++
	}
	return written, failed, nil
}

// Ingest reads Micron pages from dir into the store. A file that is not
// a regular page, or that is not this program's page format, is skipped.
// Nothing in the directory is executed. read is how many pages were
// stored. failed is how many parsed pages could not be stored.
func Ingest(ctx context.Context, st *store.Store, dir string) (read, failed int, err error) {
	if err := usable(dir); err != nil {
		return 0, 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, ErrPagesUnavailable
	}
	for _, ent := range entries {
		if err := ctx.Err(); err != nil {
			return read, failed, err
		}
		name := ent.Name()
		if !anyName.MatchString(name) {
			continue
		}
		art, ok := readParsed(dir, name)
		if !ok {
			continue
		}
		if err := storeParsed(ctx, st, name, art); err != nil {
			failed++
			continue
		}
		read++
	}
	return read, failed, nil
}

func usable(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return ErrPagesOff
	}
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return ErrPagesUnavailable
	}
	info, err := os.Lstat(dir)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrPagesUnavailable
	}
	return nil
}

func writePage(dir string, row store.SavedArticle) error {
	name, err := FileName(row.RowID)
	if err != nil {
		return err
	}
	text, err := row.Article.RenderMicron()
	if err != nil {
		return err
	}
	final, err := within(dir, name)
	if err != nil {
		return err
	}
	tmpName := fmt.Sprintf(".cb-tmp-%d", row.RowID)
	tmp, err := within(dir, tmpName)
	if err != nil {
		return err
	}
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, pageMode)
	if err != nil {
		return errors.New("article could not be written")
	}
	_, werr := io.WriteString(f, text)
	// fchmod applies to this inode. A later chmod by path would follow a
	// symlink swapped in after the rename.
	merr := f.Chmod(pageMode)
	cerr := f.Close()
	if werr != nil || merr != nil || cerr != nil {
		_ = os.Remove(tmp)
		return errors.New("article could not be written")
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return errors.New("article could not be written")
	}
	info, err := os.Lstat(final)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o111 != 0 {
		_ = os.Remove(final)
		return errors.New("article could not be written")
	}
	return nil
}

func readParsed(dir, name string) (article.Article, bool) {
	path, err := within(dir, name)
	if err != nil {
		return article.Article{}, false
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return article.Article{}, false
	}
	if info.Size() < 0 || info.Size() > article.MaxMessageBytes {
		return article.Article{}, false
	}
	f, err := openPage(path)
	if err != nil {
		return article.Article{}, false
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return article.Article{}, false
	}
	buf, err := io.ReadAll(io.LimitReader(f, article.MaxMessageBytes+1))
	if err != nil || len(buf) > article.MaxMessageBytes {
		return article.Article{}, false
	}
	art, err := article.ParseMicron(string(buf))
	if err != nil {
		return article.Article{}, false
	}
	return art, true
}

func storeParsed(ctx context.Context, st *store.Store, name string, art article.Article) error {
	rows, err := st.ListArticles(ctx)
	if err != nil {
		return err
	}
	rowID := ownedRow(name, rows)
	if rowID == 0 {
		rowID = matchRow(art, rows)
	}
	in := store.ArticleIn{
		RowID:   rowID,
		ID:      art.ID,
		Title:   art.Title,
		Author:  art.Author,
		Body:    art.Body,
		Created: timeRFC3339(art),
	}
	_, err = st.SaveArticle(ctx, in)
	return err
}

func timeRFC3339(art article.Article) string {
	return art.Created.UTC().Format("2006-01-02T15:04:05Z")
}

func ownedRow(name string, rows []store.SavedArticle) int64 {
	m := ownedName.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	for _, row := range rows {
		if row.RowID == id {
			return id
		}
	}
	return 0
}

func matchRow(art article.Article, rows []store.SavedArticle) int64 {
	if art.ID != "" {
		var found int64
		n := 0
		for _, row := range rows {
			if row.Article.ID == art.ID {
				found = row.RowID
				n++
			}
		}
		if n == 1 {
			return found
		}
		return 0
	}
	var found int64
	n := 0
	for _, row := range rows {
		if row.Article.ID == "" && row.Article.Title == art.Title && row.Article.Author == art.Author && row.Article.Created.Equal(art.Created) {
			found = row.RowID
			n++
		}
	}
	if n == 1 {
		return found
	}
	return 0
}

func within(dir, name string) (string, error) {
	if name == "" || name != filepath.Base(name) || strings.Contains(name, "..") {
		return "", errors.New("page name is not valid")
	}
	full := filepath.Join(dir, name)
	rel, err := filepath.Rel(dir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errors.New("page name is not valid")
	}
	return full, nil
}

func publishErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	msg := err.Error()
	if msg == "" || len(msg) > 120 || strings.ContainsAny(msg, "\r\n/\\") {
		return errors.New("the pages could not be published")
	}
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "sqlite") || strings.Contains(lower, ".db") {
		return errors.New("the pages could not be published")
	}
	return err
}
