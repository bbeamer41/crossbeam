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

type ended struct {
	leave Leave
	err   error
}

func converse(svc *Service, who Caller, tm *term, first string) chan ended {
	done := make(chan ended, 1)
	go func() {
		leave, err := svc.Converse(context.Background(), who, tm, tm.read, first)
		done <- ended{leave, err}
	}()
	return done
}

func waitEnd(t *testing.T, done chan ended) ended {
	t.Helper()
	select {
	case e := <-done:
		return e
	case <-time.After(3 * time.Second):
		t.Fatal("chat did not end")
		return ended{}
	}
}

func waitDone(t *testing.T, done chan ended) error {
	t.Helper()
	e := waitEnd(t, done)
	if e.err == nil && e.leave != ToMenu {
		t.Fatalf("chat ended with %v, not back at the menu", e.leave)
	}
	return e.err
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
	tm.lines <- "/EXIT"
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
	if e := waitEnd(t, done); e.err == nil {
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

func TestConverseByeDisconnects(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(r fakeRequest) (int, string, time.Duration) {
		switch r.Msgs[len(r.Msgs)-1].Content {
		case "slow":
			return 200, "late", 10 * time.Second
		case "long":
			return 200, strings.TrimSpace(strings.Repeat("Packet radio is fun and useful. ", 60)), 0
		}
		return 200, "ok", 0
	})
	for _, c := range []struct {
		name  string
		setup []string // lines before the leave word
		wait  string
		word  string
	}{
		{"prompt BYE", nil, "AI>\r", "BYE"},
		{"prompt bye", nil, "AI>\r", "bye"},
		{"EXIT", nil, "AI>\r", "EXIT"},
		{"QUIT", nil, "AI>\r", "QUIT"},
		{"/BYE", nil, "AI>\r", "/BYE"},
		{"/QUIT", nil, "AI>\r", "/QUIT"},
		{"/Q", nil, "AI>\r", "/q"},
		{"while thinking", []string{"slow"}, "Thinking...\r", "BYE"},
		{"while paging", []string{"long"}, "-- Enter for more", "BYE"},
	} {
		t.Run(c.name, func(t *testing.T) {
			svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL, settings.KeyAIReply: "2000"})
			tm := newTerm()
			done := converse(svc, radioCaller, tm, "AI")
			tm.waitFor(t, "AI>\r")
			for _, l := range c.setup {
				tm.lines <- l
			}
			tm.waitFor(t, c.wait)
			tm.lines <- c.word
			e := waitEnd(t, done)
			if e.err != nil || e.leave != Disconnect {
				t.Fatalf("%v %v", e.leave, e.err)
			}
			if !strings.HasSuffix(tm.text(), "73 de K5LEB-1, disconnecting.\r") {
				t.Fatalf("sign-off %q", tm.text())
			}
			time.Sleep(20 * time.Millisecond)
			reads := tm.reads.Load()
			tm.lines <- "x"
			time.Sleep(20 * time.Millisecond)
			if tm.reads.Load() != reads {
				t.Fatal("chat read a line after BYE")
			}
		})
	}
}

// TestLineRacedWithTheAnswerIsNotTheNextQuestion queues an ordinary
// line before the answer is released, so both are ready together. That
// line must not be sent to the model as the next question.
func TestLineRacedWithTheAnswerIsNotTheNextQuestion(t *testing.T) {
	const raced = "typed while the answer arrived"
	for i := 0; i < 12; i++ {
		var tm *term
		fake := newFake(t, settings.AIBackendOllama, func(r fakeRequest) (int, string, time.Duration) {
			q := r.Msgs[len(r.Msgs)-1].Content
			if q != "first question" {
				return 200, "UNEXPECTED " + q, 0
			}
			deadline := time.Now().Add(2 * time.Second)
			for tm != nil && tm.reads.Load() < 1 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			tm.lines <- raced
			time.Sleep(50 * time.Millisecond)
			return 200, "first answer", 0
		})
		svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL})
		tm = newTerm()
		done := converse(svc, radioCaller, tm, "AI first question")
		tm.waitFor(t, "first answer\r")
		deadline := time.Now().Add(300 * time.Millisecond)
		for time.Now().Before(deadline) {
			if n := len(fake.all()); n != 1 {
				t.Fatalf("pass %d made %d requests", i, n)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !strings.Contains(tm.text(), "Still thinking. Send /STOP to cancel.") {
			t.Fatalf("pass %d did not treat the in-flight line as busy: %q", i, tm.text())
		}
		for _, req := range fake.all() {
			if strings.Contains(req.Msgs[len(req.Msgs)-1].Content, raced) {
				t.Fatalf("pass %d sent the in-flight line to the model", i)
			}
		}
		tm.lines <- "/EXIT"
		if err := waitDone(t, done); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStopQueuedWithTheAnswerCancels(t *testing.T) {
	var tm *term
	var mu sync.Mutex
	var asked []string
	fake := newFake(t, settings.AIBackendOllama, func(r fakeRequest) (int, string, time.Duration) {
		q := r.Msgs[len(r.Msgs)-1].Content
		mu.Lock()
		asked = append(asked, q)
		mu.Unlock()
		deadline := time.Now().Add(2 * time.Second)
		for tm != nil && tm.reads.Load() < 1 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		tm.lines <- "/STOP"
		time.Sleep(50 * time.Millisecond)
		return 200, "secret answer", 0
	})
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL})
	tm = newTerm()
	done := converse(svc, radioCaller, tm, "AI first question")
	tm.waitFor(t, "Cancelled.\r")
	if strings.Contains(tm.text(), "secret answer") {
		t.Fatalf("the raced answer was shown: %q", tm.text())
	}
	// Give a follow-up question time to leave, if the raced line was kept.
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	got := append([]string(nil), asked...)
	mu.Unlock()
	if len(got) != 1 || got[0] != "first question" {
		t.Fatalf("questions sent: %q", got)
	}
	tm.lines <- "/EXIT"
	if err := waitDone(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestChatHelpNamesBothWaysOut(t *testing.T) {
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: "http://127.0.0.1:1"})
	tm := newTerm()
	done := converse(svc, radioCaller, tm, "AI /?")
	tm.waitFor(t, "/EXIT    Back to the BBS prompt")
	tm.waitFor(t, "BYE      Disconnect (also EXIT, QUIT, /BYE, /QUIT, /Q)")
	if !strings.Contains(tm.text(), "/EXIT goes back to the BBS, BYE disconnects") {
		t.Fatalf("banner %q", tm.text())
	}
	tm.lines <- "/menu"
	if err := waitDone(t, done); err != nil {
		t.Fatal(err)
	}
}
