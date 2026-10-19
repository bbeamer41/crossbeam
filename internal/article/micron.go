// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package article

import (
	"fmt"
	"strings"
	"time"
)

// RenderMicron returns a NomadNet Micron page. The title is a level-1
// heading. Author, Created, and ID are plain lines. A divider follows.
// Each body line is indented by two spaces so a line the author wrote
// cannot become a Micron heading or divider. Backslashes and backticks
// in text are escaped with a backslash. The result always ends with a
// newline.
func (a Article) RenderMicron() (string, error) {
	c, err := a.Canonical()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "> %s\n", escapeInline(c.Title))
	fmt.Fprintf(&b, "Author: %s\n", escapeInline(c.Author))
	fmt.Fprintf(&b, "Created: %s\n", c.Created.Format(time.RFC3339))
	if c.ID != "" {
		fmt.Fprintf(&b, "ID: %s\n", escapeInline(c.ID))
	}
	b.WriteString("\n----\n")
	if c.Body != "" {
		paras := strings.Split(c.Body, "\n\n")
		for i, para := range paras {
			if i > 0 {
				b.WriteByte('\n')
			}
			for _, line := range wrap(para, PacketWidth) {
				fmt.Fprintf(&b, "  %s\n", escapeInline(line))
			}
		}
	}
	return b.String(), nil
}

// ParseMicron reads a page produced by RenderMicron.
func ParseMicron(text string) (Article, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimPrefix(text, "\uFEFF")
	text = strings.TrimRight(text, "\n")
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || lines[0] == "" {
		return Article{}, fmt.Errorf("micron page is empty")
	}
	if !strings.HasPrefix(lines[0], "> ") {
		return Article{}, fmt.Errorf("micron page must start with a \"> \" heading")
	}
	title, err := unescapeInline(strings.TrimPrefix(lines[0], "> "))
	if err != nil {
		return Article{}, fmt.Errorf("micron title: %w", err)
	}
	divider := -1
	fields := map[string]string{"Title": title}
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if line == "----" {
			divider = i
			break
		}
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ": ")
		if !ok {
			return Article{}, fmt.Errorf("micron header line %d must look like \"Name: value\"", i+1)
		}
		raw, err := unescapeInline(val)
		if err != nil {
			return Article{}, fmt.Errorf("micron header %s: %w", key, err)
		}
		switch key {
		case "Author", "Created", "ID":
		default:
			return Article{}, fmt.Errorf("unknown micron header %q", key)
		}
		if _, dup := fields[key]; dup {
			return Article{}, fmt.Errorf("duplicate micron header %q", key)
		}
		fields[key] = raw
	}
	if divider < 0 {
		return Article{}, fmt.Errorf("micron page is missing the ---- divider")
	}
	body, err := parseMicronBody(lines[divider+1:])
	if err != nil {
		return Article{}, err
	}
	created, err := time.Parse(time.RFC3339, fields["Created"])
	if err != nil {
		return Article{}, fmt.Errorf("created time %q is not RFC3339", fields["Created"])
	}
	return Article{
		ID:      fields["ID"],
		Title:   fields["Title"],
		Author:  fields["Author"],
		Created: created,
		Body:    body,
	}.Canonical()
}

func parseMicronBody(lines []string) (string, error) {
	var paras []string
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		paras = append(paras, strings.Join(cur, " "))
		cur = nil
	}
	for i, line := range lines {
		if line == "" {
			flush()
			continue
		}
		if !strings.HasPrefix(line, "  ") {
			return "", fmt.Errorf("micron body line %d must be indented two spaces", i+1)
		}
		content, err := unescapeInline(line[2:])
		if err != nil {
			return "", fmt.Errorf("micron body line %d: %w", i+1, err)
		}
		cur = append(cur, content)
	}
	flush()
	return strings.Join(paras, "\n\n"), nil
}

func escapeInline(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '\\' || r == '`' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func unescapeInline(s string) (string, error) {
	var b strings.Builder
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '\\' {
			b.WriteRune(runes[i])
			continue
		}
		if i+1 >= len(runes) {
			return "", fmt.Errorf("backslash at end of text")
		}
		next := runes[i+1]
		if next != '\\' && next != '`' {
			return "", fmt.Errorf("unknown escape \\%c", next)
		}
		b.WriteRune(next)
		i++
	}
	return b.String(), nil
}
