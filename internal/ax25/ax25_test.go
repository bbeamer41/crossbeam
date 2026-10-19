// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ax25

import (
	"strings"
	"testing"
)

func TestCallsign(t *testing.T) {
	call, err := Parse(" k5leb-0 ")
	if err != nil || call.String() != "K5LEB" || call.SSID != 0 {
		t.Fatalf("parse %v %v", call, err)
	}
	call, err = Parse("W1AW-15")
	if err != nil || call.String() != "W1AW-15" {
		t.Fatalf("ssid %v %v", call, err)
	}
	for _, bad := range []string{"", "K5LEB-16", "K5LEB-01", "WAYTOOLONG", "K5 LEB", "K5LEB-"} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("%q was accepted", bad)
		}
	}
}

func TestFCS(t *testing.T) {
	if got := fcs([]byte("123456789")); got != 0x906E {
		t.Fatalf("crc %04x", got)
	}
	body := []byte{0x96, 0x6a, 0x98, 0x8a, 0x84, 0x40, 0x61}
	packet := appendFCS(body)
	if !checkFCS(packet) {
		t.Fatal("residue")
	}
	packet[0] ^= 0x01
	if checkFCS(packet) {
		t.Fatal("flipped bit passed")
	}
}

func TestFrameRoundTrip(t *testing.T) {
	f := Frame{
		Dest:    Address{Call: "W1AW"},
		Src:     Address{Call: "K5LEB", SSID: 1},
		Command: true,
		Control: SABM | 0x10,
	}
	raw := f.Encode()
	if raw[0] != 'W'<<1 || raw[6] != 0xE0 {
		t.Fatalf("dest header %x", raw[:7])
	}
	if raw[7] != 'K'<<1 || raw[13] != 0x63 {
		t.Fatalf("src header %x", raw[7:14])
	}
	got, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Dest.Equal(f.Dest) || !got.Src.Equal(f.Src) || !got.Command || !IsSABM(got.Control) || !PF(got.Control) {
		t.Fatalf("decoded %+v", got)
	}
	text := Frame{
		Dest:    f.Dest,
		Src:     f.Src,
		Command: true,
		Control: IControl(1, 2, false),
		PID:     PIDText,
		Info:    []byte("Hello\r"),
	}
	got, err = Decode(text.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if NS(got.Control) != 1 || NR(got.Control) != 2 || got.PID != PIDText || string(got.Info) != "Hello\r" {
		t.Fatalf("info %+v %q", got, got.Info)
	}
	raw[len(raw)-1] ^= 0xFF
	if _, err := Decode(raw); err == nil || !strings.Contains(err.Error(), "frame check") {
		t.Fatalf("bad fcs %v", err)
	}
}
