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
// render and import use. Created is UTC, truncated to one second.
// Body paragraphs are separated by one blank line.
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
	if a.Created.IsZero() {
		return Article{}, fmt.Errorf("created time is missing")
	}
	return Article{
		ID:      id,
		Title:   title,
		Author:  author,
		Created: a.Created.UTC().Truncate(time.Second),
		Body:    normalizeBody(a.Body),
	}, nil
}

func singleLine(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("%s must be a single line", name)
	}
	if strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) != -1 {
		return "", fmt.Errorf("%s contains a control character", name)
	}
	return value, nil
}

// normalizeBody turns arbitrary text into canonical paragraphs.
func normalizeBody(body string) string {
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
	return strings.Join(paras, "\n\n")
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
