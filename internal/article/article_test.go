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
	if !strings.HasSuffix(packet, "\n") || !strings.HasSuffix(micron, "\n") {
		t.Fatal("rendering does not end with a newline")
	}
	if strings.Contains(packet, ".999") || !strings.Contains(packet, "2026-10-11T18:04:05Z") {
		t.Fatalf("created time was not stored as a UTC second:\n%s", packet)
	}
	if !strings.Contains(micron, "\\`") || !strings.Contains(micron, "\\\\") {
		t.Fatalf("micron escapes missing:\n%s", micron)
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
	lines := strings.Split(strings.TrimRight(page, "\n"), "\n")
	divider := -1
	for i, line := range lines {
		if line == "----" {
			divider = i
			break
		}
	}
	if divider < 0 {
		t.Fatalf("page missing divider:\n%s", page)
	}
	for i, line := range lines[divider+1:] {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			t.Fatalf("body line %d is not indented: %q", i+1, line)
		}
		if strings.HasPrefix(line, ">") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") || line == "----" || line == "====" {
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
	if got := want.Created.Format(time.RFC3339); got != "2026-01-02T08:04:05Z" {
		t.Fatalf("created = %s", got)
	}
	packet, err := a.RenderPacket()
	if err != nil {
		t.Fatal(err)
	}
	fromPacket, err := ParsePacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if fromPacket != want {
		t.Fatalf("packet got %#v want %#v from %q", fromPacket, want, packet)
	}
	page, err := a.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	fromPage, err := ParseMicron(page)
	if err != nil {
		t.Fatal(err)
	}
	if fromPage != want {
		t.Fatalf("micron got %#v want %#v from %q", fromPage, want, page)
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

func TestDocumentedExamples(t *testing.T) {
	a := Article{
		ID:      "k5leb-001",
		Title:   "Net night notes",
		Author:  "K5LEB",
		Created: time.Date(2026, 10, 11, 18, 4, 5, 0, time.UTC),
		Body:    "The Denton net meets on Thursday.",
	}
	packet, err := a.RenderPacket()
	if err != nil {
		t.Fatal(err)
	}
	wantPacket := "" +
		"Title: Net night notes\n" +
		"Author: K5LEB\n" +
		"Created: 2026-10-11T18:04:05Z\n" +
		"ID: k5leb-001\n" +
		"\n" +
		"The Denton net meets on Thursday.\n"
	if packet != wantPacket {
		t.Fatalf("packet\n got %q\nwant %q", packet, wantPacket)
	}
	page, err := a.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	wantPage := "" +
		"> Net night notes\n" +
		"Author: K5LEB\n" +
		"Created: 2026-10-11T18:04:05Z\n" +
		"ID: k5leb-001\n" +
		"\n" +
		"----\n" +
		"  The Denton net meets on Thursday.\n"
	if page != wantPage {
		t.Fatalf("page\n got %q\nwant %q", page, wantPage)
	}
}

func TestHeaderOrderAndLookalikeBody(t *testing.T) {
	in := "" +
		"ID: k5leb-001\n" +
		"Created:  2026-10-11T18:04:05Z  \n" +
		"Author: K5LEB\n" +
		"Title: Net night notes\n" +
		"\n" +
		"Title: not a header\n" +
		"\n" +
		"----\n"
	got, err := ParsePacket(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Net night notes" || got.Body != "Title: not a header\n\n----" {
		t.Fatalf("%#v", got)
	}
	page := "" +
		"> Net night notes\n" +
		"\n" +
		"ID: k5leb-001\n" +
		"Created: 2026-10-11T18:04:05Z\n" +
		"Author: K5LEB\n" +
		"\n" +
		"----\n" +
		"  Title: not a header\n" +
		"\n" +
		"  ----\n"
	got, err = ParseMicron(page)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "Title: not a header\n\n----" {
		t.Fatalf("%#v", got)
	}
}

func TestWrapBoundaries(t *testing.T) {
	created := time.Date(2026, 10, 11, 18, 4, 5, 0, time.UTC)
	fits := strings.Repeat("a", 70) + " b"
	wraps := strings.Repeat("a", 71) + " b"
	long := strings.Repeat("a", 73)
	wide := strings.Repeat("é", 72)
	cases := []struct {
		body  string
		lines []string
	}{
		{fits, []string{fits}},
		{wraps, []string{strings.Repeat("a", 71), "b"}},
		{long, []string{long}},
		{wide, []string{wide}},
	}
	for _, tc := range cases {
		a := Article{Title: "T", Author: "K5LEB", Created: created, Body: tc.body}
		packet, err := a.RenderPacket()
		if err != nil {
			t.Fatal(err)
		}
		_, body, ok := strings.Cut(packet, "\n\n")
		if !ok {
			t.Fatalf("no body in %q", packet)
		}
		got := strings.Split(strings.TrimRight(body, "\n"), "\n")
		if strings.Join(got, "\n") != strings.Join(tc.lines, "\n") {
			t.Fatalf("body %q\n got %q\nwant %q", tc.body, got, tc.lines)
		}
		back, err := ParsePacket(packet)
		if err != nil {
			t.Fatal(err)
		}
		want, err := a.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		if back != want {
			t.Fatalf("round trip %#v want %#v", back, want)
		}
		for _, line := range got {
			n := utf8.RuneCountInString(line)
			if n > PacketWidth && strings.Contains(line, " ") {
				t.Fatalf("wrapped line is %d runes: %q", n, line)
			}
		}
	}
}

func TestBlankIDIsOmitted(t *testing.T) {
	a := Article{
		ID:      "   ",
		Title:   "Quiet",
		Author:  "K5LEB",
		Created: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	got, err := a.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "" {
		t.Fatalf("id %q", got.ID)
	}
	packet, err := a.RenderPacket()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(packet, "ID:") {
		t.Fatalf("blank id was rendered:\n%s", packet)
	}
}

func TestParserErrors(t *testing.T) {
	created := "2026-10-11T18:04:05Z"
	cases := []struct {
		name string
		fn   func() error
		want string
	}{
		{
			name: "packet missing created",
			fn: func() error {
				_, err := ParsePacket("Title: Hi\nAuthor: K5LEB\n\n")
				return err
			},
			want: "created time is missing",
		},
		{
			name: "packet blank created",
			fn: func() error {
				_, err := ParsePacket("Title: Hi\nAuthor: K5LEB\nCreated:   \n\n")
				return err
			},
			want: "created time is missing",
		},
		{
			name: "packet bad created",
			fn: func() error {
				_, err := ParsePacket("Title: Hi\nAuthor: K5LEB\nCreated: nope\n\n")
				return err
			},
			want: `created time "nope" is not RFC3339`,
		},
		{
			name: "packet missing title",
			fn: func() error {
				_, err := ParsePacket("Author: K5LEB\nCreated: " + created + "\n\n")
				return err
			},
			want: "title is missing",
		},
		{
			name: "duplicate title",
			fn: func() error {
				_, err := ParsePacket("Title: Hi\nTitle: No\nAuthor: K5LEB\nCreated: " + created + "\n\n")
				return err
			},
			want: `duplicate packet header "Title"`,
		},
		{
			name: "unknown packet header",
			fn: func() error {
				_, err := ParsePacket("Title: Hi\nAuthor: K5LEB\nCreated: " + created + "\nPath: none\n\n")
				return err
			},
			want: `unknown packet header "Path"`,
		},
		{
			name: "micron missing created",
			fn: func() error {
				_, err := ParseMicron("> Hi\nAuthor: K5LEB\n\n----\n")
				return err
			},
			want: "created time is missing",
		},
		{
			name: "duplicate micron author",
			fn: func() error {
				_, err := ParseMicron("> Hi\nAuthor: K5LEB\nAuthor: W1AW\nCreated: " + created + "\n\n----\n")
				return err
			},
			want: `duplicate micron header "Author"`,
		},
		{
			name: "unknown micron header",
			fn: func() error {
				_, err := ParseMicron("> Hi\nAuthor: K5LEB\nCreated: " + created + "\nPath: none\n\n----\n")
				return err
			},
			want: `unknown micron header "Path"`,
		},
		{
			name: "bad escape",
			fn: func() error {
				_, err := ParseMicron("> Hi\\q\nAuthor: K5LEB\nCreated: " + created + "\n\n----\n")
				return err
			},
			want: "unknown escape",
		},
		{
			name: "dangling backslash",
			fn: func() error {
				_, err := ParseMicron("> Hi\\\nAuthor: K5LEB\nCreated: " + created + "\n\n----\n")
				return err
			},
			want: "backslash at end of text",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.fn()
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestRejectsTextThatCannotRoundTrip(t *testing.T) {
	created := time.Date(2026, 10, 11, 18, 4, 5, 0, time.UTC)
	base := Article{Title: "T", Author: "K5LEB", Created: created, Body: "Hello"}
	cases := []Article{
		{Title: "bad\xff", Author: base.Author, Created: created},
		{Title: "foo\u2028bar", Author: base.Author, Created: created},
		{Title: "foo\u0085bar", Author: base.Author, Created: created},
		{Title: base.Title, Author: "K5\x00LEB", Created: created},
		{Title: base.Title, Author: base.Author, Created: time.Date(10000, 1, 2, 3, 4, 5, 0, time.UTC)},
		{Title: base.Title, Author: base.Author, Created: time.Date(-1, 1, 2, 3, 4, 5, 0, time.UTC)},
		{Title: base.Title, Author: base.Author, Created: created, Body: "hello\x00world"},
		{Title: base.Title, Author: base.Author, Created: created, Body: "hello\x1b[31m"},
		{Title: base.Title, Author: base.Author, Created: created, Body: "bad\xff"},
		{Title: base.Title, Author: base.Author, Created: created, Body: "foo\u2028bar"},
	}
	for _, a := range cases {
		if _, err := a.Canonical(); err == nil {
			t.Fatalf("expected error for %#v", a)
		}
		if _, err := a.RenderPacket(); err == nil {
			t.Fatalf("packet render accepted %#v", a)
		}
		if _, err := a.RenderMicron(); err == nil {
			t.Fatalf("micron render accepted %#v", a)
		}
	}
	collapsed, err := Article{
		Title:   "T",
		Author:  "K5LEB",
		Created: created,
		Body:    "foo\u00a0bar\tbaz",
	}.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if collapsed.Body != "foo bar baz" {
		t.Fatalf("body %q", collapsed.Body)
	}
}
