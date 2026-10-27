// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package link

import (
	"context"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
	"github.com/bbeamer41/crossbeam/internal/settings"
	"github.com/bbeamer41/crossbeam/internal/telnet"
)

const (
	maxTCPSessions = 8
	tcpIdle        = 15 * time.Minute
	// tcpBusyWait bounds the rejection line. A peer that does not read
	// must not stall the accept loop.
	tcpBusyWait = 2 * time.Second
)

// Telnet returns the bound telnet address. It is empty when telnet is off.
func (n *Node) Telnet() string {
	if n == nil || n.telnetLn == nil {
		return ""
	}
	return n.telnetLn.Addr().String()
}

// FBB returns the bound FBB TCP address. It is empty when that port is off.
func (n *Node) FBB() string {
	if n == nil || n.fbbLn == nil {
		return ""
	}
	return n.fbbLn.Addr().String()
}

func (n *Node) bindTCP(telnetListen, fbbListen string) error {
	if telnetListen != "" {
		ln, err := net.Listen("tcp", telnetListen)
		if err != nil {
			return err
		}
		n.telnetLn = ln
		go n.acceptTCP(ln, "telnet")
	}
	if fbbListen != "" {
		ln, err := net.Listen("tcp", fbbListen)
		if err != nil {
			return err
		}
		n.fbbLn = ln
		go n.acceptTCP(ln, "fbb")
	}
	return nil
}

func (n *Node) acceptTCP(ln net.Listener, kind string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		if !n.takeTCP(kind, conn) {
			if n.alive() {
				_ = conn.SetWriteDeadline(time.Now().Add(tcpBusyWait))
				_, _ = conn.Write([]byte("The station is busy.\r"))
			}
			_ = conn.Close()
			continue
		}
		go n.serveTCP(conn, kind)
	}
}

func (n *Node) takeTCP(kind string, conn net.Conn) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.alive() {
		return false
	}
	count := &n.fbbN
	if kind == "telnet" {
		count = &n.telnetN
	}
	if *count >= maxTCPSessions {
		return false
	}
	*count++
	n.tcpLive[conn] = struct{}{}
	return true
}

func (n *Node) releaseTCP(kind string, conn net.Conn) {
	n.mu.Lock()
	if kind == "telnet" {
		if n.telnetN > 0 {
			n.telnetN--
		}
	} else if n.fbbN > 0 {
		n.fbbN--
	}
	delete(n.tcpLive, conn)
	n.mu.Unlock()
	_ = conn.Close()
}

func (n *Node) serveTCP(conn net.Conn, kind string) {
	defer n.releaseTCP(kind, conn)
	var rw io.ReadWriteCloser = deadlineConn{Conn: conn, idle: tcpIdle}
	if kind == "telnet" {
		rw = telnet.Wrap(rw)
	}
	allow, label := n.tcpPolicy(kind, conn)
	n.user(ax25.Address{}, &sessionGate{ReadWriteCloser: rw, allow: allow, label: label})
}

// tcpPolicy allows a bulletin forward only when the remote IP is one of
// the listed peers for this port. The prompt stays available either way.
// The incoming port is ephemeral, so the match is the address only.
func (n *Node) tcpPolicy(kind string, conn net.Conn) (bool, string) {
	remote := ""
	if conn != nil && conn.RemoteAddr() != nil {
		remote = conn.RemoteAddr().String()
	}
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip := net.ParseIP(host)
	n.mu.Lock()
	defer n.mu.Unlock()
	list := n.fbbTo
	if kind == "telnet" {
		list = n.telnetTo
	}
	var calls []string
	for call, addr := range list {
		peerHost, _, splitErr := net.SplitHostPort(addr)
		if splitErr != nil {
			continue
		}
		peerIP := net.ParseIP(peerHost)
		if peerHost == host || (ip != nil && peerIP != nil && ip.Equal(peerIP)) {
			calls = append(calls, call)
		}
	}
	if len(calls) == 0 {
		if remote == "" {
			return false, kind + " unknown"
		}
		return false, kind + " " + remote
	}
	sort.Strings(calls)
	return true, strings.Join(calls, ",") + " " + remote
}

func (n *Node) rememberPeers(key, text string, dest map[string]string) error {
	list, err := settings.ParsePeerList(key, text)
	if err != nil {
		return err
	}
	for _, peer := range list {
		if peer.Call == n.local.String() {
			return fmt.Errorf("%s cannot list this station", key)
		}
		addr, err := n.resolve(peer.Host, peer.Port)
		if err != nil {
			return err
		}
		dest[peer.Call] = addr.String()
	}
	return nil
}

func (n *Node) tcpTarget(list map[string]string, call string) (string, bool) {
	addr, err := ax25.Parse(call)
	if err != nil {
		return "", false
	}
	target, ok := list[addr.String()]
	return target, ok
}

func (n *Node) dialTelnet(ctx context.Context, target string) (io.ReadWriteCloser, error) {
	conn, err := dialTCP(ctx, target)
	if err != nil {
		return nil, err
	}
	return telnet.Wrap(deadlineConn{Conn: conn, idle: tcpIdle}), nil
}

func (n *Node) dialFBB(ctx context.Context, target string) (io.ReadWriteCloser, error) {
	conn, err := dialTCP(ctx, target)
	if err != nil {
		return nil, err
	}
	return deadlineConn{Conn: conn, idle: tcpIdle}, nil
}

func dialTCP(ctx context.Context, target string) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", target)
}

type deadlineConn struct {
	net.Conn
	idle time.Duration
}

func (d deadlineConn) Read(p []byte) (int, error) {
	if d.idle > 0 {
		_ = d.SetReadDeadline(time.Now().Add(d.idle))
	}
	return d.Conn.Read(p)
}

func (d deadlineConn) Write(p []byte) (int, error) {
	if d.idle > 0 {
		_ = d.SetWriteDeadline(time.Now().Add(d.idle))
	}
	return d.Conn.Write(p)
}
