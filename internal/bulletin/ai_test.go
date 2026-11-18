// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package bulletin

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ai"
	"github.com/bbeamer41/crossbeam/internal/settings"
)

// radioConn is a session the link heard on the KISS radio.
type radioConn struct {
	io.ReadWriteCloser
	radio bool
}

func (c radioConn) ForwardAllowed() bool { return false }
func (c radioConn) PeerLabel() string    { return "W5ABC-7 radio" }
func (c radioConn) ViaRadio() bool       { return c.radio }

type screen struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *screen) pump(r io.Reader) {
	b := make([]byte, 256)
	for {
		n, err := r.Read(b)
		s.mu.Lock()
		s.buf.Write(b[:n])
		s.mu.Unlock()
		if err != nil {
			return
		}
	}
}

func (s *screen) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *screen) wait(t *testing.T, want string, count int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(s.text(), want) >= count {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("never saw %q x%d in %q", want, count, s.text())
}

func aiStation(t *testing.T) *ai.Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Messages []ai.Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		q := in.Messages[len(in.Messages)-1].Content
		_ = json.NewEncoder(w).Encode(map[string]any{"message": ai.Message{Role: "assistant", Content: "A TNC answer for " + q}})
	}))
	t.Cleanup(srv.Close)
	st := openStore(t)
	if _, err := st.Apply(context.Background(), map[string]string{
		settings.KeyCallsign:  "K5LEB",
		settings.KeyAIURL:     srv.URL,
		settings.KeyAIModel:   "llama3.2:3b",
		settings.KeyAIEnabled: "true",
	}); err != nil {
		t.Fatal(err)
	}
	svc := ai.New(st.Settings, st.Dir())
	return svc
}

func TestPromptAIChat(t *testing.T) {
	svc := aiStation(t)
	st := openStore(t)
	a, b := net.Pipe()
	defer a.Close()
	errc := make(chan error, 1)
	go func() {
		errc <- ServeAI(context.Background(), st, mustCall(t, "K5LEB"), radioConn{ReadWriteCloser: b, radio: true}, mustCall(t, "W5ABC-7"), svc)
	}()
	sc := &screen{}
	go sc.pump(a)
	sc.wait(t, "K5LEB>\r", 1)
	send := func(s string) {
		if _, err := io.WriteString(a, s+"\r"); err != nil {
			t.Fatal(err)
		}
	}
	send("?")
	sc.wait(t, "AI       Chat with the AI assistant\r", 1)
	send("AI")
	sc.wait(t, "AI>\r", 1)
	send("what is a TNC")
	sc.wait(t, "A TNC answer for what is a TNC\r", 1)
	send("/exit")
	sc.wait(t, "Back to the BBS.\rK5LEB>\r", 1)
	send("LIST")
	sc.wait(t, "No bulletins.\r", 1)
	send("BYE")
	sc.wait(t, "Bye\r", 1)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

func TestPromptAIRefusesUncheckedCaller(t *testing.T) {
	svc := aiStation(t)
	st := openStore(t)
	a, b := net.Pipe()
	defer a.Close()
	errc := make(chan error, 1)
	go func() {
		errc <- ServeAI(context.Background(), st, mustCall(t, "K5LEB"), radioConn{ReadWriteCloser: b}, mustCall(t, "W5ABC-7"), svc)
	}()
	sc := &screen{}
	go sc.pump(a)
	sc.wait(t, "K5LEB>\r", 1)
	_, _ = io.WriteString(a, "AI hello\r")
	sc.wait(t, "The AI assistant is open to radio callers only.\rK5LEB>\r", 1)
	_, _ = io.WriteString(a, "BYE\r")
	sc.wait(t, "Bye\r", 1)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

func TestPromptAIDisconnectMidChat(t *testing.T) {
	svc := aiStation(t)
	st := openStore(t)
	a, b := net.Pipe()
	errc := make(chan error, 1)
	go func() {
		errc <- ServeAI(context.Background(), st, mustCall(t, "K5LEB"), radioConn{ReadWriteCloser: b, radio: true}, mustCall(t, "W5ABC-7"), svc)
	}()
	sc := &screen{}
	go sc.pump(a)
	sc.wait(t, "K5LEB>\r", 1)
	_, _ = io.WriteString(a, "AI\r")
	sc.wait(t, "AI>\r", 1)
	a.Close()
	select {
	case <-errc:
	case <-time.After(3 * time.Second):
		t.Fatal("session did not end after the link dropped")
	}
}
