// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package meshcore

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// Companion serial framing. A frame from the radio starts with '>'.
// A frame to the radio starts with '<'. The next two bytes are the
// payload length, little-endian.
const (
	markFromRadio = '>'
	markToRadio   = '<'

	cmdAppStart      = 0x01
	cmdSendText      = 0x02
	cmdSetTime       = 0x06
	cmdSelfAdvert    = 0x07
	cmdSetAdvertName = 0x08
	cmdAddContact    = 0x09
	cmdSyncMessage   = 0x0A
	cmdDeviceQuery   = 0x16
	deviceQueryVer   = 0x03
	respOK           = 0x00
	respSelfInfo     = 0x05
	respMsgSent      = 0x06
	respContactMsg   = 0x07
	respChannelMsg   = 0x08
	respNoMore       = 0x0A
	respError        = 0x01
	respDeviceInfo   = 0x0D
	respContactMsgV3 = 0x10
	respChannelMsgV3 = 0x11
	respChannelData  = 0x1B
	pushNewAdvert    = 0x8A
	pushMsgWaiting   = 0x83
	pushLog          = 0x88
	txtTypeSigned    = 2
	advTypeChat      = 1
	contactPushLen   = 148
	advertTypeAt     = 33
	advertNameMax    = 31
	maxFrame         = 1024
	maxSkip          = 4096
	maxTextRunes     = 133
	maxTextBytes     = 160
	maxChunks        = 16
	chunkMarkerRunes = 8
	restNotice       = "The rest of this bulletin is on the packet board."
)

var errFrame = errors.New("meshcore frame is not valid")

func appStart() []byte {
	out := make([]byte, 8, 8+len("crossbeam"))
	out[0] = cmdAppStart
	return append(out, "crossbeam"...)
}

func deviceQuery() []byte {
	return []byte{cmdDeviceQuery, deviceQueryVer}
}

func syncNext() []byte {
	return []byte{cmdSyncMessage}
}

func setDeviceTime(now time.Time) []byte {
	out := []byte{cmdSetTime, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(out[1:], uint32(now.Unix()))
	return out
}

// advertName is the companion node name. The firmware keeps 31 bytes and
// drops an advert that has none, so a blank or broken name is not sent.
func advertName(name string) ([]byte, bool) {
	name = strings.TrimSpace(name)
	if name == "" || strings.IndexByte(name, 0) >= 0 || !utf8.ValidString(name) {
		return nil, false
	}
	b := []byte(name)
	if len(b) > advertNameMax {
		b = b[:advertNameMax]
		for len(b) > 0 && !utf8.Valid(b) {
			b = b[:len(b)-1]
		}
	}
	if len(b) == 0 {
		return nil, false
	}
	out := make([]byte, 1+len(b))
	out[0] = cmdSetAdvertName
	copy(out[1:], b)
	return out, true
}

// selfAdvert asks for one flood. The second byte is the flood flag.
func selfAdvert() []byte {
	return []byte{cmdSelfAdvert, 1}
}

// chatAdvert is a full new-advert push for a companion. The firmware
// reads 64 path bytes out of an add-contact command, so a short push is
// not one. Repeaters, rooms, and sensors are a different type byte.
func chatAdvert(frame []byte) bool {
	return len(frame) >= contactPushLen && frame[0] == pushNewAdvert && frame[advertTypeAt] == advTypeChat
}

func addContact(frame []byte) []byte {
	out := append([]byte(nil), frame...)
	out[0] = cmdAddContact
	return out
}

func sendText(prefix []byte, text string) ([]byte, error) {
	if len(prefix) != 6 {
		return nil, errFrame
	}
	if len(text) > maxTextBytes || utf8.RuneCountInString(text) > maxTextRunes || strings.IndexByte(text, 0) >= 0 || !utf8.ValidString(text) {
		return nil, errFrame
	}
	out := make([]byte, 0, 13+len(text))
	out = append(out, cmdSendText, 0, 0)
	var ts [4]byte
	binary.LittleEndian.PutUint32(ts[:], uint32(time.Now().Unix()))
	out = append(out, ts[:]...)
	out = append(out, prefix...)
	out = append(out, text...)
	return out, nil
}

type contact struct {
	prefix []byte
	text   string
	when   uint32
}

func parseContact(frame []byte) (contact, bool) {
	if len(frame) == 0 {
		return contact{}, false
	}
	offset := 1
	switch frame[0] {
	case respContactMsg:
	case respContactMsgV3:
		offset += 3
	default:
		return contact{}, false
	}
	if len(frame) < offset+6+2+4 {
		return contact{}, false
	}
	prefix := append([]byte(nil), frame[offset:offset+6]...)
	offset += 6
	txtType := frame[offset+1]
	offset += 2
	when := binary.LittleEndian.Uint32(frame[offset : offset+4])
	offset += 4
	if txtType == txtTypeSigned {
		if len(frame) < offset+4 {
			return contact{}, false
		}
		offset += 4
	}
	text := strings.ToValidUTF8(string(frame[offset:]), "")
	return contact{prefix: prefix, text: text, when: when}, true
}

func isChannel(frame []byte) bool {
	return len(frame) > 0 && (frame[0] == respChannelMsg || frame[0] == respChannelMsgV3 || frame[0] == respChannelData)
}

func writeFrame(w io.Writer, mark byte, payload []byte) error {
	if len(payload) == 0 || len(payload) > maxFrame {
		return errFrame
	}
	buf := make([]byte, 3+len(payload))
	buf[0] = mark
	binary.LittleEndian.PutUint16(buf[1:3], uint16(len(payload)))
	copy(buf[3:], payload)
	// One Write can still return early. A torn header is a lost command.
	wrote := 0
	for wrote < len(buf) {
		n, err := w.Write(buf[wrote:])
		wrote += n
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func readFrame(r io.Reader, mark byte) ([]byte, error) {
	var one [1]byte
	skipped := 0
	for {
		if _, err := io.ReadFull(r, one[:]); err != nil {
			return nil, err
		}
		if one[0] == mark {
			break
		}
		// A port that never sends a mark must not spin the reader.
		skipped++
		if skipped > maxSkip {
			return nil, errFrame
		}
	}
	var nbuf [2]byte
	if _, err := io.ReadFull(r, nbuf[:]); err != nil {
		return nil, err
	}
	n := int(binary.LittleEndian.Uint16(nbuf[:]))
	if n == 0 || n > maxFrame {
		return nil, errFrame
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// chunk splits a reply into MeshCore text messages. A reply that fits
// in one message has no chunk mark. A longer reply is marked [1/3].
// At most maxChunks messages are returned. The last of those says the
// rest stays on the packet board.
func chunk(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return []string{""}
	}
	if fits(text) {
		return []string{text}
	}
	budgetRunes := maxTextRunes - chunkMarkerRunes
	var parts []string
	rest := text
	for rest != "" && len(parts) < maxChunks {
		piece := take(rest, budgetRunes, maxTextBytes-chunkMarkerRunes)
		if piece == "" {
			_, size := utf8.DecodeRuneInString(rest)
			rest = rest[size:]
			continue
		}
		parts = append(parts, strings.TrimRight(piece, " \n"))
		rest = strings.TrimLeft(rest[len(piece):], " \n")
	}
	if rest != "" && len(parts) > 0 {
		parts[len(parts)-1] = restNotice
	}
	out := make([]string, len(parts))
	for i, part := range parts {
		marked := fmt.Sprintf("[%d/%d] %s", i+1, len(parts), part)
		if !fits(marked) {
			marked = trimToFit(marked)
		}
		out[i] = marked
	}
	return out
}

func fits(text string) bool {
	return len(text) <= maxTextBytes && utf8.RuneCountInString(text) <= maxTextRunes
}

func take(s string, maxRunes, maxBytes int) string {
	if maxRunes < 1 || maxBytes < 1 || s == "" {
		return ""
	}
	best := 0
	runes := 0
	lastBreak := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if runes+1 > maxRunes || i+size > maxBytes {
			break
		}
		i += size
		runes++
		best = i
		if r == '\n' || r == ' ' {
			lastBreak = i
		}
	}
	if best == 0 {
		return ""
	}
	if best == len(s) || lastBreak == 0 || lastBreak < best/2 {
		return s[:best]
	}
	return s[:lastBreak]
}

func trimToFit(text string) string {
	for !fits(text) && text != "" {
		_, size := utf8.DecodeLastRuneInString(text)
		text = text[:len(text)-size]
	}
	return text
}

func prefixKey(prefix []byte, when uint32, text string) string {
	return hex.EncodeToString(prefix) + ":" + fmt.Sprintf("%d", when) + ":" + text
}
