// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"math"
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
	return bannedWord(text, nil)
}

// Obscene reports whether text has a built-in blocked word or one of
// the station's extra words. Matching is by whole word.
func Obscene(text string, extra []string) bool {
	return bannedWord(text, extra)
}

func bannedWord(text string, extra []string) bool {
	more := map[string]bool{}
	for _, w := range extra {
		more[strings.ToLower(w)] = true
	}
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	for _, w := range words {
		if blocked[w] || more[w] {
			return true
		}
	}
	return false
}

// Encoded reports whether text looks like it hides its meaning: a long
// run of base64, hex, or a similar unbroken high-entropy token
// (47 CFR 97.113(a)(4)). A web address, a callsign, a grid square, or a
// long ordinary word does not count.
func Encoded(text string) bool {
	for _, tok := range strings.Fields(text) {
		tok = strings.Trim(tok, ".,;:!?()[]\"'")
		if len(tok) < 32 || strings.Contains(tok, "://") {
			continue
		}
		if encodedToken(tok) {
			return true
		}
	}
	return false
}

// encodedToken is true for a single base64-alphabet token. Mixed letters
// and digits are the usual hex or base64 case. An all-letter token counts
// when it is mixed case the way base64 is, or when it is high-entropy and
// not an ordinary word.
func encodedToken(tok string) bool {
	letters, digits, other := 0, 0, 0
	upper, lower, vowels, transitions := 0, 0, 0, 0
	var prev rune
	for i, r := range tok {
		switch {
		case r >= 'a' && r <= 'z':
			letters++
			lower++
			if r == 'a' || r == 'e' || r == 'i' || r == 'o' || r == 'u' {
				vowels++
			}
			if i > 0 && prev >= 'A' && prev <= 'Z' {
				transitions++
			}
		case r >= 'A' && r <= 'Z':
			letters++
			upper++
			if r == 'A' || r == 'E' || r == 'I' || r == 'O' || r == 'U' {
				vowels++
			}
			if i > 0 && prev >= 'a' && prev <= 'z' {
				transitions++
			}
		case r >= '0' && r <= '9':
			digits++
		case r == '+' || r == '/' || r == '=' || r == '_' || r == '-':
		default:
			other++
		}
		prev = r
	}
	if other > 0 || letters == 0 {
		return false
	}
	if digits > 0 {
		return true
	}
	if upper > 0 && lower > 0 && transitions >= 4 {
		return true
	}
	// A long lowercase blob with almost no vowels is not an English word.
	if vowels*8 < len(tok) && shannon(tok) >= 4 && uniqueRunes(tok) >= 16 {
		return true
	}
	if shannon(tok) >= 4.8 && uniqueRunes(tok) >= 20 {
		return true
	}
	return false
}

func shannon(s string) float64 {
	if s == "" {
		return 0
	}
	counts := map[rune]int{}
	n := 0
	for _, r := range s {
		counts[r]++
		n++
	}
	h := 0.0
	nf := float64(n)
	for _, c := range counts {
		p := float64(c) / nf
		h -= p * math.Log2(p)
	}
	return h
}

func uniqueRunes(s string) int {
	seen := map[rune]struct{}{}
	for _, r := range s {
		seen[r] = struct{}{}
	}
	return len(seen)
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
