// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"context"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/settings"
)

// term is the caller's side of a connected session.
type term struct {
	mu    sync.Mutex
	out   strings.Builder
	lines chan string
	reads atomic.Int32
}

func newTerm() *term { return &term{lines: make(chan string, 16)} }

func (t *term) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.out.Write(p)
}

func (t *term) text() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.out.String()
}

func (t *term) read() (string, error) {
	t.reads.Add(1)
	line, ok := <-t.lines
	if !ok {
		return "", io.EOF
	}
	return line, nil
}

func (t *term) waitFor(tb testing.TB, want string) {
	tb.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(t.text(), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	tb.Fatalf("never saw %q in %q", want, t.text())
}

func converse(svc *Service, who Caller, tm *term, first string) chan error {
	done := make(chan error, 1)
	go func() { done <- svc.Converse(context.Background(), who, tm, tm.read, first) }()
	return done
}

func waitDone(t *testing.T, done chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("chat did not end")
		return nil
	}
}

func TestConverseKeepsHistoryAndExits(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(r fakeRequest) (int, string, time.Duration) {
		return 200, "Answer to: " + r.Msgs[len(r.Msgs)-1].Content, 0
	})
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL})
	tm := newTerm()
	done := converse(svc, radioCaller, tm, "AI what is APRS")
	tm.waitFor(t, "Answer to: what is APRS\r")
	if !strings.Contains(tm.text(), "logged for the control operator") || !strings.Contains(tm.text(), "Thinking...\r") {
		t.Fatalf("banner %q", tm.text())
	}
	tm.lines <- "and who runs it?"
	tm.waitFor(t, "Answer to: and who runs it?\r")
	reqs := fake.all()
	if len(reqs[1].Msgs) != 4 || reqs[1].Msgs[1].Content != "what is APRS" || reqs[1].Msgs[2].Role != "assistant" {
		t.Fatalf("second request did not carry history: %+v", reqs[1].Msgs)
	}
	tm.lines <- "/new"
	tm.waitFor(t, "Starting a new conversation.\r")
	tm.lines <- "fresh"
	tm.waitFor(t, "Answer to: fresh\r")
	if n := len(fake.all()[2].Msgs); n != 2 {
		t.Fatalf("after /NEW the request had %d messages", n)
	}
	tm.lines <- "BYE"
	if err := waitDone(t, done); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(tm.text(), "Back to the BBS.\r") {
		t.Fatalf("exit %q", tm.text())
	}
	// No read is left running, so the station prompt gets the next line.
	time.Sleep(20 * time.Millisecond)
	reads := tm.reads.Load()
	tm.lines <- "LIST"
	time.Sleep(20 * time.Millisecond)
	if tm.reads.Load() != reads || len(tm.lines) != 1 {
		t.Fatal("chat read a line after it returned")
	}
}

func TestConversePagesLongReply(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("Packet radio is fun and useful. ", 60))
	fake := newFake(t, settings.AIBackendOllama, func(fakeRequest) (int, string, time.Duration) { return 200, long, 0 })
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL, settings.KeyAIReply: "2000"})
	tm := newTerm()
	done := converse(svc, radioCaller, tm, "AI tell me")
	tm.waitFor(t, "-- Enter for more, /STOP to skip --\r")
	for _, l := range strings.Split(tm.text(), "\r") {
		if len(l) > Width {
			t.Fatalf("line over %d: %q", Width, l)
		}
	}
	tm.lines <- ""
	time.Sleep(50 * time.Millisecond)
	if strings.Count(tm.text(), "-- Enter for more") < 2 {
		t.Fatalf("second page %q", tm.text())
	}
	tm.lines <- "/stop"
	tm.waitFor(t, "AI>\r")
	tm.lines <- "/exit"
	if err := waitDone(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestConverseDisconnectCancelsRequest(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(fakeRequest) (int, string, time.Duration) { return 200, "late", 10 * time.Second })
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL})
	tm := newTerm()
	done := converse(svc, radioCaller, tm, "AI slow question")
	tm.waitFor(t, "Thinking...\r")
	close(tm.lines) // the link drops
	if err := waitDone(t, done); err == nil {
		t.Fatal("a dropped link returned nil")
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(fake.all()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if reqs := fake.all(); len(reqs) != 1 || !reqs[0].Cancel {
		t.Fatalf("request was not cancelled: %+v", reqs)
	}
}

func TestConverseStopAndExitWhileThinking(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(r fakeRequest) (int, string, time.Duration) {
		if r.Msgs[len(r.Msgs)-1].Content == "quick" {
			return 200, "quick answer", 0
		}
		return 200, "late", 10 * time.Second
	})
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL})
	tm := newTerm()
	done := converse(svc, radioCaller, tm, "AI")
	tm.waitFor(t, "AI>\r")
	tm.lines <- "slow one"
	tm.waitFor(t, "Thinking...\r")
	tm.lines <- "hello?"
	tm.waitFor(t, "Still thinking. Send /STOP to cancel.\r")
	tm.lines <- "/STOP"
	tm.waitFor(t, "Cancelled.\r")
	tm.lines <- "quick"
	tm.waitFor(t, "quick answer\r")
	tm.lines <- "slow two"
	time.Sleep(30 * time.Millisecond)
	tm.lines <- "/EXIT"
	if err := waitDone(t, done); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(tm.text(), "Back to the BBS.\r") {
		t.Fatalf("%q", tm.text())
	}
}

func TestConverseRefusals(t *testing.T) {
	off, _ := newService(t, map[string]string{settings.KeyAIEnabled: "false"})
	tm := newTerm()
	if err := waitDone(t, converse(off, radioCaller, tm, "AI hi")); err != nil {
		t.Fatal(err)
	}
	if tm.text() != "The AI assistant is not turned on at this station.\r" {
		t.Fatalf("off %q", tm.text())
	}
	on, _ := newService(t, map[string]string{settings.KeyAIURL: "http://127.0.0.1:1"})
	tm = newTerm()
	if err := waitDone(t, converse(on, Caller{Via: "telnet"}, tm, "AI")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tm.text(), "needs a callsign") {
		t.Fatalf("telnet %q", tm.text())
	}
	if tm.reads.Load() != 0 {
		t.Fatal("a refused chat read a line")
	}
}
