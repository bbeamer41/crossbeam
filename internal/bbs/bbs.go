// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package bbs is the station prompt shared by the packet session and a
// MeshCore direct message. LIST and READ use the article store. The
// reply uses CR line endings, the same as the packet prompt.
package bbs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/bbeamer41/crossbeam/internal/store"
)

const helpText = "Commands:\r" +
	"?        This list\r" +
	"LIST     List bulletins\r" +
	"READ n   Read one bulletin\r" +
	"BYE      Disconnect\r"

// Command runs one prompt line. bye is true after BYE, QUIT, or EXIT.
// The reply, including the next prompt, is written to w. A write error
// is returned. A missing store or a database failure is a short sentence
// with no path and no database text.
func Command(ctx context.Context, st *store.Store, name, line string, w io.Writer) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if name == "" {
		name = "BBS"
	}
	prompt := name + ">\r"
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false, writeAll(w, prompt)
	}
	switch strings.ToUpper(fields[0]) {
	case "?", "HELP":
		if len(fields) != 1 {
			return false, writeAll(w, "Unknown command. Send ? for help.\r"+prompt)
		}
		return false, writeAll(w, helpText+prompt)
	case "BYE", "QUIT", "EXIT":
		if len(fields) != 1 {
			return false, writeAll(w, "Unknown command. Send ? for help.\r"+prompt)
		}
		return true, writeAll(w, "Bye\r")
	case "LIST", "L":
		if len(fields) != 1 {
			return false, writeAll(w, "Unknown command. Send ? for help.\r"+prompt)
		}
		return false, writeAll(w, list(ctx, st)+prompt)
	case "READ", "R":
		if len(fields) != 2 {
			return false, writeAll(w, "Unknown command. Send ? for help.\r"+prompt)
		}
		return false, writeAll(w, read(ctx, st, fields[1])+prompt)
	default:
		return false, writeAll(w, "Unknown command. Send ? for help.\r"+prompt)
	}
}

func list(ctx context.Context, st *store.Store) string {
	if st == nil {
		return "The bulletin list is not available.\r"
	}
	rows, err := st.ListArticles(ctx)
	if err != nil {
		return "The bulletin list is not available.\r"
	}
	if len(rows) == 0 {
		return "No bulletins.\r"
	}
	var b strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&b, "%d  %s  %s  %s", row.RowID, row.Article.Created.UTC().Format("02-Jan"), row.Article.Author, row.Article.Title)
		if row.Article.ID != "" {
			fmt.Fprintf(&b, "  %s", row.Article.ID)
		}
		b.WriteString("\r")
	}
	return b.String()
}

func read(ctx context.Context, st *store.Store, number string) string {
	rowID, err := strconv.ParseInt(number, 10, 64)
	if err != nil || rowID <= 0 || st == nil {
		return "The bulletin is not there.\r"
	}
	saved, err := st.Article(ctx, rowID)
	if errors.Is(err, store.ErrNotFound) {
		return "The bulletin is not there.\r"
	}
	if err != nil {
		return "The bulletin list is not available.\r"
	}
	text, err := saved.Article.RenderPacket()
	if err != nil {
		return "The bulletin could not be read.\r"
	}
	return strings.ReplaceAll(text, "\n", "\r")
}

func writeAll(w io.Writer, text string) error {
	_, err := io.WriteString(w, text)
	return err
}
