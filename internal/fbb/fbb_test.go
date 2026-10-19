// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package fbb

import (
	"bytes"
	"strings"
	"testing"
)

func TestChecksumAndSID(t *testing.T) {
	sid, err := SID("0.4.0")
	if err != nil || sid != "[CROSSBEAM-0.4.0-FHM$]" {
		t.Fatalf("sid %q %v", sid, err)
	}
	if _, err := SID("bad]version"); err == nil {
		t.Fatal("bad version accepted")
	}
	line := "FB B K5LEB WW ALL AB 3"
	var sum byte
	for i := 0; i < len(line); i++ {
		sum += line[i]
	}
	sum += '\r'
	if sum != 0x27 {
		t.Fatalf("frozen sum %02X", sum)
	}
	if Checksum([]string{line}) != sum {
		t.Fatal("checksum drifted")
	}
	raw, err := EncodeProposals([]Proposal{{
		Type: "b", From: "k5leb", At: "ww", To: "all", BID: "ab", Size: 3,
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "FB B K5LEB WW ALL AB 3\rF> 27\r"
	if string(raw) != want {
		t.Fatalf("block %q", raw)
	}
}

func TestProposalParsing(t *testing.T) {
	got, err := ParseProposal("FB b k5leb k5leb.#dfw.tx all note-1 12")
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "B" || got.From != "K5LEB" || got.At != "K5LEB.#DFW.TX" || got.To != "ALL" || got.BID != "NOTE-1" || got.Size != 12 {
		t.Fatalf("%+v", got)
	}
	for _, line := range []string{
		"FB B K5LEB WW ALL NOTE",
		"FB X K5LEB WW ALL NOTE 12",
		"FB B NOT A CALL WW ALL NOTE 12",
		"FB B K5LEB WW ALL NOTE 1",
		"FB B K5LEB WW ALL bad id 12",
		"FB B K5LEB WW ALL NOTE 012",
	} {
		if _, err := ParseProposal(line); err == nil {
			t.Fatalf("accepted %q", line)
		}
	}
	has, sum, err := ParseForwardEnd("F>")
	if err != nil || has {
		t.Fatalf("bare end %v %v", has, err)
	}
	has, sum, err = ParseForwardEnd("f> 27")
	if err != nil || !has || sum != 0x27 {
		t.Fatalf("end %v %02X %v", has, sum, err)
	}
	if _, _, err := ParseForwardEnd("F> ZZ"); err == nil {
		t.Fatal("bad hex accepted")
	}
	marks, err := ParseFS("fs - + H", 3)
	if err != nil || string(marks) != "-+H" {
		t.Fatalf("marks %q %v", marks, err)
	}
	if _, err := ParseFS("FS +", 2); err == nil {
		t.Fatal("short FS accepted")
	}
}

func TestMessageFraming(t *testing.T) {
	raw, err := EncodeMessage("Hello net", "Meet at seven.\n\nBring the list.")
	if err != nil {
		t.Fatal(err)
	}
	if raw[len(raw)-1] != CtrlZ {
		t.Fatal("missing Ctrl-Z")
	}
	if !bytes.Contains(raw, []byte("Hello net\rMeet at seven.\r\rBring the list.")) {
		t.Fatalf("wire %q", raw)
	}
	title, body, err := DecodeMessage(raw)
	if err != nil || title != "Hello net" || body != "Meet at seven.\n\nBring the list." {
		t.Fatalf("%q %q %v", title, body, err)
	}
	hand := []byte("Hello net\rMeet at seven.\r\rBring the list.\x1a")
	title, body, err = DecodeMessage(hand)
	if err != nil || title != "Hello net" || body != "Meet at seven.\n\nBring the list." {
		t.Fatalf("hand %q %q %v", title, body, err)
	}
	if _, err := EncodeMessage(strings.Repeat("T", MaxSubjectRunes), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeMessage(strings.Repeat("T", MaxSubjectRunes+1), ""); err == nil {
		t.Fatal("long title accepted")
	}
	if _, _, err := DecodeMessage([]byte("No ctrl z")); err == nil {
		t.Fatal("unterminated message accepted")
	}
}
