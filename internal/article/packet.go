// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package article

import (
	"fmt"
	"strings"
	"time"
)

// RenderPacket returns a plain-text bulletin. The header is one field
// per line. A blank line follows the header. The body is wrapped to
// PacketWidth. The result always ends with a newline.
func (a Article) RenderPacket() (string, error) {
	c, err := a.Canonical()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", c.Title)
	fmt.Fprintf(&b, "Author: %s\n", c.Author)
	fmt.Fprintf(&b, "Created: %s\n", c.Created.Format(time.RFC3339))
	if c.ID != "" {
		fmt.Fprintf(&b, "ID: %s\n", c.ID)
	}
	b.WriteByte('\n')
	if body := renderBody(c.Body); body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// ParsePacket reads a bulletin produced by RenderPacket.
func ParsePacket(text string) (Article, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimPrefix(text, "\uFEFF")
	header, body, ok := strings.Cut(text, "\n\n")
	if !ok {
		return Article{}, fmt.Errorf("packet bulletin is missing the blank line after the header")
	}
	fields, err := parsePlainHeader(header)
	if err != nil {
		return Article{}, err
	}
	return articleFromFields(fields, body)
}

func parsePlainHeader(header string) (map[string]string, error) {
	if header == "" {
		return nil, fmt.Errorf("packet bulletin header is empty")
	}
	fields := make(map[string]string)
	for i, line := range strings.Split(header, "\n") {
		if line == "" {
			return nil, fmt.Errorf("packet header line %d is empty", i+1)
		}
		key, val, ok := strings.Cut(line, ": ")
		if !ok {
			return nil, fmt.Errorf("packet header line %d must look like \"Name: value\"", i+1)
		}
		switch key {
		case "Title", "Author", "Created", "ID":
		default:
			return nil, fmt.Errorf("unknown packet header %q", key)
		}
		if _, dup := fields[key]; dup {
			return nil, fmt.Errorf("duplicate packet header %q", key)
		}
		fields[key] = val
	}
	return fields, nil
}

func articleFromFields(fields map[string]string, body string) (Article, error) {
	title, err := requiredField(fields, "Title", "title")
	if err != nil {
		return Article{}, err
	}
	author, err := requiredField(fields, "Author", "author")
	if err != nil {
		return Article{}, err
	}
	created, err := parseCreated(fields)
	if err != nil {
		return Article{}, err
	}
	return Article{
		ID:      fields["ID"],
		Title:   title,
		Author:  author,
		Created: created,
		Body:    body,
	}.Canonical()
}

func requiredField(fields map[string]string, key, name string) (string, error) {
	value, ok := fields[key]
	if !ok {
		return "", fmt.Errorf("%s is missing", name)
	}
	return value, nil
}

func parseCreated(fields map[string]string) (time.Time, error) {
	raw, ok := fields["Created"]
	if !ok {
		return time.Time{}, fmt.Errorf("created time is missing")
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("created time is missing")
	}
	created, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("created time %q is not RFC3339", raw)
	}
	return created, nil
}
