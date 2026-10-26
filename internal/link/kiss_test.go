// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package link

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
	"github.com/bbeamer41/crossbeam/internal/kiss"
)

func TestRadioPromptCrossesKiss(t *testing.T) {
	addr := startCable(t)
	a := startKiss(t, "K5LEB", addr, true)
	startKiss(t, "W1AW-1", addr, true)
	deadline := time.Now().Add(6 * time.Second)
	var text string
	var err error
	for {
		text, err = sessionTextWithin(a, "W1AW-1", "?\rBYE\r", 3*time.Second)
		if err == nil && strings.Contains(text, "Crossbeam W1AW-1\r") && strings.Contains(text, "Bye\r") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("radio %q %v", text, err)
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func TestKissUIAndDigipeatDoNotOpenASession(t *testing.T) {
	ln := listenTCP(t)
	got := acceptOne(t, ln)
	node := startKiss(t, "K5LEB", ln.Addr().String(), false)
	conn := waitConn(t, got, 2*time.Second)
	defer conn.Close()
	dec := &kiss.Decoder{}
	frames := readKissUntil(t, conn, dec, 2*time.Second, func(all []kiss.Frame) bool {
		return countCmd(all, kiss.CmdFullDuplex) > 0 && countCmd(all, kiss.CmdTXDelay) > 0
	})
	if frames[len(frames)-1].Command == kiss.CmdFullDuplex && len(frames[len(frames)-1].Data) == 1 && frames[len(frames)-1].Data[0] != kiss.HalfDuplex {
		t.Fatalf("duplex %+v", frames[len(frames)-1])
	}
	digi := ax25.Frame{
		Dest: node.Local(), Src: mustCall(t, "N0CALL"), Command: true,
		Digis: []ax25.Address{mustCall(t, "WIDE1")}, Control: ax25.SABM | 0x10,
	}
	if _, err := conn.Write(kiss.Encode(kiss.CmdData, digi.EncodeBody())); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < ax25.MaxSessions+8; i++ {
		call := mustCall(t, "N"+pad5(i))
		ui := ax25.Frame{Dest: node.Local(), Src: call, Command: true, Control: ax25.UI, PID: ax25.PIDText, Info: []byte("beacon")}
		if _, err := conn.Write(kiss.Encode(kiss.CmdData, ui.EncodeBody())); err != nil {
			t.Fatal(err)
		}
	}
	sabm := ax25.Frame{Dest: node.Local(), Src: mustCall(t, "N0CALL"), Command: true, Control: ax25.SABM | 0x10}
	otherPort := kiss.Encode(kiss.CmdData, sabm.EncodeBody())
	otherPort[1] = 0x10
	if _, err := conn.Write(otherPort); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(kiss.Encode(kiss.CmdData, sabm.EncodeBody())); err != nil {
		t.Fatal(err)
	}
	uas := 0
	_ = readKissUntil(t, conn, dec, 2*time.Second, func(all []kiss.Frame) bool {
		uas = countUA(all)
		return uas > 0
	})
	if uas != 1 {
		t.Fatalf("ua count %d", uas)
	}
}

func TestListedCallStaysOnUDP(t *testing.T) {
	ln := listenTCP(t)
	got := acceptOne(t, ln)
	node, err := Listen(Config{
		Bind: "127.0.0.1:0", Local: mustCall(t, "K5LEB"), Kiss: ln.Addr().String(),
		KissRetry: 30 * time.Millisecond, T1: 150 * time.Millisecond, N2: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { node.Close() })
	peer := listenUDP(t, "127.0.0.1")
	mustPeers(t, node, peerNode(t, peer, "W1AW-1"))
	done := make(chan error, 1)
	go func() {
		done <- answerSABM(peer, node.Local(), mustCall(t, "W1AW-1"), []byte("W1AW-1>\r"))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	stream, err := node.Dial(ctx, "W1AW-1")
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	conn := waitConn(t, got, 2*time.Second)
	defer conn.Close()
	dec := &kiss.Decoder{}
	frames := readKissUntil(t, conn, dec, 300*time.Millisecond, func(all []kiss.Frame) bool {
		return countCmd(all, kiss.CmdData) > 0
	})
	if countCmd(frames, kiss.CmdData) != 0 {
		t.Fatal("listed call went to the radio")
	}
}

func TestListedCallerOnTheRadioIsIgnored(t *testing.T) {
	ln := listenTCP(t)
	got := acceptOne(t, ln)
	node, err := Listen(Config{
		Bind: "127.0.0.1:0", Local: mustCall(t, "K5LEB"), Kiss: ln.Addr().String(),
		KissRetry: 30 * time.Millisecond, T1: 150 * time.Millisecond, N2: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { node.Close() })
	peer := listenUDP(t, "127.0.0.1")
	mustPeers(t, node, peerNode(t, peer, "W1AW-1"))
	conn := waitConn(t, got, 2*time.Second)
	defer conn.Close()
	dec := &kiss.Decoder{}
	_ = readKissUntil(t, conn, dec, 2*time.Second, func(all []kiss.Frame) bool {
		return countCmd(all, kiss.CmdTXDelay) > 0
	})
	drainUDP(peer)
	sabm := ax25.Frame{Dest: node.Local(), Src: mustCall(t, "W1AW-1"), Command: true, Control: ax25.SABM | 0x10}
	if _, err := conn.Write(kiss.Encode(kiss.CmdData, sabm.EncodeBody())); err != nil {
		t.Fatal(err)
	}
	frames := readKissUntil(t, conn, dec, 400*time.Millisecond, func(all []kiss.Frame) bool {
		return countUA(all) > 0
	})
	if countUA(frames) != 0 {
		t.Fatal("listed call was answered on the radio")
	}
	if n := udpWaiting(peer); n != 0 {
		t.Fatalf("listed call produced %d udp bytes", n)
	}
}

func TestKissOnlyDoesNotMoveAListedCall(t *testing.T) {
	ln := listenTCP(t)
	got := acceptOne(t, ln)
	node, err := Listen(Config{
		Local: mustCall(t, "K5LEB"), Kiss: ln.Addr().String(),
		KissRetry: 30 * time.Millisecond, T1: 150 * time.Millisecond, N2: 4,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { node.Close() })
	peer := listenUDP(t, "127.0.0.1")
	mustPeers(t, node, peerNode(t, peer, "W1AW-1"))
	conn := waitConn(t, got, 2*time.Second)
	defer conn.Close()
	dec := &kiss.Decoder{}
	_ = readKissUntil(t, conn, dec, 2*time.Second, func(all []kiss.Frame) bool {
		return countCmd(all, kiss.CmdTXDelay) > 0
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_, err = node.Dial(ctx, "W1AW-1")
	cancel()
	if err == nil {
		t.Fatal("listed call connected without udp")
	}
	frames := readKissUntil(t, conn, dec, 300*time.Millisecond, func(all []kiss.Frame) bool {
		return countCmd(all, kiss.CmdData) > 0
	})
	if countCmd(frames, kiss.CmdData) != 0 {
		t.Fatal("listed call went to the radio")
	}
}

func TestKissDialsAgain(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	node := startKiss(t, "K5LEB", addr, false)
	ln, err = net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got := acceptOne(t, ln)
	select {
	case conn := <-got:
		defer conn.Close()
		dec := &kiss.Decoder{}
		frames := readKissUntil(t, conn, dec, 2*time.Second, func(all []kiss.Frame) bool {
			return countCmd(all, kiss.CmdTXDelay) > 0 && countCmd(all, kiss.CmdFullDuplex) > 0
		})
		if countCmd(frames, kiss.CmdTXDelay) == 0 || countCmd(frames, kiss.CmdFullDuplex) == 0 {
			t.Fatalf("params %+v", frames)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tnc was not dialed again")
	}
	_ = node
}

func startKiss(t *testing.T, call, tnc string, terminal bool) *Node {
	t.Helper()
	cfg := Config{
		Local: mustCall(t, call), Kiss: tnc, KissRetry: 30 * time.Millisecond,
		T1: 150 * time.Millisecond, N2: 6, Paclen: 128, Window: 4,
	}
	if terminal {
		cfg.TerminalPath = filepath.Join(t.TempDir(), "terminal.sock")
		cfg.TerminalMode = 0o600
	}
	node, err := Listen(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { node.Close() })
	return node
}

func startCable(t *testing.T) string {
	t.Helper()
	ln := listenTCP(t)
	go func() {
		a, err := ln.Accept()
		if err != nil {
			return
		}
		b, err := ln.Accept()
		if err != nil {
			_ = a.Close()
			return
		}
		go func() {
			_, _ = io.Copy(b, a)
			_ = a.Close()
			_ = b.Close()
		}()
		_, _ = io.Copy(a, b)
		_ = a.Close()
		_ = b.Close()
	}()
	return ln.Addr().String()
}

func listenTCP(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln
}

func acceptOne(t *testing.T, ln net.Listener) <-chan net.Conn {
	t.Helper()
	ch := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		ch <- conn
	}()
	return ch
}

func waitConn(t *testing.T, ch <-chan net.Conn, d time.Duration) net.Conn {
	t.Helper()
	select {
	case conn := <-ch:
		if conn == nil {
			t.Fatal("tnc did not accept")
		}
		return conn
	case <-time.After(d):
		t.Fatal("tnc did not accept")
	}
	return nil
}

func drainUDP(pc *net.UDPConn) {
	buf := make([]byte, 2048)
	for {
		_ = pc.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		_, _, err := pc.ReadFromUDP(buf)
		if err != nil {
			return
		}
	}
}

func udpWaiting(pc *net.UDPConn) int {
	buf := make([]byte, 2048)
	_ = pc.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	n, _, err := pc.ReadFromUDP(buf)
	if err != nil {
		return 0
	}
	return n
}

func readKissUntil(t *testing.T, conn net.Conn, dec *kiss.Decoder, d time.Duration, done func([]kiss.Frame) bool) []kiss.Frame {
	t.Helper()
	deadline := time.Now().Add(d)
	buf := make([]byte, 2048)
	var all []kiss.Frame
	for {
		if done(all) || time.Now().After(deadline) {
			return all
		}
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		n, err := conn.Read(buf)
		if n > 0 {
			all = append(all, dec.Push(buf[:n])...)
		}
		if err != nil && done(all) {
			return all
		}
	}
}

func countCmd(frames []kiss.Frame, cmd byte) int {
	n := 0
	for _, frame := range frames {
		if frame.Command == cmd && frame.Port == 0 {
			n++
		}
	}
	return n
}

func countUA(frames []kiss.Frame) int {
	n := 0
	for _, frame := range frames {
		if frame.Command != kiss.CmdData {
			continue
		}
		ax, err := ax25.DecodeBody(frame.Data)
		if err == nil && ax25.IsUA(ax.Control) {
			n++
		}
	}
	return n
}

func pad5(n int) string {
	text := ""
	for i := 0; i < 5; i++ {
		text = string(rune('0'+n%10)) + text
		n /= 10
	}
	return text
}

// peerNode adapts a UDP socket into the shape mustPeers expects.
func peerNode(t *testing.T, pc *net.UDPConn, call string) *Node {
	t.Helper()
	return &Node{local: mustCall(t, call), pc: pc}
}
