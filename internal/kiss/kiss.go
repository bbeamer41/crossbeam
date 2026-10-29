// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package kiss frames AX.25 for a TNC. The framing is the KISS protocol
// published by K3MC and KA9Q: a frame starts and ends with FEND, and FEND
// or FESC inside the frame is escaped. The first byte after FEND is the
// port and the command. Port 0 is the only port this station uses.
// A data frame is an AX.25 frame without its frame check sequence.
// The TNC adds that check sequence. This package does not modulate audio.
package kiss

// Framing bytes.
const (
	FEND  byte = 0xC0
	FESC  byte = 0xDB
	TFEND byte = 0xDC
	TFESC byte = 0xDD
)

// Commands occupy the low nibble. The high nibble is the port.
const (
	CmdData        byte = 0
	CmdTXDelay     byte = 1
	CmdPersist     byte = 2
	CmdSlotTime    byte = 3
	CmdTXTail      byte = 4
	CmdFullDuplex  byte = 5
	CmdSetHardware byte = 6
)

// Timing values are the published KISS defaults. TX delay and slot time
// count 10 ms units. They suit a half-duplex 1200 baud channel. Full
// duplex is off. There is no 9600 baud set.
const (
	TXDelay1200  byte = 50
	Persist1200  byte = 63
	SlotTime1200 byte = 10
	TXTail1200   byte = 0
	HalfDuplex   byte = 0
)

const maxFrame = 1024

// Frame is one decoded KISS frame. Data does not include the command byte.
type Frame struct {
	Port    byte
	Command byte
	Data    []byte
}

// Encode builds one port-0 frame. cmd is the low nibble.
func Encode(cmd byte, data []byte) []byte {
	out := make([]byte, 0, len(data)+3)
	out = append(out, FEND, cmd&0x0F)
	for _, b := range data {
		switch b {
		case FEND:
			out = append(out, FESC, TFEND)
		case FESC:
			out = append(out, FESC, TFESC)
		default:
			out = append(out, b)
		}
	}
	return append(out, FEND)
}

// Parameters is the half-duplex 1200 baud set, sent once each time the
// TNC connection comes up. SetHardware is not included.
func Parameters() []byte {
	var out []byte
	out = append(out, Encode(CmdTXDelay, []byte{TXDelay1200})...)
	out = append(out, Encode(CmdPersist, []byte{Persist1200})...)
	out = append(out, Encode(CmdSlotTime, []byte{SlotTime1200})...)
	out = append(out, Encode(CmdTXTail, []byte{TXTail1200})...)
	out = append(out, Encode(CmdFullDuplex, []byte{HalfDuplex})...)
	return out
}

// Decoder reads a byte stream and returns whole frames.
type Decoder struct {
	started bool
	esc     bool
	drop    bool
	buf     []byte
}

// Push consumes the next bytes. A frame that is too long, or that has a
// broken escape, is discarded through the next FEND.
func (d *Decoder) Push(p []byte) []Frame {
	var out []Frame
	for _, b := range p {
		switch {
		case b == FEND:
			if d.started && !d.drop && len(d.buf) > 0 {
				out = append(out, Frame{
					Port:    d.buf[0] >> 4,
					Command: d.buf[0] & 0x0F,
					Data:    append([]byte(nil), d.buf[1:]...),
				})
			}
			d.buf = d.buf[:0]
			d.started = true
			d.esc = false
			d.drop = false
		case !d.started || d.drop:
		case d.esc:
			d.esc = false
			var decoded byte
			switch b {
			case TFEND:
				decoded = FEND
			case TFESC:
				decoded = FESC
			default:
				d.drop = true
				d.buf = d.buf[:0]
				continue
			}
			d.buf = append(d.buf, decoded)
			if len(d.buf) > maxFrame {
				d.drop = true
				d.buf = d.buf[:0]
			}
		case b == FESC:
			d.esc = true
		default:
			d.buf = append(d.buf, b)
			if len(d.buf) > maxFrame {
				d.drop = true
				d.buf = d.buf[:0]
			}
		}
	}
	return out
}
