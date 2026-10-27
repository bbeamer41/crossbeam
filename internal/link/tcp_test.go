// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package link

import (
	"bufio"
	"context"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
)

func TestTelnetPrompt(t *testing.T) {
	got := make(chan string, 1)
	peer := listenTelnet(t, func(_ ax25.Address, rw io.ReadWriteCloser) {
		defer rw.Close()
		if _, err := io.WriteString(rw, "Crossbeam W1AW-1\r"); err != nil {
			return
		}
		buf := make([]byte, 16)
		n, _ := rw.Read(buf)
		got <- string(buf[:n])
	})
	master := listenTerm(t, Config{
		TelnetPeers: "W1AW-1@" + peer.Telnet(),
		T1:          100 * time.Millisecond,
		N2:          2,
	})
	conn := dialTerm(t, master)
	defer conn.Close()
	if _, err := io.WriteString(conn, "CONNECT W1AW-1\n"); err != nil {
		t.Fatal(err)
	}
	rd := bufio.NewReader(conn)
	line, err := rd.ReadString('\n')
	if err != nil || line != "OK\n" {
		t.Fatalf("reply %q %v", line, err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, err := rd.Read(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "Crossbeam W1AW-1") {
		t.Fatalf("banner %q %v", buf[:n], err)
	}
	if strings.Contains(string(buf[:n]), "\xff") {
		t.Fatalf("telnet command leaked %q", buf[:n])
	}
	if _, err := io.WriteString(conn, "BYE\r"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-got:
		if line != "BYE\r" {
			t.Fatalf("peer saw %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("peer did not read")
	}
}

func TestFBBListenerIsRaw(t *testing.T) {
	peer, err := Listen(Config{
		FBB:   "127.0.0.1:0",
		Local: mustCall(t, "W1AW-1"),
		Handler: func(_ ax25.Address, rw io.ReadWriteCloser) {
			defer rw.Close()
			_, _ = io.WriteString(rw, "SID\r")
			_, _ = io.Copy(io.Discard, rw)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	conn, err := net.Dial("tcp", peer.FBB())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 16)
	n, err := conn.Read(buf)
	if err != nil || string(buf[:n]) != "SID\r" {
		t.Fatalf("banner %q %v", buf[:n], err)
	}
}

func TestForwardUsesFBBPeer(t *testing.T) {
	peer, err := Listen(Config{
		FBB:   "127.0.0.1:0",
		Local: mustCall(t, "W1AW-1"),
		Handler: func(_ ax25.Address, rw io.ReadWriteCloser) {
			defer rw.Close()
			_, _ = io.WriteString(rw, "BANNER\r")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	master := listenTerm(t, Config{
		FBBPeers: "W1AW-1@" + peer.FBB(),
		T1:       100 * time.Millisecond,
		N2:       2,
		Forward: func(_ context.Context, rw io.ReadWriteCloser) (string, error) {
			buf, err := io.ReadAll(rw)
			if err != nil {
				return "", err
			}
			if !strings.Contains(string(buf), "BANNER") {
				return "", io.ErrUnexpectedEOF
			}
			return "sent 1 received 0", nil
		},
	})
	conn := dialTerm(t, master)
	defer conn.Close()
	if _, err := io.WriteString(conn, "FORWARD W1AW-1\n"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "OK sent 1 received 0\n" {
		t.Fatalf("reply %q %v", line, err)
	}
}

func TestListedCallDoesNotUseTelnet(t *testing.T) {
	hit := make(chan struct{}, 1)
	peer := listenTelnet(t, func(_ ax25.Address, rw io.ReadWriteCloser) {
		hit <- struct{}{}
		rw.Close()
	})
	sink, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	master := listenTerm(t, Config{
		Bind:        "127.0.0.1:0",
		Peers:       "W1AW-1@" + sink.LocalAddr().String(),
		TelnetPeers: "W1AW-1@" + peer.Telnet(),
		T1:          40 * time.Millisecond,
		N2:          1,
	})
	conn := dialTerm(t, master)
	defer conn.Close()
	if _, err := io.WriteString(conn, "CONNECT W1AW-1\n"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "ERR ") {
		t.Fatalf("reply %q %v", line, err)
	}
	select {
	case <-hit:
		t.Fatal("telnet was dialed for a listed station")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestTelnetFailureDoesNotUseTheRadio(t *testing.T) {
	master := listenTerm(t, Config{
		Kiss:        "127.0.0.1:9",
		TelnetPeers: "W1AW-1@127.0.0.1:1",
		T1:          30 * time.Second,
		N2:          2,
	})
	conn := dialTerm(t, master)
	defer conn.Close()
	if _, err := io.WriteString(conn, "CONNECT W1AW-1\n"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.Contains(line, "connection refused") {
		t.Fatalf("reply %q %v", line, err)
	}
}

func TestListedForwardDoesNotUseFBB(t *testing.T) {
	hit := make(chan struct{}, 1)
	peer, err := Listen(Config{
		FBB:   "127.0.0.1:0",
		Local: mustCall(t, "W1AW-1"),
		Handler: func(_ ax25.Address, rw io.ReadWriteCloser) {
			hit <- struct{}{}
			rw.Close()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	sink, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	called := false
	master := listenTerm(t, Config{
		Bind:     "127.0.0.1:0",
		Peers:    "W1AW-1@" + sink.LocalAddr().String(),
		FBBPeers: "W1AW-1@" + peer.FBB(),
		T1:       40 * time.Millisecond,
		N2:       1,
		Forward: func(context.Context, io.ReadWriteCloser) (string, error) {
			called = true
			return "sent 0 received 0", nil
		},
	})
	conn := dialTerm(t, master)
	defer conn.Close()
	if _, err := io.WriteString(conn, "FORWARD W1AW-1\n"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "ERR ") || called {
		t.Fatalf("reply %q called %v %v", line, called, err)
	}
	select {
	case <-hit:
		t.Fatal("FBB TCP was dialed for a listed station")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestNinthTelnetSessionIsBusy(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	entered := make(chan struct{}, maxTCPSessions)
	peer := listenTelnet(t, func(_ ax25.Address, rw io.ReadWriteCloser) {
		defer rw.Close()
		entered <- struct{}{}
		<-release
	})
	var conns []net.Conn
	defer func() {
		for _, conn := range conns {
			conn.Close()
		}
	}()
	for i := 0; i < maxTCPSessions; i++ {
		conn, err := net.Dial("tcp", peer.Telnet())
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
	}
	for i := 0; i < maxTCPSessions; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("session did not start")
		}
	}
	extra, err := net.Dial("tcp", peer.Telnet())
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	_ = extra.SetDeadline(time.Now().Add(2 * time.Second))
	buf, err := io.ReadAll(extra)
	if err != nil || string(buf) != "The station is busy.\r" {
		t.Fatalf("busy %q %v", buf, err)
	}
}

func TestTCPForwardPolicy(t *testing.T) {
	type info interface {
		ForwardAllowed() bool
		PeerLabel() string
	}
	got := make(chan info, 1)
	handler := func(_ ax25.Address, rw io.ReadWriteCloser) {
		gate, _ := rw.(info)
		got <- gate
		_ = rw.Close()
	}
	guest := listenTelnet(t, handler)
	conn, err := net.Dial("tcp", guest.Telnet())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	select {
	case gate := <-got:
		if gate == nil || gate.ForwardAllowed() || !strings.Contains(gate.PeerLabel(), "127.0.0.1") {
			t.Fatalf("guest telnet %+v", gate)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("guest telnet did not start")
	}
	listed, err := Listen(Config{
		Telnet:      "127.0.0.1:0",
		TelnetPeers: "W1AW-1@127.0.0.1:8770",
		Local:       mustCall(t, "K5LEB"),
		Handler:     handler,
		T1:          100 * time.Millisecond,
		N2:          2,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listed.Close()
	conn2, err := net.Dial("tcp", listed.Telnet())
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	select {
	case gate := <-got:
		if gate == nil || !gate.ForwardAllowed() || !strings.Contains(gate.PeerLabel(), "W1AW-1") {
			t.Fatalf("listed telnet %+v", gate)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("listed telnet did not start")
	}
}

func listenTelnet(t *testing.T, handler func(ax25.Address, io.ReadWriteCloser)) *Node {
	t.Helper()
	node, err := Listen(Config{
		Telnet:  "127.0.0.1:0",
		Local:   mustCall(t, "W1AW-1"),
		Handler: handler,
		T1:      100 * time.Millisecond,
		N2:      2,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { node.Close() })
	return node
}

func listenTerm(t *testing.T, cfg Config) *Node {
	t.Helper()
	if cfg.Local.Call == "" {
		cfg.Local = mustCall(t, "K5LEB")
	}
	if cfg.TerminalPath == "" {
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

func dialTerm(t *testing.T, node *Node) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", node.Terminal())
	if err != nil {
		t.Fatal(err)
	}
	return conn
}
