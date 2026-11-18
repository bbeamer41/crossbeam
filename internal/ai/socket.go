// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// SocketName is the bridge socket next to the admin socket. The
// Crossbeam LXMF bridge, a separate program that speaks Reticulum,
// sends each message it receives here and sends back the reply.
// Crossbeam itself does not speak Reticulum.
const SocketName = "ai.sock"

const (
	maxBridgeLine = 16 << 10
	bridgeIdle    = 10 * time.Minute
)

// BridgeRequest is one LXMF message, one JSON object per line.
type BridgeRequest struct {
	// From is the sender's LXMF address, as hex.
	From string `json:"from"`
	// Text is the message content.
	Text string `json:"text"`
}

// BridgeReply is the answer, one JSON object per line. Ignore is true
// when the bridge should not reply at all: the text did not start with
// AI, or the assistant is off for Reticulum.
type BridgeReply struct {
	Reply  string `json:"reply,omitempty"`
	Ignore bool   `json:"ignore,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ListenBridge opens the bridge socket with mode. A stale socket from
// an earlier run is replaced. Any other file at path is left alone.
func ListenBridge(path string, mode fs.FileMode) (net.Listener, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&fs.ModeSocket == 0 {
			return nil, errors.New("the AI bridge socket path is not a socket")
		}
		if conn, err := net.DialTimeout("unix", path, 200*time.Millisecond); err == nil {
			conn.Close()
			return nil, errors.New("another Crossbeam is using the AI bridge socket")
		}
		_ = os.Remove(path)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, mode); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// ServeBridge answers bridge connections until ctx ends or ln closes.
func (s *Service) ServeBridge(ctx context.Context, ln net.Listener) {
	var wg sync.WaitGroup
	defer wg.Wait()
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.bridgeConn(ctx, conn)
		}()
	}
}

func (s *Service) bridgeConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	scan := bufio.NewScanner(conn)
	scan.Buffer(make([]byte, 4096), maxBridgeLine)
	enc := json.NewEncoder(conn)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(bridgeIdle))
		if !scan.Scan() {
			return
		}
		var req BridgeRequest
		var out BridgeReply
		if err := json.Unmarshal(scan.Bytes(), &req); err != nil {
			out.Error = "bad request"
		} else {
			out = s.Bridge(ctx, req)
		}
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := enc.Encode(out); err != nil {
			return
		}
	}
}

// Bridge answers one LXMF message.
func (s *Service) Bridge(ctx context.Context, req BridgeRequest) BridgeReply {
	from := strings.ToLower(strings.TrimSpace(req.From))
	if !lxmfAddress(from) {
		return BridgeReply{Error: "from must be an LXMF address in hex"}
	}
	if len(req.Text) > 4096 {
		return BridgeReply{Ignore: true}
	}
	if !IsCommand(req.Text) {
		return BridgeReply{Ignore: true}
	}
	cfg, err := s.Config(ctx)
	if err != nil || !cfg.Enabled || !cfg.Reticulum {
		// Quiet when off, so the station does not answer strangers.
		return BridgeReply{Ignore: true}
	}
	reply, ok := s.Message(ctx, Caller{Call: from, Reticulum: true, Via: "reticulum"}, req.Text)
	if !ok {
		return BridgeReply{Ignore: true}
	}
	return BridgeReply{Reply: reply}
}

// lxmfAddress is a Reticulum destination hash: 16 bytes as 32 hex digits.
func lxmfAddress(s string) bool {
	if len(s) != 32 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
