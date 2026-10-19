// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package article

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestFieldLimits(t *testing.T) {
	created := time.Date(2026, 10, 11, 18, 4, 5, 0, time.UTC)
	if MaxMessageBytes < 2*MaxBodyBytes+64*1024 {
		t.Fatalf("MaxMessageBytes %d is too small for a rendered maximum body", MaxMessageBytes)
	}

	title := strings.Repeat("😀", MaxTitleRunes)
	a := Article{
		Title:   title,
		Author:  strings.Repeat("K", MaxAuthorRunes),
		ID:      strings.Repeat("i", MaxIDRunes),
		Created: created,
		Body:    "ok",
	}
	got, err := a.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(got.Title) != MaxTitleRunes {
		t.Fatalf("title runes %d", utf8.RuneCountInString(got.Title))
	}
	packet, err := got.RenderPacket()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParsePacket(packet)
	if err != nil || back != got {
		t.Fatalf("max title round trip err=%v", err)
	}

	over := a
	over.Title += "x"
	if _, err := over.Canonical(); err == nil || !strings.Contains(err.Error(), "title is") {
		t.Fatalf("long title err %v", err)
	}
	over = a
	over.Author += "x"
	if _, err := over.Canonical(); err == nil || !strings.Contains(err.Error(), "author is") {
		t.Fatalf("long author err %v", err)
	}
	over = a
	over.ID += "x"
	if _, err := over.Canonical(); err == nil || !strings.Contains(err.Error(), "id is") {
		t.Fatalf("long id err %v", err)
	}

	body := strings.Repeat("a", MaxBodyBytes)
	full := Article{Title: "T", Author: "K5LEB", Created: created, Body: body}
	got, err = full.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != body {
		t.Fatalf("body len %d", len(got.Body))
	}
	full.Body += "a"
	if _, err := full.Canonical(); err == nil || !strings.Contains(err.Error(), "body is") {
		t.Fatalf("long body err %v", err)
	}
}

func TestParserRejectsOversizedInput(t *testing.T) {
	exact := strings.Repeat("x", MaxMessageBytes)
	for _, parse := range []func(string) (Article, error){ParsePacket, ParseMicron} {
		_, err := parse(exact)
		if err == nil || strings.Contains(err.Error(), "above the limit") {
			t.Fatalf("exact cap err %v", err)
		}
		_, err = parse(exact + "x")
		if err == nil || !strings.Contains(err.Error(), "above the limit") || len(err.Error()) > 80 {
			t.Fatalf("over cap err %v", err)
		}
	}

	line := "Title: " + strings.Repeat("a", MaxHeaderLineBytes)
	_, err := ParsePacket(line + "\nAuthor: K5LEB\nCreated: 2026-10-11T18:04:05Z\n\n")
	if err == nil || !strings.Contains(err.Error(), "too long") || strings.Contains(err.Error(), "aaa") {
		t.Fatalf("long packet header err %v", err)
	}
	heading := "> " + strings.Repeat("a", MaxHeaderLineBytes)
	_, err = ParseMicron(heading + "\n")
	if err == nil || !strings.Contains(err.Error(), "too long") || strings.Contains(err.Error(), "aaa") {
		t.Fatalf("long micron heading err %v", err)
	}
}

func TestFormatCharactersRejected(t *testing.T) {
	created := time.Date(2026, 10, 11, 18, 4, 5, 0, time.UTC)
	hidden := []string{"\u200b", "\u200e", "\u202e", "\ufeff", "\u00ad"}
	for _, h := range hidden {
		articles := []Article{
			{Title: "K5" + h + "LEB", Author: "K5LEB", Created: created},
			{Title: "T", Author: "K5" + h + "LEB", Created: created},
			{Title: "T", Author: "K5LEB", ID: "id" + h, Created: created},
			{Title: "T", Author: "K5LEB", Created: created, Body: "hello" + h + "there"},
		}
		for _, a := range articles {
			if _, err := a.Canonical(); err == nil || !strings.Contains(err.Error(), "format character") {
				t.Fatalf("accepted %q err %v", h, err)
			}
		}
	}
}

func TestMicronCommandsStayLiteral(t *testing.T) {
	created := time.Date(2026, 10, 11, 18, 4, 5, 0, time.UTC)
	a := Article{
		Title:   "`!bold`! Title: nested",
		Author:  "K5LEB`[link]",
		ID:      "id\\x",
		Created: created,
		Body: "`!bold`! and `F000 and `[Label]=/pages/other.mu\n\n" +
			"# comment\n\n" +
			"> heading\n\n" +
			"----\n\n" +
			"`{partial}\n\n" +
			"`=\n\n" +
			"<reset",
	}
	want, err := a.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	page, err := a.RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	if !backticksEscaped(page) {
		t.Fatalf("unescaped backtick:\n%s", page)
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
		t.Fatal("missing divider")
	}
	for i, line := range lines[divider+1:] {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			t.Fatalf("body line %d is a Micron command: %q", i+1, line)
		}
	}
	got, err := ParseMicron(page)
	if err != nil || got != want {
		t.Fatalf("round trip err=%v match=%v", err, got == want)
	}

	hostile := "> T\nAuthor: K5LEB\nCreated: 2026-10-11T18:04:05Z\n\n----\n`!bold`!\n"
	if _, err := ParseMicron(hostile); err == nil {
		t.Fatal("imported an unindented Micron command")
	}
}

func backticksEscaped(page string) bool {
	for i, r := range page {
		if r != '`' {
			continue
		}
		if i == 0 || page[i-1] != '\\' {
			return false
		}
	}
	return true
}
