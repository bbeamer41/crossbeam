// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package link

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
	"github.com/bbeamer41/crossbeam/internal/kiss"
)

const (
	kissDialTimeout = 5 * time.Second
	kissWriteWait   = 2 * time.Second
	kissRetry       = 2 * time.Second
)

// kissLoop dials the TNC, sends the 1200 baud parameters, and reads frames
// until the connection drops. It dials again until the node stops.
func (n *Node) kissLoop(addr string) {
	wait := n.kissWait
	if wait <= 0 {
		wait = kissRetry
	}
	for {
		if !n.alive() {
			return
		}
		conn, err := n.dialKiss(addr)
		if err != nil {
			if !n.waitKiss(wait) {
				return
			}
			continue
		}
		n.setKissConn(conn)
		if !n.alive() {
			n.clearKissConn(conn)
			_ = conn.Close()
			return
		}
		err = n.writeKissRaw(kiss.Parameters())
		if err == nil {
			n.readKiss(conn)
		}
		n.clearKissConn(conn)
		_ = conn.Close()
		if !n.waitKiss(wait) {
			return
		}
	}
}

func (n *Node) dialKiss(addr string) (net.Conn, error) {
	ch := make(chan struct {
		c   net.Conn
		err error
	}, 1)
	go func() {
		c, err := net.DialTimeout("tcp", addr, kissDialTimeout)
		ch <- struct {
			c   net.Conn
			err error
		}{c, err}
	}()
	select {
	case <-n.quit:
		go func() {
			r := <-ch
			if r.c != nil {
				_ = r.c.Close()
			}
		}()
		return nil, errors.New("link is closed")
	case r := <-ch:
		if !n.alive() {
			if r.c != nil {
				_ = r.c.Close()
			}
			return nil, errors.New("link is closed")
		}
		if r.err == nil && r.c != nil {
			enableKeepalive(r.c)
		}
		return r.c, r.err
	}
}

func enableKeepalive(conn net.Conn) {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tcp.SetKeepAlive(true)
	_ = tcp.SetKeepAlivePeriod(30 * time.Second)
}

func (n *Node) waitKiss(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-n.quit:
		return false
	case <-timer.C:
		return true
	}
}

func (n *Node) alive() bool {
	select {
	case <-n.quit:
		return false
	default:
		return true
	}
}

func (n *Node) setKissConn(conn net.Conn) {
	n.kissMu.Lock()
	old := n.kissConn
	n.kissConn = conn
	n.kissMu.Unlock()
	if old != nil && old != conn {
		_ = old.Close()
	}
}

func (n *Node) clearKissConn(conn net.Conn) {
	n.kissMu.Lock()
	if n.kissConn == conn {
		n.kissConn = nil
	}
	n.kissMu.Unlock()
}

func (n *Node) writeKissRaw(p []byte) error {
	n.kissMu.Lock()
	conn := n.kissConn
	n.kissMu.Unlock()
	if conn == nil {
		return errors.New("radio is not connected")
	}
	n.kissWrite.Lock()
	defer n.kissWrite.Unlock()
	n.kissMu.Lock()
	current := n.kissConn
	n.kissMu.Unlock()
	if current != conn {
		return errors.New("radio is not connected")
	}
	_ = conn.SetWriteDeadline(time.Now().Add(kissWriteWait))
	_, err := conn.Write(p)
	if err != nil {
		// A wedged TNC must not keep the reader blocked. Closing the
		// connection lets the dial loop start again.
		n.clearKissConn(conn)
		_ = conn.Close()
	}
	return err
}

func (n *Node) writeKiss(frame ax25.Frame) {
	_ = n.writeKissRaw(kiss.Encode(kiss.CmdData, frame.EncodeBody()))
}

func (n *Node) readKiss(conn net.Conn) {
	dec := &kiss.Decoder{}
	buf := make([]byte, 2048)
	for {
		count, err := conn.Read(buf)
		if count > 0 {
			for _, item := range dec.Push(buf[:count]) {
				n.takeKiss(item)
			}
		}
		if err != nil {
			return
		}
	}
}

func (n *Node) takeKiss(item kiss.Frame) {
	if n.sw == nil || item.Port != 0 || item.Command != kiss.CmdData {
		return
	}
	frame, err := ax25.DecodeBody(item.Data)
	if err != nil || len(frame.Digis) > 0 || ax25.IsUI(frame.Control) {
		return
	}
	if !frame.Dest.Equal(n.local) || frame.Src.Equal(n.local) {
		return
	}
	n.mu.Lock()
	udp := n.lookupAddr(frame.Src) != nil
	n.mu.Unlock()
	if udp {
		return
	}
	n.sw.Inject(frame)
}

// Kiss returns the TNC address. It is empty when the radio is off.
func (n *Node) Kiss() string { return n.kissAddr }

// dialRadio opens an AX.25 session on the KISS port. The callsign does
// not have to be listed. Bulletin forwarding does not use this path.
func (n *Node) dialRadio(ctx context.Context, addr ax25.Address) (*ax25.Stream, error) {
	if n.kissAddr == "" || n.sw == nil {
		return nil, errors.New("radio is off")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return n.sw.Dial(ctx, addr)
}
