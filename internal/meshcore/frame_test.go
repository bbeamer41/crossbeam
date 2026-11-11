// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package meshcore

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte{respSelfInfo, 0x01, 0x02}
	if err := writeFrame(&buf, markFromRadio, payload); err != nil {
		t.Fatal(err)
	}
	buf.WriteByte(0x00)
	got, err := readFrame(&buf, markFromRadio)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("%v %x", err, got)
	}
}

type oneByteWriter struct {
	buf bytes.Buffer
}

func (w *oneByteWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return w.buf.Write(p[:1])
}

func TestFrameShortWrite(t *testing.T) {
	var w oneByteWriter
	payload := []byte{cmdSendText, 0, 1, 2, 3}
	if err := writeFrame(&w, markToRadio, payload); err != nil {
		t.Fatal(err)
	}
	got, err := readFrame(&w.buf, markToRadio)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("%v %x", err, got)
	}
}

func TestReadFrameSkipsNoise(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x11, 0xff})
	payload := []byte{respSelfInfo, 0x02}
	if err := writeFrame(&buf, markFromRadio, payload); err != nil {
		t.Fatal(err)
	}
	got, err := readFrame(&buf, markFromRadio)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("%v %x", err, got)
	}
}

func TestReadFrameGivesUp(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(bytes.Repeat([]byte{0x00}, maxSkip+1))
	_, err := readFrame(&buf, markFromRadio)
	if !errors.Is(err, errFrame) {
		t.Fatalf("%v", err)
	}
}

func TestAdvertNameCap(t *testing.T) {
	long := strings.Repeat("å", 40)
	got, ok := advertName(long)
	if !ok || !utf8.Valid(got[1:]) || len(got) < 2 || len(got[1:]) > advertNameMax || got[0] != cmdSetAdvertName {
		t.Fatalf("%v %q", ok, got)
	}
	if _, ok := advertName("bad\x00"); ok {
		t.Fatal("nul name")
	}
	if _, ok := advertName("  "); ok {
		t.Fatal("blank name")
	}
}

func TestSendTextRejectsNul(t *testing.T) {
	_, err := sendText([]byte{1, 2, 3, 4, 5, 6}, "LIST\x00")
	if !errors.Is(err, errFrame) {
		t.Fatalf("%v", err)
	}
}

func TestParseContact(t *testing.T) {
	prefix := []byte{9, 8, 7, 6, 5, 4}
	plain := contactFrame(prefix, "LIST", 0, 42)
	msg, ok := parseContact(plain)
	if !ok || msg.when != 42 || msg.text != "LIST" || !bytes.Equal(msg.prefix, prefix) {
		t.Fatalf("%v %+v", ok, msg)
	}
	signed := contactFrame(prefix, "READ 1", txtTypeSigned, 7)
	msg, ok = parseContact(signed)
	if !ok || msg.text != "READ 1" || msg.when != 7 {
		t.Fatalf("signed %+v", msg)
	}
	v3 := []byte{respContactMsgV3, 4, 0, 0}
	v3 = append(v3, prefix...)
	v3 = append(v3, 0, 0, 1, 0, 0, 0)
	v3 = append(v3, []byte("L")...)
	msg, ok = parseContact(v3)
	if !ok || msg.text != "L" || !bytes.Equal(msg.prefix, prefix) {
		t.Fatalf("v3 %+v", msg)
	}
	if _, ok := parseContact([]byte{respChannelMsg, 0}); ok {
		t.Fatal("channel parsed as a contact")
	}
	if !isChannel([]byte{respChannelMsgV3, 0}) || !isChannel([]byte{respChannelData, 0}) {
		t.Fatal("channel not ignored")
	}
}

func TestChunk(t *testing.T) {
	one := chunk("No bulletins.\nK5LEB>")
	if len(one) != 1 || strings.Contains(one[0], "[1/") {
		t.Fatalf("short %#v", one)
	}
	long := strings.Repeat("word ", 80)
	parts := chunk(long)
	if len(parts) < 2 {
		t.Fatalf("parts %d", len(parts))
	}
	for i, part := range parts {
		if !fits(part) {
			t.Fatalf("part %d does not fit %d %d", i, len(part), utf8.RuneCountInString(part))
		}
		want := "[" + itoa(i+1) + "/" + itoa(len(parts)) + "] "
		if !strings.HasPrefix(part, want) {
			t.Fatalf("mark %q", part)
		}
	}
	huge := strings.Repeat("a", 4000)
	capped := chunk(huge)
	if len(capped) != maxChunks || !strings.Contains(capped[len(capped)-1], restNotice) {
		t.Fatalf("cap %d %q", len(capped), capped[len(capped)-1])
	}
}

func contactFrame(prefix []byte, text string, txtType byte, when uint32) []byte {
	out := []byte{respContactMsg}
	out = append(out, prefix...)
	out = append(out, 0, txtType)
	var ts [4]byte
	putUint32(ts[:], when)
	out = append(out, ts[:]...)
	if txtType == txtTypeSigned {
		out = append(out, 0, 0, 0, 0)
	}
	return append(out, text...)
}

func putUint32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
