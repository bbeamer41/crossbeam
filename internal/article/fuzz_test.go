// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package article

import (
	"strings"
	"testing"
	"time"
)

func FuzzRoundTrip(f *testing.F) {
	s := sample()
	f.Add(s.Title, s.Author, s.ID, s.Body)
	f.Add("Quiet", "K5LEB", "", "")
	f.Add("T", "K5LEB", "id", strings.Repeat("é", 80)+" "+strings.Repeat("a", 73))
	f.Fuzz(func(t *testing.T, title, author, id, body string) {
		a := Article{
			Title:   title,
			Author:  author,
			ID:      id,
			Created: time.Unix(1_700_000_000, 123),
			Body:    body,
		}
		c, err := a.Canonical()
		if err != nil {
			if _, err2 := a.RenderPacket(); err2 == nil {
				t.Fatalf("RenderPacket accepted an article Canonical rejected: %v", err)
			}
			if _, err2 := a.RenderMicron(); err2 == nil {
				t.Fatalf("RenderMicron accepted an article Canonical rejected: %v", err)
			}
			return
		}
		packet, err := c.RenderPacket()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(packet, "\n") {
			t.Fatalf("packet does not end with a newline: %q", packet)
		}
		got, err := ParsePacket(packet)
		if err != nil || got != c {
			t.Fatalf("packet round trip match=%v err=%v\n%s", got == c, err, packet)
		}
		page, err := c.RenderMicron()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(page, "\n") {
			t.Fatalf("page does not end with a newline: %q", page)
		}
		got, err = ParseMicron(page)
		if err != nil || got != c {
			t.Fatalf("micron round trip match=%v err=%v\n%s", got == c, err, page)
		}
	})
}

func FuzzParse(f *testing.F) {
	a, err := sample().Canonical()
	if err != nil {
		f.Fatal(err)
	}
	packet, err := a.RenderPacket()
	if err != nil {
		f.Fatal(err)
	}
	page, err := a.RenderMicron()
	if err != nil {
		f.Fatal(err)
	}
	f.Add(packet)
	f.Add(page)
	f.Add("")
	f.Fuzz(func(t *testing.T, text string) {
		_, _ = ParsePacket(text)
		_, _ = ParseMicron(text)
	})
}
