// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package netrom

import (
	"errors"

	"github.com/bbeamer41/crossbeam/internal/ax25"
)

// Header sizes and opcodes are the amateur form in NET/ROM Version 1.3
// (Software 2000, 1987). The opcode is the low four bits. Bit 7 is choke
// (and, on a connect acknowledgement, refusal). Bit 6 is NAK. Bit 5 is
// more-follows. Bit 4 is reserved.
const (
	headerLen = 20
	MaxInfo   = 236

	OpConnect byte = 1
	OpAccept  byte = 2
	OpDisc    byte = 3
	OpDiscAck byte = 4
	OpInfo    byte = 5
	OpAck     byte = 6

	FlagChoke byte = 0x80
	FlagNAK   byte = 0x40
	FlagMore  byte = 0x20

	sigBroadcast byte = 0xFF
	perFrame          = 11
	recordLen         = 21
)

// Message is one inter-node frame after the AX.25 link header.
type Message struct {
	Origin ax25.Address
	Dest   ax25.Address
	TTL    byte
	Index  byte
	ID     byte
	Tx     byte
	Rx     byte
	Op     byte
	Choke  bool
	NAK    bool
	More   bool
	Window byte
	User   ax25.Address
	Node   ax25.Address
	Info   []byte
}

// Encode writes a network header and a transport header. Connect messages
// carry the window and the two callsigns. An information message carries
// Info. The destination callsign has the address-extension bit set.
func Encode(m Message) ([]byte, error) {
	if m.Op < OpConnect || m.Op > OpAck {
		return nil, errors.New("unknown opcode")
	}
	op := m.Op & 0x0F
	if m.Choke {
		op |= FlagChoke
	}
	if m.NAK {
		op |= FlagNAK
	}
	if m.More {
		op |= FlagMore
	}
	buf := make([]byte, 0, headerLen+len(m.Info)+15)
	buf = append(buf, ax25.EncodeAddress(m.Origin, false)...)
	buf = append(buf, ax25.EncodeAddress(m.Dest, true)...)
	buf = append(buf, m.TTL, m.Index, m.ID, m.Tx, m.Rx, op)
	switch m.Op {
	case OpConnect:
		buf = append(buf, m.Window)
		buf = append(buf, ax25.EncodeAddress(m.User, true)...)
		buf = append(buf, ax25.EncodeAddress(m.Node, true)...)
	case OpAccept:
		buf = append(buf, m.Window)
	case OpInfo:
		if len(m.Info) > MaxInfo {
			return nil, errors.New("information is too long")
		}
		buf = append(buf, m.Info...)
	}
	return buf, nil
}

// Decode reads one inter-node frame. A short or unknown frame is rejected.
func Decode(frame []byte) (Message, error) {
	if len(frame) < headerLen {
		return Message{}, errors.New("frame is short")
	}
	origin, _, err := ax25.DecodeAddress(frame[0:7])
	if err != nil {
		return Message{}, err
	}
	dest, _, err := ax25.DecodeAddress(frame[7:14])
	if err != nil {
		return Message{}, err
	}
	op := frame[19]
	m := Message{
		Origin: origin,
		Dest:   dest,
		TTL:    frame[14],
		Index:  frame[15],
		ID:     frame[16],
		Tx:     frame[17],
		Rx:     frame[18],
		Op:     op & 0x0F,
		Choke:  op&FlagChoke != 0,
		NAK:    op&FlagNAK != 0,
		More:   op&FlagMore != 0,
	}
	rest := frame[headerLen:]
	switch m.Op {
	case OpConnect:
		if len(rest) < 15 {
			return Message{}, errors.New("frame is short")
		}
		m.Window = rest[0]
		m.User, _, err = ax25.DecodeAddress(rest[1:8])
		if err != nil {
			return Message{}, err
		}
		m.Node, _, err = ax25.DecodeAddress(rest[8:15])
		if err != nil {
			return Message{}, err
		}
	case OpAccept:
		if len(rest) < 1 {
			return Message{}, errors.New("frame is short")
		}
		m.Window = rest[0]
	case OpInfo:
		if len(rest) > MaxInfo {
			return Message{}, errors.New("information is too long")
		}
		m.Info = append([]byte(nil), rest...)
	case OpDisc, OpDiscAck, OpAck:
	default:
		return Message{}, errors.New("unknown opcode")
	}
	return m, nil
}

// Alias is the six-byte node identifier. It is the callsign, padded with
// spaces. The SSID is not part of it.
func Alias(call string) [6]byte {
	var out [6]byte
	for i := range out {
		out[i] = ' '
	}
	if len(call) > 6 {
		call = call[:6]
	}
	copy(out[:], call)
	return out
}

func aliasText(raw [6]byte) string {
	end := 6
	for end > 0 && raw[end-1] == ' ' {
		end--
	}
	return string(raw[:end])
}

// Record is one destination in a routing broadcast.
type Record struct {
	Call     ax25.Address
	Alias    [6]byte
	Neighbor ax25.Address
	Quality  byte
}

// Broadcast is one NODES UI payload: the signature, our identifier, and
// up to 11 destinations.
type Broadcast struct {
	Alias [6]byte
	Dests []Record
}

// EncodeBroadcast splits records into UI payloads of at most 11 destinations.
func EncodeBroadcast(alias [6]byte, dests []Record) [][]byte {
	if len(dests) == 0 {
		return [][]byte{broadcastOne(alias, nil)}
	}
	var out [][]byte
	for len(dests) > 0 {
		n := len(dests)
		if n > perFrame {
			n = perFrame
		}
		out = append(out, broadcastOne(alias, dests[:n]))
		dests = dests[n:]
	}
	return out
}

func broadcastOne(alias [6]byte, dests []Record) []byte {
	buf := make([]byte, 0, 7+len(dests)*recordLen)
	buf = append(buf, sigBroadcast)
	buf = append(buf, alias[:]...)
	for _, d := range dests {
		buf = append(buf, ax25.EncodeAddress(d.Call, true)...)
		buf = append(buf, d.Alias[:]...)
		buf = append(buf, ax25.EncodeAddress(d.Neighbor, true)...)
		buf = append(buf, d.Quality)
	}
	return buf
}

// DecodeBroadcast reads one NODES UI payload. The bool is false when the
// signature is missing or the records are not whole.
func DecodeBroadcast(info []byte) (Broadcast, bool) {
	if len(info) < 7 || info[0] != sigBroadcast {
		return Broadcast{}, false
	}
	var alias [6]byte
	copy(alias[:], info[1:7])
	rest := info[7:]
	if len(rest)%recordLen != 0 || len(rest)/recordLen > perFrame {
		return Broadcast{}, false
	}
	b := Broadcast{Alias: alias}
	for len(rest) >= recordLen {
		call, _, err := ax25.DecodeAddress(rest[0:7])
		if err != nil {
			return Broadcast{}, false
		}
		var name [6]byte
		copy(name[:], rest[7:13])
		neighbor, _, err := ax25.DecodeAddress(rest[13:20])
		if err != nil {
			return Broadcast{}, false
		}
		b.Dests = append(b.Dests, Record{
			Call:     call,
			Alias:    name,
			Neighbor: neighbor,
			Quality:  rest[20],
		})
		rest = rest[recordLen:]
	}
	return b, true
}
