// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package bbs

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/bbeamer41/crossbeam/internal/store"
)

func TestCommands(t *testing.T) {
	ctx := context.Background()
	st := openStore(t)
	saved, err := st.SaveArticle(ctx, store.ArticleIn{
		Title:   "Net night notes",
		Author:  "K5LEB",
		ID:      "k5leb-001",
		Body:    "The Denton net meets on Thursday.",
		Created: "2026-11-09T18:04:05Z",
	})
	if err != nil {
		t.Fatal(err)
	}

	var empty bytes.Buffer
	bye, err := Command(ctx, st, "K5LEB", "", &empty)
	if err != nil || bye || empty.String() != "K5LEB>\r" {
		t.Fatalf("empty bye=%v err=%v %q", bye, err, empty.String())
	}

	var help bytes.Buffer
	bye, err = Command(ctx, st, "K5LEB", "?", &help)
	if err != nil || bye || !strings.Contains(help.String(), "Commands:\r") || !strings.Contains(help.String(), "LIST") || !strings.Contains(help.String(), "READ n") || !strings.Contains(help.String(), "BYE") || !strings.HasSuffix(help.String(), "K5LEB>\r") {
		t.Fatalf("help bye=%v err=%v %q", bye, err, help.String())
	}

	var listed bytes.Buffer
	if _, err := Command(ctx, st, "K5LEB", "LIST", &listed); err != nil {
		t.Fatal(err)
	}
	wantLine := "1  09-Nov  K5LEB  Net night notes  k5leb-001\r"
	if !strings.Contains(listed.String(), wantLine) || !strings.HasSuffix(listed.String(), "K5LEB>\r") {
		t.Fatalf("list %q", listed.String())
	}
	if saved.RowID != 1 {
		t.Fatalf("row %d", saved.RowID)
	}

	var body bytes.Buffer
	if _, err := Command(ctx, st, "K5LEB", "READ 1", &body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body.String(), "Title: Net night notes\r") || !strings.Contains(body.String(), "The Denton net meets on Thursday.\r") || !strings.HasSuffix(body.String(), "K5LEB>\r") {
		t.Fatalf("read %q", body.String())
	}

	var missing bytes.Buffer
	if _, err := Command(ctx, st, "K5LEB", "R 9", &missing); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(missing.String(), "The bulletin is not there.\r") {
		t.Fatalf("missing %q", missing.String())
	}

	var unknown bytes.Buffer
	if _, err := Command(ctx, st, "K5LEB", "SEND", &unknown); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unknown.String(), "Unknown command. Send ? for help.\r") {
		t.Fatalf("unknown %q", unknown.String())
	}

	var out bytes.Buffer
	bye, err = Command(ctx, st, "K5LEB", "QUIT", &out)
	if err != nil || !bye || out.String() != "Bye\r" {
		t.Fatalf("bye %v %v %q", bye, err, out.String())
	}
}

func TestEmptyList(t *testing.T) {
	var out bytes.Buffer
	if _, err := Command(context.Background(), openStore(t), "K5LEB", "L", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No bulletins.\r") || !strings.HasSuffix(out.String(), "K5LEB>\r") {
		t.Fatalf("empty %q", out.String())
	}
}

func TestNilStore(t *testing.T) {
	var out bytes.Buffer
	if _, err := Command(context.Background(), nil, "K5LEB", "LIST", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "The bulletin list is not available.\r") {
		t.Fatalf("nil %q", out.String())
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}
