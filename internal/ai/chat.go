// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"context"
	"errors"
	"strings"
	"time"
)

const chatHelp = "AI chat commands:\r" +
	"/NEW     Forget this conversation\r" +
	"/STOP    Cancel a question or skip the rest of a reply\r" +
	"/EXIT    Back to the BBS prompt (/MENU works too)\r" +
	"BYE      Disconnect (also EXIT, QUIT, /BYE, /QUIT, /Q)\r" +
	"/?       This list\r"

// Leave is how a chat ended.
type Leave int

const (
	// ToMenu means the caller is back at the station prompt.
	ToMenu Leave = iota
	// Disconnect means the caller sent BYE. The sign-off is already
	// written, and the session should close the link.
	Disconnect
)

// act is what a line typed in chat asks for.
type act int

const (
	actNone act = iota
	actMenu
	actBye
)

func action(cmd string) act {
	switch cmd {
	case "/EXIT", "/MENU":
		return actMenu
	case "BYE", "EXIT", "QUIT", "/BYE", "/QUIT", "/Q":
		return actBye
	}
	return actNone
}

// line is one read from the caller.
type line struct {
	text string
	err  error
}

// reader keeps at most one read in flight. While the AI server works,
// that read is how a dropped link or a /STOP is noticed. A line that
// arrives first is kept for the next prompt, so no input is lost.
type reader struct {
	read    func() (string, error)
	ch      chan line
	pending bool
}

func (r *reader) start() {
	if r.pending {
		return
	}
	r.pending = true
	go func() {
		text, err := r.read()
		r.ch <- line{text: text, err: err}
	}()
}

func (r *reader) next() line {
	r.start()
	got := <-r.ch
	r.pending = false
	return got
}

func (r *reader) wait() <-chan line {
	r.start()
	return r.ch
}

func (r *reader) took() { r.pending = false }

// Converse runs chat mode on a connected session until the caller
// leaves. read returns one line without its line ending, or ErrLong. It
// is called on another goroutine, never twice at once, and never after
// Converse returns to the prompt. first is the line that started chat,
// such as "AI" or "AI what is APRS". With a nil error, Leave says
// whether the caller is back at the station prompt or sent BYE. Any
// other error means the link is gone.
func (s *Service) Converse(ctx context.Context, who Caller, w interface{ Write([]byte) (int, error) }, read func() (string, error), first string) (Leave, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg, err := s.Config(ctx)
	if err != nil {
		return ToMenu, writeAll(w, "The AI assistant is not available.\r")
	}
	if ok, why := cfg.Allowed(who); !ok {
		return ToMenu, writeAll(w, why+"\r")
	}
	if err := writeAll(w, banner(cfg)); err != nil {
		return ToMenu, err
	}
	leave := func(a act) (Leave, error) {
		if a == actBye {
			return Disconnect, writeAll(w, "73 de "+cfg.Station+", disconnecting.\r")
		}
		return ToMenu, writeAll(w, "Back to the BBS.\r")
	}
	in := &reader{read: read, ch: make(chan line, 1)}
	var history []Message
	next := Question(first)
	for {
		if next == "" {
			if err := writeAll(w, "AI>\r"); err != nil {
				return ToMenu, err
			}
			got := in.next()
			if errors.Is(got.err, ErrLong) {
				if err := writeAll(w, "The line is too long.\r"); err != nil {
					return ToMenu, err
				}
				continue
			}
			if got.err != nil {
				return ToMenu, got.err
			}
			next = strings.TrimSpace(got.text)
			if next == "" {
				continue
			}
		}
		question := next
		next = ""
		cmd := strings.ToUpper(question)
		if a := action(cmd); a != actNone {
			return leave(a)
		}
		switch {
		case cmd == "/NEW" || cmd == "/CLEAR":
			history = nil
			if err := writeAll(w, "Starting a new conversation.\r"); err != nil {
				return ToMenu, err
			}
			continue
		case cmd == "/?" || cmd == "/HELP" || cmd == "?":
			if err := writeAll(w, chatHelp); err != nil {
				return ToMenu, err
			}
			continue
		case cmd == "/STOP":
			continue
		case strings.HasPrefix(cmd, "/"):
			if err := writeAll(w, "Unknown chat command. Send /? for help.\r"); err != nil {
				return ToMenu, err
			}
			continue
		}
		// Settings are read again so a change on the panel applies now.
		if fresh, err := s.Config(ctx); err == nil {
			cfg = fresh
		}
		if ok, why := cfg.Allowed(who); !ok {
			if err := writeAll(w, why+"\r"); err != nil {
				return ToMenu, err
			}
			return leave(actMenu)
		}
		if err := writeAll(w, "Thinking...\r"); err != nil {
			return ToMenu, err
		}
		r, a, err := s.waitAnswer(ctx, cfg, who, history, question, in, w)
		if err != nil {
			return ToMenu, err
		}
		if r.ok {
			history = trimHistory(append(history,
				Message{Role: "user", Content: question},
				Message{Role: "assistant", Content: r.text}))
		}
		if a != actNone {
			return leave(a)
		}
		a, err = page(w, r.text, in)
		if err != nil {
			return ToMenu, err
		}
		if a != actNone {
			return leave(a)
		}
	}
}

// waitAnswer asks the server while watching the link. A dropped link
// or /STOP cancels the request. /EXIT or BYE cancels it and leaves.
func (s *Service) waitAnswer(ctx context.Context, cfg Config, who Caller, history []Message, question string, in *reader, w interface{ Write([]byte) (int, error) }) (result, act, error) {
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan result, 1)
	go func() {
		done <- s.ask(reqCtx, cfg, who, history, question, cfg.Reply, cfg.Timeout)
	}()
	gone := func(err error) (result, act, error) {
		cancel()
		return <-done, actNone, err
	}
	leave := actNone
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		// After /EXIT or BYE no new read is started, so the station
		// prompt gets the next line.
		var input <-chan line
		if leave == actNone {
			input = in.wait()
		}
		select {
		case r := <-done:
			return r, leave, nil
		case got := <-input:
			in.took()
			if got.err != nil && !errors.Is(got.err, ErrLong) {
				return gone(got.err)
			}
			cmd := strings.ToUpper(strings.TrimSpace(got.text))
			switch {
			case cmd == "/STOP":
				cancel()
			case action(cmd) != actNone:
				leave = action(cmd)
				cancel()
			default:
				if err := writeAll(w, "Still thinking. Send /STOP to cancel.\r"); err != nil {
					return gone(err)
				}
			}
		case <-tick.C:
			// The caller already signed off or went back to the prompt.
			// The sign-off is sent when the cancelled request finishes.
			if leave != actNone {
				continue
			}
			// A keep-alive line so a slow model does not look like a dead link.
			if err := writeAll(w, "Still thinking...\r"); err != nil {
				return gone(err)
			}
		}
	}
}

// page writes text in pages of pageLines. Between pages the caller
// presses Enter for more, or sends /STOP to skip the rest.
func page(w interface{ Write([]byte) (int, error) }, text string, in *reader) (act, error) {
	lines := Wrap(text, Width)
	for len(lines) > 0 {
		n := len(lines)
		if n > pageLines {
			n = pageLines
		}
		if err := writeAll(w, strings.Join(lines[:n], "\r")+"\r"); err != nil {
			return actNone, err
		}
		lines = lines[n:]
		if len(lines) == 0 {
			break
		}
		if err := writeAll(w, "-- Enter for more, /STOP to skip --\r"); err != nil {
			return actNone, err
		}
		got := in.next()
		if got.err != nil && !errors.Is(got.err, ErrLong) {
			return actNone, got.err
		}
		cmd := strings.ToUpper(strings.TrimSpace(got.text))
		if a := action(cmd); a != actNone {
			return a, nil
		}
		if cmd == "/STOP" {
			return actNone, nil
		}
	}
	return actNone, nil
}

func banner(cfg Config) string {
	return "AI chat on " + cfg.Station + " using " + cfg.Model + ".\r" +
		"Replies are machine-made and may be wrong. Questions and replies\r" +
		"are logged for the control operator. Keep it legal for amateur radio.\r" +
		"/EXIT goes back to the BBS, BYE disconnects, /? for help.\r"
}
