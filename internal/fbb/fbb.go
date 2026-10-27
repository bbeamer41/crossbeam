// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package fbb is the ASCII form of FBB forwarding. Feature F is the
// batch of proposals. Compressed forwarding is not implemented, so a
// peer that cannot do ASCII stops. The bytes are readable text.
package fbb

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bbeamer41/crossbeam/internal/ax25"
)

const (
	// CtrlZ ends one message body.
	CtrlZ = 0x1A
	// MaxProposals is the number of FB lines in one block.
	MaxProposals = 5
	// MaxBlock is the usual size of one block, in bytes of message text.
	// One message may be larger and is sent as a block by itself.
	MaxBlock = 10000
	// MaxSubjectRunes is the subject length on the wire.
	MaxSubjectRunes = 80
	// MaxBID is the bulletin id length. The 1993 specification uses 12.
	MaxBID = 12
	// MaxTo is the category length.
	MaxTo = 12
	// MaxAt is the distribution length.
	MaxAt = 40
	// MinSize is the smallest message the receiver will read.
	MinSize = 2
	// MaxMessage is the largest message the receiver will read.
	MaxMessage = 256 * 1024
)

// SID is the station identification sent at the start of forwarding.
// The letters are F (batch forward), H (hierarchical address), M (message
// id), and $ (bulletin id). Compressed and restart flags are not claimed.
func SID(version string) (string, error) {
	if version == "" || strings.ContainsAny(version, "[]\r\n") {
		return "", errors.New("version is not valid in a SID")
	}
	return "[CROSSBEAM-" + version + "-FHM$]", nil
}

// Proposal is one FB line: FB type from at to bid size.
type Proposal struct {
	Type string
	From string
	At   string
	To   string
	BID  string
	Size int
}

// Line is the proposal without the carriage return.
func (p Proposal) Line() string {
	return fmt.Sprintf("FB %s %s %s %s %s %d", p.Type, p.From, p.At, p.To, p.BID, p.Size)
}

// Checksum is the ASCII proposal checksum published for F6FBB. Each
// proposal line contributes its bytes and the carriage return that ends
// it. The F> line is not included. The value is the two's complement of
// that 8-bit sum, printed as two uppercase hex digits. The checksum on a
// compressed binary block is a different sum and is not implemented.
func Checksum(lines []string) byte {
	var sum byte
	for _, line := range lines {
		for i := 0; i < len(line); i++ {
			sum += line[i]
		}
		sum += '\r'
	}
	return -sum
}

// EncodeProposals writes the FB lines and the F> checksum line.
// Every line ends with a carriage return.
func EncodeProposals(list []Proposal) ([]byte, error) {
	if len(list) < 1 || len(list) > MaxProposals {
		return nil, errors.New("proposal count is out of range")
	}
	lines := make([]string, len(list))
	var buf bytes.Buffer
	for i, p := range list {
		next, err := canonical(p)
		if err != nil {
			return nil, err
		}
		lines[i] = next.Line()
		buf.WriteString(lines[i])
		buf.WriteByte('\r')
	}
	fmt.Fprintf(&buf, "F> %02X\r", Checksum(lines))
	return buf.Bytes(), nil
}

func canonical(p Proposal) (Proposal, error) {
	p.Type = strings.ToUpper(p.Type)
	if p.Type != "B" && p.Type != "P" && p.Type != "T" {
		return Proposal{}, errors.New("proposal type is not B, P, or T")
	}
	from, err := ax25.Parse(p.From)
	if err != nil {
		return Proposal{}, errors.New("proposal sender is not a callsign")
	}
	p.From = from.String()
	p.At = strings.ToUpper(p.At)
	p.To = strings.ToUpper(p.To)
	p.BID = strings.ToUpper(p.BID)
	if !ValidAt(p.At) || !ValidTo(p.To) || !ValidBID(p.BID) {
		return Proposal{}, errors.New("proposal field is not valid")
	}
	if p.Size < MinSize || p.Size > MaxMessage {
		return Proposal{}, errors.New("proposal size is not valid")
	}
	return p, nil
}

// ParseProposal reads one FB line. The checksum uses the raw line, so
// this does not reformat it.
func ParseProposal(line string) (Proposal, error) {
	parts := strings.Fields(line)
	if len(parts) != 7 || !strings.EqualFold(parts[0], "FB") {
		return Proposal{}, errors.New("proposal is missing a field")
	}
	if len(parts[6]) > 7 {
		return Proposal{}, errors.New("message is too large")
	}
	size, err := strconv.Atoi(parts[6])
	if err != nil || (len(parts[6]) > 1 && parts[6][0] == '0') {
		return Proposal{}, errors.New("proposal size is not valid")
	}
	p, err := canonical(Proposal{
		Type: parts[1],
		From: parts[2],
		At:   parts[3],
		To:   parts[4],
		BID:  parts[5],
		Size: size,
	})
	if err != nil {
		return Proposal{}, err
	}
	if p.Size > MaxMessage {
		return Proposal{}, errors.New("message is too large")
	}
	return p, nil
}

// ParseForwardEnd reads the F> line. A line with no hex digits has no
// checksum, which the receiver accepts. A bad pair is an error.
func ParseForwardEnd(line string) (hasSum bool, sum byte, err error) {
	trim := strings.ToUpper(strings.TrimSpace(line))
	if trim == "F>" {
		return false, 0, nil
	}
	if !strings.HasPrefix(trim, "F>") {
		return false, 0, errors.New("bad proposal")
	}
	rest := strings.TrimSpace(trim[2:])
	if len(rest) != 2 {
		return false, 0, errors.New("bad checksum")
	}
	v, conv := strconv.ParseUint(rest, 16, 8)
	if conv != nil {
		return false, 0, errors.New("bad checksum")
	}
	return true, byte(v), nil
}

// ParseFS reads the accept, reject, and defer marks for one block.
// R and E are rejects. H is a defer. The count must match the block.
func ParseFS(line string, n int) ([]byte, error) {
	trim := strings.TrimSpace(line)
	if len(trim) < 2 || !strings.EqualFold(trim[:2], "FS") {
		return nil, errors.New("expected FS")
	}
	rest := strings.ReplaceAll(strings.TrimSpace(trim[2:]), " ", "")
	if n < 1 || len(rest) != n {
		return nil, errors.New("FS response does not match the proposal")
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		switch rest[i] {
		case '+', '-', '=':
			out[i] = rest[i]
		case 'r', 'R':
			out[i] = 'R'
		case 'e', 'E':
			out[i] = 'E'
		case 'h', 'H':
			out[i] = 'H'
		default:
			return nil, errors.New("FS response does not match the proposal")
		}
	}
	return out, nil
}

// EncodeMessage builds the bytes counted by the proposal size: the
// subject, a carriage return, the body with carriage returns, and Ctrl-Z.
func EncodeMessage(title, body string) ([]byte, error) {
	if strings.TrimSpace(title) == "" {
		return nil, errors.New("title is empty")
	}
	if n := utf8.RuneCountInString(title); n > MaxSubjectRunes {
		return nil, fmt.Errorf("title is %d runes, above the bulletin limit of %d", n, MaxSubjectRunes)
	}
	if strings.ContainsAny(title, "\r\n") {
		return nil, errors.New("title must be a single line")
	}
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	var buf bytes.Buffer
	buf.WriteString(title)
	buf.WriteByte('\r')
	buf.WriteString(strings.ReplaceAll(body, "\n", "\r"))
	buf.WriteByte(CtrlZ)
	if buf.Len() < MinSize || buf.Len() > MaxMessage {
		return nil, errors.New("message size is not valid")
	}
	return buf.Bytes(), nil
}

// DecodeMessage splits a message whose size was declared in the proposal.
// The last byte must be Ctrl-Z. A carriage return, or a carriage return
// and line feed, is one line ending. The line feed does not become a
// blank line. The subject is limited to MaxSubjectRunes.
func DecodeMessage(raw []byte) (string, string, error) {
	if len(raw) < MinSize || raw[len(raw)-1] != CtrlZ {
		return "", "", errors.New("message does not end with Ctrl-Z")
	}
	payload := bytes.ReplaceAll(raw[:len(raw)-1], []byte("\r\n"), []byte("\r"))
	cut := bytes.IndexByte(payload, '\r')
	if cut < 0 {
		return "", "", errors.New("message has no subject")
	}
	title := strings.TrimSpace(string(payload[:cut]))
	if title == "" || !utf8.ValidString(title) || utf8.RuneCountInString(title) > MaxSubjectRunes {
		return "", "", errors.New("message subject is not valid")
	}
	body := string(payload[cut+1:])
	if !utf8.ValidString(body) {
		return "", "", errors.New("message is not valid UTF-8")
	}
	body = strings.ReplaceAll(body, "\r", "\n")
	return title, body, nil
}

// ValidBID reports whether text is a bulletin id on the wire.
func ValidBID(text string) bool {
	return validSet(text, MaxBID, func(c byte) bool {
		return isALNUM(c) || c == '_' || c == '-'
	})
}

// ValidTo reports whether text is a bulletin category.
func ValidTo(text string) bool {
	return validSet(text, MaxTo, isALNUM)
}

// ValidAt reports whether text is a distribution address.
func ValidAt(text string) bool {
	return validSet(text, MaxAt, func(c byte) bool {
		return isALNUM(c) || c == '.' || c == '#' || c == '-'
	})
}

func validSet(text string, max int, ok func(byte) bool) bool {
	if len(text) < 1 || len(text) > max {
		return false
	}
	for i := 0; i < len(text); i++ {
		if !ok(text[i]) {
			return false
		}
	}
	return true
}

func isALNUM(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
