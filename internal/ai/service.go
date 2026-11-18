// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	// maxTurns is how many question and answer pairs a chat remembers.
	maxTurns = 6
	// maxHistory is the most bytes of earlier turns sent with a question.
	maxHistory = 4000
	// pageLines is how many lines are sent before the caller is asked to
	// press Enter. Twelve full lines are about seven seconds at 1200 baud.
	pageLines = 12
	// slots is how many questions may wait on the AI server at once.
	slots = 2
	// meshWait caps a MeshCore question, because the companion loop waits.
	meshWait  = 45 * time.Second
	meshReply = 400
)

// ErrLong is returned by a read function for a line over the limit.
var ErrLong = errors.New("line is too long")

// Service is the assistant shared by every session. Settings are read
// for each chat and each question, so a saved change applies at once.
type Service struct {
	settings func(context.Context) (map[string]string, error)
	dir      string
	client   *http.Client
	journal  *Journal
	limits   limiter
	slots    chan struct{}
	now      func() time.Time
	threads  threads
}

// New returns the assistant. read is the settings reader, usually
// store.Settings. dir is the state directory, which holds the key file
// and the AI log.
func New(read func(context.Context) (map[string]string, error), dir string) *Service {
	return &Service{
		settings: read,
		dir:      dir,
		client:   NewHTTPClient(),
		journal:  NewJournal(dir),
		slots:    make(chan struct{}, slots),
		now:      time.Now,
	}
}

// SetHTTPClient replaces the client, for a test server.
func (s *Service) SetHTTPClient(c *http.Client) { s.client = c }

// Config reads the settings and the key file.
func (s *Service) Config(ctx context.Context) (Config, error) {
	if s == nil || s.settings == nil {
		return Config{}, errors.New("the AI assistant is not set up")
	}
	current, err := s.settings(ctx)
	if err != nil {
		return Config{}, err
	}
	key := ""
	if s.dir != "" {
		key, _ = ReadKey(s.dir)
	}
	return FromSettings(current, key), nil
}

// On reports whether the assistant is turned on.
func (s *Service) On(ctx context.Context) bool {
	if s == nil {
		return false
	}
	cfg, err := s.Config(ctx)
	return err == nil && cfg.Enabled
}

// system is what the model is always told. It carries the station's
// Part 97 limits so the model declines before the filter has to act.
func system(cfg Config, max int) string {
	text := fmt.Sprintf("You are the AI assistant on amateur radio station %s. Callers reach you over slow packet radio, so answer in at most %d characters. "+
		"Write plain English in plain ASCII text: no markdown, no tables, no emoji. "+
		"Never use profanity or obscene or indecent language. "+
		"Never write in codes or ciphers, or encode text to hide its meaning. "+
		"Do not help with business or commercial dealings, advertising, or selling anything. "+
		"If a request asks for any of these, decline in one short sentence.", cfg.Station, max)
	if cfg.Instructions != "" {
		text += " " + cfg.Instructions
	}
	return text
}

// result is one answered or refused question.
type result struct {
	text    string // the reply, or the sentence the caller sees instead
	outcome string
	ok      bool
	err     error // the server error, for crossbeam ai test only
}

// ask runs one question with the history of this chat.
func (s *Service) ask(ctx context.Context, cfg Config, who Caller, history []Message, question string, max int, wait time.Duration) result {
	start := s.now()
	entry := Entry{Time: start, Caller: who.Call, Via: who.Via, Model: cfg.Model, Question: question}
	finish := func(r result) result {
		entry.Outcome = r.outcome
		if r.ok || r.outcome == OutWithheld {
			entry.Reply = r.text
		}
		entry.Millis = s.now().Sub(start).Milliseconds()
		if err := s.journal.Add(entry); err != nil {
			log.Printf("crossbeam: ai log could not be written")
		}
		if entry.Reply != "" {
			log.Printf("crossbeam: ai %s %s %s", logWho(who), entry.Outcome, plural(len(entry.Reply), "byte"))
		} else {
			log.Printf("crossbeam: ai %s %s", logWho(who), entry.Outcome)
		}
		return r
	}
	if Profane(question) || Encoded(question) {
		return finish(result{text: "That question cannot be sent on amateur radio.", outcome: OutRefused})
	}
	if ok, left := s.limits.take(limitKey(who), cfg.Hourly, start); !ok {
		mins := int((left + time.Minute - 1) / time.Minute)
		return finish(result{text: fmt.Sprintf("You have reached %d questions this hour. Try again in %s.", cfg.Hourly, plural(mins, "minute")), outcome: OutLimited})
	}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		return finish(result{text: "The AI server is busy with other callers. Try again shortly.", outcome: OutBusy})
	}
	msgs := make([]Message, 0, len(history)+2)
	msgs = append(msgs, Message{Role: "system", Content: system(cfg, max)})
	msgs = append(msgs, history...)
	msgs = append(msgs, Message{Role: "user", Content: question})
	reqCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	call := cfg
	call.Reply = max
	raw, err := complete(reqCtx, s.client, call, msgs)
	if err != nil {
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return finish(result{text: "Cancelled.", outcome: OutCancelled})
		}
		return finish(result{text: publicError(err), outcome: OutError, err: err})
	}
	text := Cut(Plain(raw), max)
	if Profane(text) || Encoded(text) {
		r := finish(result{text: text, outcome: OutWithheld})
		r.text = "The reply was withheld because it is not allowed on amateur radio."
		return r
	}
	if text == "" {
		return finish(result{text: "The AI server sent an empty answer.", outcome: OutError})
	}
	return finish(result{text: text, outcome: OutAnswered, ok: true})
}

func logWho(who Caller) string {
	call := who.Call
	if call == "" || strings.ContainsAny(call, "\r\n ") || len(call) > 40 {
		call = "unknown"
	}
	if who.Via != "" {
		return call + " " + who.Via
	}
	return call
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// IsCommand reports whether line starts the assistant: AI, or AI and a
// question.
func IsCommand(line string) bool {
	fields := strings.Fields(line)
	return len(fields) > 0 && strings.EqualFold(fields[0], "AI")
}

// Question is the text after AI, or empty.
func Question(line string) string {
	line = strings.TrimSpace(line)
	if len(line) < 2 || !strings.EqualFold(line[:2], "AI") {
		return ""
	}
	return strings.TrimSpace(line[2:])
}

// trimHistory keeps the last maxTurns pairs within maxHistory bytes.
func trimHistory(h []Message) []Message {
	if n := len(h) - 2*maxTurns; n > 0 {
		h = h[n:]
	}
	size := 0
	for _, m := range h {
		size += len(m.Content)
	}
	for len(h) >= 2 && size > maxHistory {
		size -= len(h[0].Content) + len(h[1].Content)
		h = h[2:]
	}
	return append([]Message(nil), h...)
}

func writeAll(w io.Writer, text string) error {
	_, err := io.WriteString(w, text)
	return err
}

// Test sends one question for the sysop, even while the assistant is
// off, so the server can be checked before callers can use it. It goes
// through the same filter and log as a caller's question.
func (s *Service) Test(ctx context.Context, question string) (string, error) {
	cfg, err := s.Config(ctx)
	if err != nil {
		return "", err
	}
	if cfg.URL == "" || cfg.Model == "" {
		return "", errors.New("set ai.url and ai.model first")
	}
	who := Caller{Call: "SYSOP", Via: "test"}
	r := s.ask(ctx, cfg, who, nil, question, cfg.Reply, cfg.Timeout)
	if r.err != nil {
		return "", sysopError(cfg, r.err)
	}
	if !r.ok {
		return "", errors.New(strings.TrimSuffix(r.text, "."))
	}
	return r.text, nil
}
