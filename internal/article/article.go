// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package article is the canonical bulletin used by Crossbeam.
// One article renders to a packet-radio bulletin and to a NomadNet
// Micron page, and either rendering imports back to the same article.
package article

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// PacketWidth is the body line width for a packet bulletin and for a
// Micron page. Headers stay on one line even when they are longer.
const PacketWidth = 72

// Article is one bulletin. Body is plain text: paragraphs are separated
// by a blank line, and a paragraph is a single line of words. Renderers
// wrap paragraphs for display. Importers join those wrapped lines back
// into the same paragraphs.
type Article struct {
	ID      string
	Title   string
	Author  string
	Created time.Time
	Body    string
}

// Canonical checks the article and returns the normalized form that
// render and import use. Created is UTC, truncated to one second, and
// must survive a write and read as RFC3339. Body paragraphs are
// separated by one blank line.
func (a Article) Canonical() (Article, error) {
	title, err := singleLine("title", a.Title)
	if err != nil {
		return Article{}, err
	}
	if title == "" {
		return Article{}, fmt.Errorf("title is empty")
	}
	author, err := singleLine("author", a.Author)
	if err != nil {
		return Article{}, err
	}
	if author == "" {
		return Article{}, fmt.Errorf("author is empty")
	}
	id, err := singleLine("id", a.ID)
	if err != nil {
		return Article{}, err
	}
	created, err := canonicalCreated(a.Created)
	if err != nil {
		return Article{}, err
	}
	body, err := normalizeBody(a.Body)
	if err != nil {
		return Article{}, err
	}
	return Article{
		ID:      id,
		Title:   title,
		Author:  author,
		Created: created,
		Body:    body,
	}, nil
}

func singleLine(name, value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("%s is not valid UTF-8", name)
	}
	value = strings.TrimSpace(value)
	for _, r := range value {
		switch {
		case r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029':
			return "", fmt.Errorf("%s must be a single line", name)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			return "", fmt.Errorf("%s contains a control character", name)
		}
	}
	return value, nil
}

// canonicalCreated stores a time that Render writes and Parse reads
// back as the same instant. Go's RFC3339 layout round-trips years
// 0000 through 9999. Later and earlier years do not.
func canonicalCreated(t time.Time) (time.Time, error) {
	if t.IsZero() {
		return time.Time{}, fmt.Errorf("created time is missing")
	}
	u := t.UTC().Truncate(time.Second)
	formatted := u.Format(time.RFC3339)
	parsed, err := time.Parse(time.RFC3339, formatted)
	if err != nil || !parsed.Equal(u) {
		return time.Time{}, fmt.Errorf("created time cannot be written as RFC3339")
	}
	return u, nil
}

// normalizeBody turns arbitrary text into canonical paragraphs.
func normalizeBody(body string) (string, error) {
	if !utf8.ValidString(body) {
		return "", fmt.Errorf("body is not valid UTF-8")
	}
	for _, r := range body {
		switch r {
		case '\n', '\r', '\t', '\v', '\f':
			continue
		case '\u2028', '\u2029':
			return "", fmt.Errorf("body contains a line separator")
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return "", fmt.Errorf("body contains a control character")
		}
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	chunks := strings.Split(body, "\n\n")
	var paras []string
	for _, chunk := range chunks {
		words := strings.Fields(chunk)
		if len(words) == 0 {
			continue
		}
		paras = append(paras, strings.Join(words, " "))
	}
	return strings.Join(paras, "\n\n"), nil
}

// wrap breaks one paragraph into lines of at most width runes.
// A word longer than width stays intact on its own line.
func wrap(paragraph string, width int) []string {
	words := strings.Fields(paragraph)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	cur := ""
	curLen := 0
	for _, word := range words {
		wlen := utf8.RuneCountInString(word)
		if cur == "" {
			cur = word
			curLen = wlen
			continue
		}
		if curLen+1+wlen <= width {
			cur += " " + word
			curLen += 1 + wlen
			continue
		}
		lines = append(lines, cur)
		cur = word
		curLen = wlen
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func renderBody(body string) string {
	if body == "" {
		return ""
	}
	var parts []string
	for _, para := range strings.Split(body, "\n\n") {
		parts = append(parts, strings.Join(wrap(para, PacketWidth), "\n"))
	}
	return strings.Join(parts, "\n\n")
}
