// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/settings"
)

const sender = "0123456789abcdef0123456789abcdef"

func bridgeAsk(t *testing.T, conn net.Conn, rd *bufio.Reader, req BridgeRequest) BridgeReply {
	t.Helper()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := rd.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var out BridgeReply
	if err := json.Unmarshal(line, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestReticulumBridge(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(r fakeRequest) (int, string, time.Duration) {
		q := r.Msgs[len(r.Msgs)-1].Content
		if q == "rude" {
			return 200, "what the fuck", 0
		}
		return 200, "Reticulum answer to " + q, 0
	})
	svc, dir := newService(t, map[string]string{settings.KeyAIURL: fake.URL, settings.KeyAIReticulum: "true"})
	if err := svc.Approve("reticulum:" + sender); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), SocketName)
	ln, err := ListenBridge(path, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v", info.Mode())
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() { svc.ServeBridge(ctx, ln); close(served) }()
	defer func() { cancel(); <-served }()

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	rd := bufio.NewReader(conn)

	if got := bridgeAsk(t, conn, rd, BridgeRequest{From: sender, Text: "hello there"}); !got.Ignore {
		t.Fatalf("plain text was answered: %+v", got)
	}
	if got := bridgeAsk(t, conn, rd, BridgeRequest{From: "nope", Text: "AI hi"}); got.Error == "" {
		t.Fatalf("bad address accepted: %+v", got)
	}
	got := bridgeAsk(t, conn, rd, BridgeRequest{From: strings.ToUpper(sender), Text: "AI what is LXMF"})
	if got.Reply != "Reticulum answer to what is LXMF" {
		t.Fatalf("reply %+v", got)
	}
	got = bridgeAsk(t, conn, rd, BridgeRequest{From: sender, Text: "ai and who wrote it"})
	reqs := fake.all()
	if len(reqs[len(reqs)-1].Msgs) != 4 || got.Reply == "" {
		t.Fatalf("history not kept per sender: %+v", reqs[len(reqs)-1].Msgs)
	}
	if got := bridgeAsk(t, conn, rd, BridgeRequest{From: sender, Text: "AI rude"}); !strings.Contains(got.Reply, "withheld") {
		t.Fatalf("filter %+v", got)
	}
	// Same hourly limit as packet, counted per sender.
	cfg, _ := svc.Config(context.Background())
	for i := 0; i < cfg.Hourly; i++ {
		bridgeAsk(t, conn, rd, BridgeRequest{From: sender, Text: "AI again"})
	}
	if got := bridgeAsk(t, conn, rd, BridgeRequest{From: sender, Text: "AI once more"}); !strings.Contains(got.Reply, "questions this hour") {
		t.Fatalf("limit %+v", got)
	}
	entries, _ := ReadLog(dir, 0)
	logged := false
	for _, e := range entries {
		if e.Via == "reticulum" && e.Caller == sender && e.Outcome == OutAnswered {
			logged = true
			break
		}
	}
	if !logged {
		t.Fatalf("log %+v", entries)
	}
}

func TestReticulumOffIsQuiet(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(fakeRequest) (int, string, time.Duration) { return 200, "x", 0 })
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL}) // ai.reticulum stays false
	if got := svc.Bridge(context.Background(), BridgeRequest{From: sender, Text: "AI hi"}); !got.Ignore || got.Reply != "" {
		t.Fatalf("answered while off: %+v", got)
	}
	off, _ := newService(t, map[string]string{settings.KeyAIEnabled: "false", settings.KeyAIReticulum: "true"})
	if got := off.Bridge(context.Background(), BridgeRequest{From: sender, Text: "AI hi"}); !got.Ignore {
		t.Fatalf("answered while the assistant is off: %+v", got)
	}
	if len(fake.all()) != 0 {
		t.Fatal("a request reached the server")
	}
}

func TestListenBridgeLeavesOtherFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), SocketName)
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListenBridge(path, 0o600); err == nil {
		t.Fatal("replaced a regular file")
	}
	stale := filepath.Join(t.TempDir(), SocketName)
	ln, err := net.Listen("unix", stale)
	if err != nil {
		t.Fatal(err)
	}
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	ln.Close()
	ln2, err := ListenBridge(stale, 0o600)
	if err != nil {
		t.Fatalf("stale socket: %v", err)
	}
	ln2.Close()
}
