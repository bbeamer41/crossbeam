// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"strings"
	"unicode"
)

// Width is the longest line written to a packet terminal.
const Width = 76

// blocked is a short list of words that are not sent on amateur radio
// (47 CFR 97.113(a)(4)). The model is told the same thing; this list
// catches a reply that ignores it. Matching is by whole word, so
// "Scunthorpe" and "class" pass.
var blocked = map[string]bool{
	"fuck": true, "fucking": true, "fucker": true, "fucked": true, "motherfucker": true,
	"shit": true, "shitty": true, "bullshit": true,
	"cunt": true, "cunts": true,
	"cocksucker": true,
	"piss":       true, "pissed": true,
	"tits":    true,
	"asshole": true, "assholes": true,
	"bitch": true, "bitches": true,
	"bastard": true,
	"nigger":  true, "niggers": true, "faggot": true, "faggots": true,
	"retard": true,
}

// Profane reports whether text has a word from the blocked list.
func Profane(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	for _, w := range words {
		if blocked[w] {
			return true
		}
	}
	return false
}

// Encoded reports whether text looks like it hides its meaning: a long
// run of base64 or hex with no spaces (47 CFR 97.113(a)(4)). A web
// address or a long ordinary word does not count.
func Encoded(text string) bool {
	for _, tok := range strings.Fields(text) {
		tok = strings.Trim(tok, ".,;:!?()[]\"'")
		if len(tok) < 32 || strings.Contains(tok, "://") {
			continue
		}
		letters, digits, other := 0, 0, 0
		for _, r := range tok {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
				letters++
			case r >= '0' && r <= '9':
				digits++
			case r == '+' || r == '/' || r == '=' || r == '_' || r == '-':
			default:
				other++
			}
		}
		if other == 0 && digits > 0 && letters > 0 {
			return true
		}
	}
	return false
}

var punct = strings.NewReplacer(
	"\u2018", "'", "\u2019", "'", "\u201c", "\"", "\u201d", "\"",
	"\u2013", "-", "\u2014", "-", "\u2026", "...", "\u00a0", " ",
	"\u2022", "-", "\u00b0", " deg", "**", "", "__", "", "`", "",
)

// Plain turns a model answer into ASCII text for a packet terminal:
// markdown marks are dropped, other non-ASCII runes are removed, and
// runs of blank lines become one.
func Plain(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = punct.Replace(text)
	var lines []string
	blank := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "#")
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "* ") {
			line = "- " + line[2:]
		}
		var b strings.Builder
		for _, r := range line {
			if r == '\t' {
				r = ' '
			}
			if r >= 0x20 && r < 0x7f {
				b.WriteRune(r)
			}
		}
		line = strings.Join(strings.Fields(b.String()), " ")
		if line == "" {
			if !blank && len(lines) > 0 {
				lines = append(lines, "")
			}
			blank = true
			continue
		}
		blank = false
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// Cut shortens text to at most max bytes at a word boundary and marks
// the cut.
func Cut(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	const mark = " [cut]"
	limit := max - len(mark)
	if limit < 1 {
		limit = 1
	}
	head := text[:limit]
	if i := strings.LastIndexAny(head, " \n"); i > limit/2 {
		head = head[:i]
	}
	return strings.TrimRight(head, " \n") + mark
}

// Wrap breaks each line at Width columns on a space.
func Wrap(text string, width int) []string {
	if width < 20 {
		width = Width
	}
	var out []string
	for _, para := range strings.Split(text, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		line := ""
		for _, word := range strings.Fields(para) {
			for len(word) > width {
				if line != "" {
					out = append(out, line)
					line = ""
				}
				out = append(out, word[:width])
				word = word[width:]
			}
			switch {
			case line == "":
				line = word
			case len(line)+1+len(word) <= width:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
