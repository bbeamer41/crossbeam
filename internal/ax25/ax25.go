// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ax25 encodes AX.25 frames and runs a modulo-8 connection.
// The bytes are the published AX.25 link protocol. Payloads stay readable.
package ax25

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Control bytes without the poll/final bit. That bit is 0x10.
const (
	SABM  byte = 0x2F
	DISC  byte = 0x43
	UA    byte = 0x63
	DM    byte = 0x0F
	SABME byte = 0x6F
	UI    byte = 0x03
	FRMR  byte = 0x87
	RR    byte = 0x01
	RNR   byte = 0x05
	REJ   byte = 0x09
	// PIDText is "no layer 3". Text uses it.
	PIDText byte = 0xF0
	// PIDNetrom is the amateur NET/ROM network protocol, PID CF hex.
	PIDNetrom byte = 0xCF
)

// Address is one AX.25 callsign. SSID 0 is the plain callsign.
type Address struct {
	Call     string
	SSID     int
	Repeated bool
}

// Parse reads a callsign such as K5LEB or W1AW-1.
func Parse(text string) (Address, error) {
	text = strings.ToUpper(strings.TrimSpace(text))
	if text == "" {
		return Address{}, errors.New("callsign is empty")
	}
	call := text
	ssidText := "0"
	if dash := strings.LastIndex(text, "-"); dash >= 0 {
		call = text[:dash]
		ssidText = text[dash+1:]
	}
	if len(call) < 1 || len(call) > 6 {
		return Address{}, errCall
	}
	for _, c := range call {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return Address{}, errCall
		}
	}
	n, err := strconv.Atoi(ssidText)
	if err != nil || n < 0 || n > 15 || strconv.Itoa(n) != ssidText {
		return Address{}, errCall
	}
	return Address{Call: call, SSID: n}, nil
}

var errCall = errors.New("callsign must be 1 to 6 letters or digits with an optional SSID from 0 to 15")

// String renders the callsign. SSID 0 is omitted.
func (a Address) String() string {
	if a.SSID == 0 {
		return a.Call
	}
	return fmt.Sprintf("%s-%d", a.Call, a.SSID)
}

// Equal compares the callsign and SSID.
func (a Address) Equal(b Address) bool {
	return a.Call == b.Call && a.SSID == b.SSID
}

// Frame is one AX.25 frame without HDLC flags. Info is set for I and UI frames.
type Frame struct {
	Dest    Address
	Src     Address
	Digis   []Address
	Command bool
	Legacy  bool
	Control byte
	PID     byte
	Info    []byte
}

// Encode appends the frame check sequence. Flags and bit stuffing are omitted
// because the datagram already marks the ends, as in RFC 1226.
func (f Frame) Encode() []byte {
	return appendFCS(f.EncodeBody())
}

// EncodeBody returns the frame without the frame check sequence.
// A KISS TNC carries this form and adds the check sequence itself.
func (f Frame) EncodeBody() []byte {
	buf := make([]byte, 0, 14+2+len(f.Info))
	buf = appendAddress(buf, f.Dest, f.Command, false)
	last := len(f.Digis) == 0
	buf = appendAddress(buf, f.Src, !f.Command, last)
	for i, digi := range f.Digis {
		buf = appendAddress(buf, digi, digi.Repeated, i == len(f.Digis)-1)
	}
	buf = append(buf, f.Control)
	if hasPID(f.Control) {
		buf = append(buf, f.PID)
		buf = append(buf, f.Info...)
	} else if len(f.Info) > 0 {
		buf = append(buf, f.Info...)
	}
	return buf
}

// Decode checks the frame check sequence and parses one frame.
func Decode(packet []byte) (Frame, error) {
	if len(packet) < 17 {
		return Frame{}, errors.New("frame is short")
	}
	if !checkFCS(packet) {
		return Frame{}, errors.New("frame check failed")
	}
	return DecodeBody(packet[:len(packet)-2])
}

// DecodeBody parses one AX.25 frame that has no frame check sequence.
func DecodeBody(packet []byte) (Frame, error) {
	if len(packet) < 15 {
		return Frame{}, errors.New("frame is short")
	}
	body := packet
	var addrs []Address
	var bits []bool
	i := 0
	for {
		if len(addrs) == 10 || i+7 > len(body) {
			return Frame{}, errors.New("address is not valid")
		}
		addr, bit7, last, err := takeAddress(body[i : i+7])
		if err != nil {
			return Frame{}, err
		}
		addr.Repeated = bit7
		addrs = append(addrs, addr)
		bits = append(bits, bit7)
		i += 7
		if last {
			break
		}
	}
	if len(addrs) < 2 || i >= len(body) {
		return Frame{}, errors.New("frame is short")
	}
	f := Frame{Dest: addrs[0], Src: addrs[1]}
	if len(addrs) > 2 {
		f.Digis = addrs[2:]
	}
	f.Dest.Repeated = false
	f.Src.Repeated = false
	destC := bits[0]
	srcC := bits[1]
	switch {
	case destC && !srcC:
		f.Command = true
	case !destC && srcC:
		f.Command = false
	default:
		f.Legacy = true
		f.Command = true
	}
	f.Control = body[i]
	i++
	if hasPID(f.Control) {
		if i >= len(body) {
			return Frame{}, errors.New("frame is short")
		}
		f.PID = body[i]
		i++
	}
	if i < len(body) {
		f.Info = append([]byte(nil), body[i:]...)
	}
	return f, nil
}

func hasPID(control byte) bool {
	return IsI(control) || IsUI(control)
}

// IsI reports an information frame.
func IsI(control byte) bool { return control&0x01 == 0 }

// IsS reports a supervisory frame.
func IsS(control byte) bool { return control&0x03 == 0x01 }

// IsU reports an unnumbered frame.
func IsU(control byte) bool { return control&0x03 == 0x03 }

// IsUI reports an unnumbered information frame.
func IsUI(control byte) bool { return control&0xEF == UI }

// IsSABM reports a modulo-8 connection request.
func IsSABM(control byte) bool { return control&0xEF == SABM }

// IsSABME reports a modulo-128 connection request.
func IsSABME(control byte) bool { return control&0xEF == SABME }

// IsDISC reports a disconnect request.
func IsDISC(control byte) bool { return control&0xEF == DISC }

// IsUA reports an unnumbered acknowledgement.
func IsUA(control byte) bool { return control&0xEF == UA }

// IsDM reports disconnected mode.
func IsDM(control byte) bool { return control&0xEF == DM }

// IsFRMR reports a frame reject.
func IsFRMR(control byte) bool { return control&0xEF == FRMR }

// PF reports the poll or final bit.
func PF(control byte) bool { return control&0x10 != 0 }

// NS returns the send sequence number of an I frame.
func NS(control byte) int { return int((control >> 1) & 0x07) }

// NR returns the receive sequence number of an I or S frame.
func NR(control byte) int { return int((control >> 5) & 0x07) }

// IControl builds an I-frame control byte.
func IControl(ns, nr int, poll bool) byte {
	c := byte(((nr & 7) << 5) | ((ns & 7) << 1))
	if poll {
		c |= 0x10
	}
	return c
}

// SControl builds an S-frame control byte. base is RR, RNR, or REJ.
func SControl(base byte, nr int, pf bool) byte {
	c := (base & 0x0F) | byte((nr&7)<<5)
	if pf {
		c |= 0x10
	}
	return c
}

// EncodeAddress writes one 7-byte AX.25 address. last sets the extension bit,
// which NET/ROM sets on the destination callsign in a network header.
func EncodeAddress(a Address, last bool) []byte {
	return appendAddress(nil, a, false, last)
}

// DecodeAddress reads one 7-byte AX.25 address. The bool reports the extension bit.
func DecodeAddress(b []byte) (Address, bool, error) {
	if len(b) < 7 {
		return Address{}, false, errors.New("address is not valid")
	}
	addr, _, last, err := takeAddress(b[:7])
	if err != nil {
		return Address{}, false, err
	}
	return addr, last, nil
}

func appendAddress(dst []byte, a Address, bit7, last bool) []byte {
	call := a.Call
	for len(call) < 6 {
		call += " "
	}
	for i := 0; i < 6; i++ {
		dst = append(dst, call[i]<<1)
	}
	ssid := byte(a.SSID<<1) | 0x60
	if bit7 {
		ssid |= 0x80
	}
	if last {
		ssid |= 0x01
	}
	return append(dst, ssid)
}

func takeAddress(b []byte) (Address, bool, bool, error) {
	var raw [6]byte
	for i := 0; i < 6; i++ {
		if b[i]&0x01 != 0 {
			return Address{}, false, false, errors.New("address is not valid")
		}
		raw[i] = b[i] >> 1
	}
	end := 6
	for end > 0 && raw[end-1] == ' ' {
		end--
	}
	for i := 0; i < end; i++ {
		c := raw[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return Address{}, false, false, errors.New("address is not valid")
		}
	}
	if end == 0 {
		return Address{}, false, false, errors.New("address is not valid")
	}
	ssid := b[6]
	return Address{Call: string(raw[:end]), SSID: int((ssid >> 1) & 0x0F)}, ssid&0x80 != 0, ssid&0x01 != 0, nil
}

// fcs is CRC-16/X-25, the AX.25 frame check sequence.
func fcs(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return crc ^ 0xFFFF
}

func appendFCS(data []byte) []byte {
	c := fcs(data)
	return append(data, byte(c), byte(c>>8))
}

func checkFCS(packet []byte) bool {
	if len(packet) < 2 {
		return false
	}
	crc := uint16(0xFFFF)
	for _, b := range packet {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return crc == 0xF0B8
}
