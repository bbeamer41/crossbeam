// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package article

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func sample() Article {
	return Article{
		ID:      "k5leb-001",
		Title:   "Net night notes",
		Author:  "K5LEB",
		Created: time.Date(2026, 10, 11, 18, 4, 5, 999, time.UTC),
		Body: "The Denton net meets on Thursday.\n\n" +
			"Bring traffic. A longer paragraph follows so the renderer has to wrap it onto several lines for a packet bulletin and for a Micron page without losing any of the words.\n\n" +
			"Special text: `backticks` and a \\path and a line.\n\n" +
			"> not a heading\n\n" +
			"----\n\n" +
			"====\n\n" +
			"[Label]=/pages/other.mu",
	}
}

func TestRoundTripPacketAndMicron(t *testing.T) {
	want, err := sample().Canonical()
	if err != nil {
		t.Fatal(err)
	}
	packet, err := want.RenderPacket()
	if err != nil {
		t.Fatal(err)
	}
	micron, err := want.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	fromPacket, err := ParsePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	fromMicron, err := ParseMicron(micron)
	if err != nil {
		t.Fatal(err)
	}
	if fromPacket != want {
		t.Fatalf("packet round trip\n got %#v\nwant %#v", fromPacket, want)
	}
	if fromMicron != want {
		t.Fatalf("micron round trip\n got %#v\nwant %#v", fromMicron, want)
	}
	if fromPacket != fromMicron {
		t.Fatalf("imports differ\npacket %#v\nmicron %#v", fromPacket, fromMicron)
	}
}

func TestBodyLinesFitPacketWidth(t *testing.T) {
	a, err := sample().Canonical()
	if err != nil {
		t.Fatal(err)
	}
	packet, err := a.RenderPacket()
	if err != nil {
		t.Fatal(err)
	}
	_, body, _ := strings.Cut(packet, "\n\n")
	for i, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		if line == "" {
			continue
		}
		if n := utf8.RuneCountInString(line); n > PacketWidth {
			t.Fatalf("body line %d is %d runes: %q", i+1, n, line)
		}
	}
}

func TestMicronBodyIsNotMarkup(t *testing.T) {
	a, err := sample().Canonical()
	if err != nil {
		t.Fatal(err)
	}
	page, err := a.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(page, "\n----\n")
	if len(parts) != 2 {
		t.Fatalf("page missing divider:\n%s", page)
	}
	for i, line := range strings.Split(strings.TrimRight(parts[1], "\n"), "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			t.Fatalf("body line %d is not indented: %q", i+1, line)
		}
		if strings.HasPrefix(line, ">") || line == "----" || line == "====" {
			t.Fatalf("body line %d is Micron structure: %q", i+1, line)
		}
	}
	if !strings.HasPrefix(page, "> Net night notes\n") {
		t.Fatalf("heading missing:\n%s", page)
	}
}

func TestEmptyBody(t *testing.T) {
	a := Article{
		Title:   "Quiet",
		Author:  "K5LEB",
		Created: time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("CDT", -5*3600)),
	}
	want, err := a.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if want.Created.Location() != time.UTC {
		t.Fatal("created was not stored in UTC")
	}
	for _, render := range []func(Article) (string, error){Article.RenderPacket, Article.RenderMicron} {
		text, err := render(a)
		if err != nil {
			t.Fatal(err)
		}
		var got Article
		if strings.Contains(text, "----") {
			got, err = ParseMicron(text)
		} else {
			got, err = ParsePacket(text)
		}
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("got %#v want %#v from %q", got, want, text)
		}
	}
}

func TestRejectsBadArticles(t *testing.T) {
	base := sample()
	cases := []Article{
		{Author: base.Author, Created: base.Created, Body: base.Body},
		{Title: "   ", Author: base.Author, Created: base.Created},
		{Title: base.Title, Created: base.Created},
		{Title: base.Title, Author: base.Author},
		{Title: "two\nlines", Author: base.Author, Created: base.Created},
	}
	for _, a := range cases {
		if _, err := a.Canonical(); err == nil {
			t.Fatalf("expected error for %#v", a)
		}
	}
	if _, err := ParsePacket("Title: Hi\n"); err == nil {
		t.Fatal("expected missing blank line to fail")
	}
	if _, err := ParseMicron("Hello\n----\n"); err == nil {
		t.Fatal("expected missing heading to fail")
	}
}
