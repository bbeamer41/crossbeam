// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	// threadIdle is how long a MeshCore or Reticulum sender's history
	// is kept after their last question.
	threadIdle = 30 * time.Minute
	threadMax  = 256
)

// MessageHelp is the reply to AI with no question over MeshCore or
// Reticulum.
const MessageHelp = "Send AI and a question, such as AI what is a repeater. The assistant remembers your last few questions for 30 minutes. Send AI /NEW to start over."

type thread struct {
	msgs []Message
	last time.Time
}

// threads is the short history of each MeshCore contact and Reticulum
// sender. There is no session on those paths, so history is kept by
// sender and dropped after threadIdle.
type threads struct {
	mu  sync.Mutex
	all map[string]*thread
}

func (t *threads) get(key string, now time.Time) []Message {
	t.mu.Lock()
	defer t.mu.Unlock()
	th := t.all[key]
	if th == nil || now.Sub(th.last) > threadIdle {
		return nil
	}
	return append([]Message(nil), th.msgs...)
}

func (t *threads) put(key string, msgs []Message, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.all == nil {
		t.all = map[string]*thread{}
	}
	if _, ok := t.all[key]; !ok && len(t.all) >= threadMax {
		oldest := ""
		for k, th := range t.all {
			if now.Sub(th.last) > threadIdle {
				delete(t.all, k)
				continue
			}
			if oldest == "" || th.last.Before(t.all[oldest].last) {
				oldest = k
			}
		}
		if len(t.all) >= threadMax && oldest != "" {
			delete(t.all, oldest)
		}
	}
	t.all[key] = &thread{msgs: msgs, last: now}
}

func (t *threads) drop(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.all, key)
}

// Message answers one MeshCore or Reticulum message. ok is false when
// the text does not start with AI, so the caller can treat it as some
// other command or ignore it. The reply uses LF line breaks. The sender
// keeps a short history: AI /NEW forgets it.
func (s *Service) Message(ctx context.Context, who Caller, text string) (string, bool) {
	if !IsCommand(text) {
		return "", false
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return "The AI assistant is not available.", true
	}
	if ok, why := cfg.Allowed(who); !ok {
		return why, true
	}
	question := Question(text)
	key := limitKey(who)
	switch strings.ToUpper(question) {
	case "", "/?", "/HELP", "?":
		return MessageHelp, true
	case "/NEW", "/CLEAR":
		s.threads.drop(key)
		return "Starting a new conversation.", true
	}
	max, wait := cfg.Reply, cfg.Timeout
	if who.Mesh {
		if max > meshReply {
			max = meshReply
		}
		if wait > meshWait {
			wait = meshWait
		}
	}
	history := s.threads.get(key, s.now())
	r := s.ask(ctx, cfg, who, history, question, max, wait)
	if r.ok {
		s.threads.put(key, trimHistory(append(history,
			Message{Role: "user", Content: question},
			Message{Role: "assistant", Content: r.text})), s.now())
	}
	return r.text, true
}
