// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/settings"
)

func TestSenderApprovalGate(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(r fakeRequest) (int, string, time.Duration) {
		return 200, "Answer to " + r.Msgs[len(r.Msgs)-1].Content, 0
	})
	svc, dir := newService(t, map[string]string{
		settings.KeyAIURL:       fake.URL,
		settings.KeyAIMesh:      "true",
		settings.KeyAIReticulum: "true",
		settings.KeyAIHourly:    "2",
	})
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	mesh := Caller{Call: "a1b2c3", Mesh: true, Via: "meshcore"}
	rns := Caller{Call: sender, Reticulum: true, Via: "reticulum"}

	got, ok := svc.Message(context.Background(), mesh, "AI what is a repeater")
	if !ok || got != WaitingApproval || len(fake.all()) != 0 {
		t.Fatalf("first mesh %q ok=%v requests=%d", got, ok, len(fake.all()))
	}
	got, ok = svc.Message(context.Background(), mesh, "AI again")
	if !ok || got != "" || len(fake.all()) != 0 {
		t.Fatalf("second mesh %q ok=%v", got, ok)
	}
	lists, err := svc.Senders()
	if err != nil || len(lists.Pending) != 1 || lists.Pending[0].ID != "meshcore:a1b2c3" || lists.Pending[0].Network != NetMesh || !strings.Contains(lists.Pending[0].Text, "repeater") {
		t.Fatalf("pending %+v %v", lists, err)
	}

	got, ok = svc.Message(context.Background(), rns, "AI what is LXMF")
	if !ok || got != WaitingApproval || len(fake.all()) != 0 {
		t.Fatalf("first rns %q ok=%v requests=%d", got, ok, len(fake.all()))
	}
	if got, ok = svc.Message(context.Background(), rns, "AI again"); !ok || got != "" {
		t.Fatalf("second rns %q ok=%v", got, ok)
	}
	now = now.Add(23 * time.Hour)
	if got, ok = svc.Message(context.Background(), mesh, "AI still waiting"); !ok || got != "" {
		t.Fatalf("same day %q ok=%v", got, ok)
	}
	now = now.Add(time.Hour)
	if got, ok = svc.Message(context.Background(), mesh, "AI next day"); !ok || got != WaitingApproval {
		t.Fatalf("next day %q ok=%v", got, ok)
	}
	lists, _ = svc.Senders()
	if len(lists.Pending) != 2 {
		t.Fatalf("pending grew or shrank: %+v", lists.Pending)
	}

	if err := svc.Approve("meshcore:a1b2c3"); err != nil {
		t.Fatal(err)
	}
	got, ok = svc.Message(context.Background(), mesh, "AI what is a repeater")
	if !ok || got != "Answer to what is a repeater" {
		t.Fatalf("approved %q", got)
	}
	// The waiting messages did not use the hourly limit.
	if _, ok = svc.Message(context.Background(), mesh, "AI second"); !ok {
		t.Fatal("second approved question failed")
	}
	if got, _ = svc.Message(context.Background(), mesh, "AI third"); !strings.Contains(got, "questions this hour") {
		t.Fatalf("limit after approval %q", got)
	}

	if err := svc.Deny("reticulum:" + sender); err != nil {
		t.Fatal(err)
	}
	before := len(fake.all())
	got, ok = svc.Message(context.Background(), rns, "AI hello")
	if !ok || got != "" || len(fake.all()) != before {
		t.Fatalf("denied %q ok=%v requests=%d", got, ok, len(fake.all()))
	}
	if reply := svc.Bridge(context.Background(), BridgeRequest{From: sender, Text: "AI hello"}); !reply.Ignore || reply.Reply != "" {
		t.Fatalf("bridge denied %+v", reply)
	}
	lists, _ = svc.Senders()
	if len(lists.Pending) != 0 || len(lists.Denied) != 1 || lists.Denied[0].ID != "reticulum:"+sender {
		t.Fatalf("after deny %+v", lists)
	}

	if err := svc.Revoke("meshcore:a1b2c3"); err != nil {
		t.Fatal(err)
	}
	before = len(fake.all())
	got, ok = svc.Message(context.Background(), mesh, "AI after revoke")
	if !ok || got != WaitingApproval || len(fake.all()) != before {
		t.Fatalf("revoked %q requests=%d", got, len(fake.all()))
	}

	// A radio caller is not on the list and still reaches the model.
	cfg, _ := svc.Config(context.Background())
	if ok, why := cfg.Allowed(radioCaller); !ok {
		t.Fatal(why)
	}
	if r := svc.ask(context.Background(), cfg, radioCaller, nil, "from the radio", 600, time.Second); !r.ok {
		t.Fatalf("radio %+v", r)
	}
	lists, _ = svc.Senders()
	for _, group := range [][]Sender{lists.Pending, lists.Approved, lists.Denied} {
		for _, s := range group {
			if strings.Contains(s.ID, "W5ABC") {
				t.Fatalf("radio caller listed: %+v", s)
			}
		}
	}

	info, err := os.Stat(filepath.Join(dir, SendersName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info, err)
	}
	again := New(testSettings(t, map[string]string{
		settings.KeyAIURL:       fake.URL,
		settings.KeyAIMesh:      "true",
		settings.KeyAIReticulum: "true",
	}), dir)
	loaded, err := again.Senders()
	if err != nil || len(loaded.Approved) != 0 || len(loaded.Denied) != 1 || loaded.Denied[0].Address != sender || len(loaded.Pending) != 1 || loaded.Pending[0].ID != "meshcore:a1b2c3" {
		t.Fatalf("reloaded %+v %v", loaded, err)
	}

	entries, err := ReadLog(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	saw := map[string]bool{}
	for _, e := range entries {
		if e.Via == "sysop" {
			saw[e.Outcome] = true
		}
		if strings.Contains(e.Question, "sk-") || strings.Contains(e.Caller, "secret") {
			t.Fatalf("log leaked %q", e.Question)
		}
	}
	for _, outcome := range []string{OutPending, OutApproved, OutDenied, OutRevoked} {
		if !saw[outcome] {
			t.Fatalf("missing %s in %+v", outcome, entries)
		}
	}
}

func TestSenderQueueFullIsSilent(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(fakeRequest) (int, string, time.Duration) {
		return 200, "no", 0
	})
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL, settings.KeyAIMesh: "true"})
	for i := 0; i < pendingMax; i++ {
		who := Caller{Call: fmt.Sprintf("%012x", i+1), Mesh: true}
		got, ok := svc.Message(context.Background(), who, "AI hello")
		if !ok || got != WaitingApproval {
			t.Fatalf("fill %d %q ok=%v", i, got, ok)
		}
	}
	extra := Caller{Call: fmt.Sprintf("%012x", pendingMax+1), Mesh: true}
	got, ok := svc.Message(context.Background(), extra, "AI overflow")
	if !ok || got != "" {
		t.Fatalf("overflow %q ok=%v", got, ok)
	}
	got, ok = svc.Message(context.Background(), extra, "AI overflow again")
	if !ok || got != "" {
		t.Fatalf("overflow again %q", got)
	}
	lists, err := svc.Senders()
	if err != nil || len(lists.Pending) != pendingMax {
		t.Fatalf("pending %d %v", len(lists.Pending), err)
	}
	for _, s := range lists.Pending {
		if s.ID == "meshcore:"+extra.Call {
			t.Fatal("overflow sender was queued")
		}
	}
	if len(fake.all()) != 0 {
		t.Fatal("a queued sender reached the model")
	}
	// A sender already waiting does not grow the queue, and still gets no
	// second reply the same day.
	first := Caller{Call: fmt.Sprintf("%012x", 1), Mesh: true}
	if got, ok = svc.Message(context.Background(), first, "AI again"); !ok || got != "" {
		t.Fatalf("known pending %q", got)
	}
	lists, _ = svc.Senders()
	if len(lists.Pending) != pendingMax {
		t.Fatalf("queue grew to %d", len(lists.Pending))
	}
}

func TestApprovalOffKeepsToday(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(fakeRequest) (int, string, time.Duration) {
		return 200, "open", 0
	})
	svc, _ := newService(t, map[string]string{
		settings.KeyAIURL:             fake.URL,
		settings.KeyAIMesh:            "true",
		settings.KeyAIRequireApproval: "false",
	})
	if err := svc.Deny("meshcore:aabb"); err != nil {
		t.Fatal(err)
	}
	got, ok := svc.Message(context.Background(), Caller{Call: "aabb", Mesh: true}, "AI hi")
	if !ok || got != "open" || len(fake.all()) != 1 {
		t.Fatalf("off %q ok=%v requests=%d", got, ok, len(fake.all()))
	}
}

func TestSenderListSurvivesABadFile(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(fakeRequest) (int, string, time.Duration) {
		return 200, "no", 0
	})
	svc, dir := newService(t, map[string]string{settings.KeyAIURL: fake.URL, settings.KeyAIMesh: "true"})
	path := filepath.Join(dir, SendersName)
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := svc.Message(context.Background(), Caller{Call: "abcd", Mesh: true}, "AI hi")
	if !ok || got != "" || len(fake.all()) != 0 {
		t.Fatalf("corrupt %q ok=%v", got, ok)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "{" {
		t.Fatalf("file changed to %q %v", raw, err)
	}
	if _, err := svc.Senders(); err == nil {
		t.Fatal("a corrupt list was treated as empty")
	}
}

func TestParseSenderID(t *testing.T) {
	net, addr, err := ParseSenderID("MeshCore:A1B2C3")
	if err != nil || net != NetMesh || addr != "a1b2c3" {
		t.Fatalf("%s %s %v", net, addr, err)
	}
	if _, _, err := ParseSenderID("radio:W5ABC"); err == nil {
		t.Fatal("radio id accepted")
	}
	if err := (&Service{now: time.Now}).Revoke("meshcore:aa"); err != errNotApproved {
		t.Fatalf("revoke %v", err)
	}
}
