// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package link

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
)

func TestNetromInboxIsBounded(t *testing.T) {
	n := &Node{wake: make(chan struct{}, 1)}
	from := mustCall(t, "W1AW-1")
	for i := 0; i < maxNetromInbox+10; i++ {
		n.onNetrom(from, []byte{byte(i)})
	}
	n.netMu.Lock()
	got := len(n.inbox)
	n.netMu.Unlock()
	if got != maxNetromInbox {
		t.Fatalf("inbox %d", got)
	}
}

func TestConnectionCrossesANeighbor(t *testing.T) {
	a := startNode(t, "K5LEB", true)
	b := startNode(t, "W1AW-1", true)
	c := startNode(t, "W5BBS", true)
	mustPeers(t, a, b)
	mustPeers(t, c, b)
	if err := b.SetPeers(a.Local().String() + "@" + a.LocalAddr().String() + " " + c.Local().String() + "@" + c.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
	waitRoute(t, a, c.Local())

	fromA, err := sessionTextWithin(a, c.Local().String(), "?\rBYE\r", 8*time.Second)
	if err != nil || !strings.Contains(fromA, "Crossbeam W5BBS\r") || !strings.Contains(fromA, "W5BBS>\r") || !strings.Contains(fromA, "Commands:\r") || !strings.Contains(fromA, "Bye\r") {
		t.Fatalf("far prompt %q %v", fromA, err)
	}

	var direct string
	deadline := time.Now().Add(3 * time.Second)
	for {
		direct, err = sessionTextWithin(a, b.Local().String(), "?\rBYE\r", 4*time.Second)
		if err == nil && strings.Contains(direct, "Crossbeam W1AW-1\r") && strings.Contains(direct, "Bye\r") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("direct %q %v", direct, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestNodesBroadcastDoesNotOpenASession(t *testing.T) {
	node := startNode(t, "K5LEB", false)
	other := listenUDP(t, "127.0.0.1")
	for i := 0; i < ax25.MaxSessions+8; i++ {
		call := mustCall(t, fmt.Sprintf("N%05d", i))
		info := []byte{0xFF, 'N', '0', 'D', 'E', 'S', ' '}
		ui := ax25.Frame{
			Dest:    ax25.Address{Call: "NODES"},
			Src:     call,
			Command: true,
			Control: ax25.UI,
			PID:     ax25.PIDNetrom,
			Info:    info,
		}.Encode()
		if _, err := other.WriteToUDP(ui, node.LocalAddr()); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, ok := node.routes.Lookup(mustCall(t, "W5BBS")); ok {
		t.Fatal("unlisted broadcast installed a route")
	}
	sabm := ax25.Frame{Dest: node.Local(), Src: mustCall(t, "N0CALL"), Command: true, Control: ax25.SABM | 0x10}.Encode()
	if _, err := other.WriteToUDP(sabm, node.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	frames := readFrames(other, 1, time.Second)
	if len(frames) == 0 || !ax25.IsUA(frames[0].Control) {
		t.Fatalf("after nodes %+v", frames)
	}
}

func waitRoute(t *testing.T, n *Node, dest ax25.Address) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, quality, ok := n.routes.Lookup(dest); ok && quality > 0 {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("no route to %s", dest)
}

func sessionTextWithin(node *Node, call, send string, d time.Duration) (string, error) {
	conn, err := net.Dial("unix", node.Terminal())
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(d))
	if _, err := fmt.Fprintf(conn, "CONNECT %s\n", call); err != nil {
		return "", err
	}
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	if line != "OK\n" {
		return line, fmt.Errorf("handshake %q", line)
	}
	if _, err := conn.Write([]byte(send)); err != nil {
		return "", err
	}
	text, err := readUntil(reader, "Bye\r", d)
	return text, err
}
