// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package kiss

import "testing"

func TestDataRoundTripEscapesMarkers(t *testing.T) {
	raw := []byte{'h', 'i', FEND, FESC, 'x'}
	frames := (&Decoder{}).Push(Encode(CmdData, raw))
	if len(frames) != 1 || frames[0].Port != 0 || frames[0].Command != CmdData {
		t.Fatalf("frame %+v", frames)
	}
	if string(frames[0].Data) != string(raw) {
		t.Fatalf("data %x", frames[0].Data)
	}
	wire := Encode(CmdData, []byte{FEND})
	if wire[0] != FEND || wire[1] != CmdData || wire[2] != FESC || wire[3] != TFEND || wire[4] != FEND {
		t.Fatalf("escape %x", wire)
	}
}

func TestDecoderSplitsAndSkipsSync(t *testing.T) {
	dec := &Decoder{}
	if len(dec.Push([]byte{FEND, FEND, CmdData})) != 0 {
		t.Fatal("partial")
	}
	frames := dec.Push([]byte{'A', FEND})
	if len(frames) != 1 || string(frames[0].Data) != "A" {
		t.Fatalf("%+v", frames)
	}
}

func TestOtherPortAndBadEscapeAreSeparate(t *testing.T) {
	dec := &Decoder{}
	wire := []byte{FEND, 0x10, 'Z', FEND, FEND, CmdData, FESC, 0x00, FEND, FEND, CmdData, 'Q', FEND}
	frames := dec.Push(wire)
	if len(frames) != 2 {
		t.Fatalf("kept %+v", frames)
	}
	if frames[0].Port != 1 || frames[0].Command != CmdData || string(frames[0].Data) != "Z" {
		t.Fatalf("port %+v", frames[0])
	}
	if frames[1].Command != CmdData || string(frames[1].Data) != "Q" {
		t.Fatalf("after bad escape %+v", frames[1])
	}
}

func TestParametersAreTheHalfDuplexSet(t *testing.T) {
	frames := (&Decoder{}).Push(Parameters())
	if len(frames) != 5 {
		t.Fatalf("%d frames", len(frames))
	}
	want := []struct {
		cmd byte
		val byte
	}{
		{CmdTXDelay, TXDelay1200},
		{CmdPersist, Persist1200},
		{CmdSlotTime, SlotTime1200},
		{CmdTXTail, TXTail1200},
		{CmdFullDuplex, HalfDuplex},
	}
	for i, item := range want {
		f := frames[i]
		if f.Port != 0 || f.Command != item.cmd || len(f.Data) != 1 || f.Data[0] != item.val {
			t.Fatalf("param %d %+v", i, f)
		}
		if f.Command == CmdSetHardware || f.Command == 0x0F {
			t.Fatal("hardware command")
		}
	}
}

func TestOverlongFrameIsDropped(t *testing.T) {
	dec := &Decoder{}
	buf := []byte{FEND, CmdData}
	buf = append(buf, make([]byte, maxFrame+1)...)
	buf = append(buf, FEND, CmdData, 'o', 'k', FEND)
	frames := dec.Push(buf)
	if len(frames) != 1 || string(frames[0].Data) != "ok" {
		t.Fatalf("%+v", frames)
	}
}

func TestEscapedOverlongFrameIsDropped(t *testing.T) {
	dec := &Decoder{}
	buf := []byte{FEND, CmdData}
	for i := 0; i < maxFrame; i++ {
		buf = append(buf, FESC, TFEND)
	}
	buf = append(buf, FEND, CmdData, 'o', 'k', FEND)
	frames := dec.Push(buf)
	if len(frames) != 1 || string(frames[0].Data) != "ok" {
		t.Fatalf("%+v", frames)
	}
}
