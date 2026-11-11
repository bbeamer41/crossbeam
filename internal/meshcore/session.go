// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package meshcore

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/bbeamer41/crossbeam/internal/bbs"
	"github.com/bbeamer41/crossbeam/internal/store"
)

const commandWait = 5 * time.Second

// advertEvery is how long a successful flood advert covers. The
// companion firmware does not advert on its own, and a serial reconnect
// every couple of seconds must not flood the mesh.
const advertEvery = 3 * time.Hour

// A direct message can ask for the whole board, and the answer can be
// sixteen radio sends. These caps keep one station, or a pile of
// stations, from filling the channel.
const (
	meshPerMinute = 12
	meshAllMinute = 30
	meshWindow    = time.Minute
)

// ErrDenied is a serial port the process may not open.
var ErrDenied = errors.New("permission denied")

// ErrBusy means this process already has the companion port open.
var ErrBusy = errors.New("the MeshCore radio is in use")

// ErrDown means the companion radio is not answering.
var ErrDown = errors.New("the MeshCore radio is not connected")

type deadliner interface {
	SetReadDeadline(time.Time) error
}

// announcer remembers the last flood advert across serial reconnects in
// this process. A nil announcer allows every session.
type announcer struct {
	next time.Time
}

func (a *announcer) allow(now time.Time) bool {
	if a == nil {
		return true
	}
	return a.next.IsZero() || !now.Before(a.next)
}

func (a *announcer) schedule(now time.Time) {
	if a == nil {
		return
	}
	a.next = now.Add(advertEvery)
}

type session struct {
	rw       io.ReadWriteCloser
	st       *store.Store
	name     string
	ann      *announcer
	seen     map[string]struct{}
	inflight string
	pending  [][]byte
	adverts  [][]byte
	waiting  bool
	gates    map[string]*meter
	all      meter
	told     time.Time
}

type meter struct {
	start time.Time
	n     int
}

// Session speaks the companion protocol on rw. It sets the radio clock,
// names the advert, and floods that advert when ann allows it. Contact
// messages are station-prompt commands, and any other sentence gets one
// short greeting. Channel messages are ignored. One command is in flight
// at a time.
func Session(ctx context.Context, rw io.ReadWriteCloser, st *store.Store, name string, ann *announcer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "BBS"
	}
	s := &session{rw: rw, st: st, name: name, ann: ann, seen: map[string]struct{}{}}
	self, err := s.exchange(ctx, appStart(), respSelfInfo)
	clearBytes(self)
	if err != nil {
		return err
	}
	// A firmware that rejects the query still has a usable command channel.
	// The answer can carry the radio's BLE PIN. It is not kept or logged.
	info, err := s.command(ctx, deviceQuery(), func(f []byte) bool {
		return len(f) > 0 && (f[0] == respDeviceInfo || f[0] == respError)
	})
	clearBytes(info)
	if err != nil {
		return err
	}
	if err := s.prepare(ctx); err != nil {
		return err
	}
	if err := s.syncAdverts(ctx); err != nil {
		return err
	}
	if err := s.drain(ctx); err != nil {
		return err
	}
	if err := s.flush(ctx); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.syncAdverts(ctx); err != nil {
			return err
		}
		if err := s.flush(ctx); err != nil {
			return err
		}
		if s.waiting {
			s.waiting = false
			if err := s.drain(ctx); err != nil {
				return err
			}
			continue
		}
		frame, err := s.read(ctx, true)
		if err != nil {
			return err
		}
		if len(frame) == 0 {
			continue
		}
		switch {
		case frame[0] == pushMsgWaiting:
			if err := s.drain(ctx); err != nil {
				return err
			}
		case frame[0] == respContactMsg || frame[0] == respContactMsgV3:
			if err := s.reply(ctx, frame); err != nil {
				return err
			}
		case chatAdvert(frame):
			s.noteAdvert(frame)
		default:
		}
	}
}

// prepare makes this station look like another companion. The firmware
// drops an advert that has no name, and it drops one that is not newer
// than the last advert it stored. A clock that is already ahead refuses
// the set and is left alone. The next flood waits until the last success
// is three hours old.
func (s *session) prepare(ctx context.Context) error {
	if _, err := s.command(ctx, setDeviceTime(time.Now()), okOrErr); err != nil {
		return err
	}
	if payload, ok := advertName(s.name); ok {
		if _, err := s.command(ctx, payload, okOrErr); err != nil {
			return err
		}
	}
	if !s.ann.allow(time.Now()) {
		return nil
	}
	got, err := s.command(ctx, selfAdvert(), okOrErr)
	if err != nil {
		return err
	}
	if got[0] == respOK {
		s.ann.schedule(time.Now())
	}
	return nil
}

func okOrErr(frame []byte) bool {
	return len(frame) > 0 && (frame[0] == respOK || frame[0] == respError)
}

func (s *session) drain(ctx context.Context) error {
	for i := 0; i < 32; i++ {
		frame, err := s.command(ctx, syncNext(), func(f []byte) bool {
			if len(f) == 0 {
				return false
			}
			switch f[0] {
			case respNoMore, respContactMsg, respContactMsgV3, respChannelMsg, respChannelMsgV3, respChannelData, respError:
				return true
			default:
				return false
			}
		})
		if err != nil {
			return err
		}
		switch frame[0] {
		case respNoMore, respError:
			return s.flush(ctx)
		case respContactMsg, respContactMsgV3:
			if err := s.reply(ctx, frame); err != nil {
				return err
			}
		default:
		}
	}
	return s.flush(ctx)
}

func (s *session) reply(ctx context.Context, frame []byte) error {
	msg, ok := parseContact(frame)
	if !ok || isChannel(frame) {
		return nil
	}
	if len(msg.text) > maxTextBytes || strings.IndexByte(msg.text, 0) >= 0 {
		return nil
	}
	if !s.allow(msg.prefix) {
		return nil
	}
	if !s.remember(msg) {
		return nil
	}
	var out string
	if greet, ok := meshGreeting(s.name, msg.text); ok {
		out = greet
	} else {
		var buf strings.Builder
		if _, err := bbs.Command(ctx, s.st, s.name, msg.text, &buf); err != nil {
			return err
		}
		out = buf.String()
	}
	for _, part := range chunk(out) {
		if part == "" {
			continue
		}
		stop, err := s.sendPart(ctx, msg.prefix, part)
		if err != nil {
			return err
		}
		if stop {
			return s.flush(ctx)
		}
	}
	return s.flush(ctx)
}

// meshGreeting answers a sentence that is not a station-prompt command.
// The packet prompt still reports that line as an unknown command.
func meshGreeting(name, line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", false
	}
	switch strings.ToUpper(fields[0]) {
	case "?", "HELP", "LIST", "L", "READ", "R", "BYE", "QUIT", "EXIT":
		return "", false
	}
	if strings.TrimSpace(name) == "" {
		name = "BBS"
	}
	return name + " here. Send LIST for bulletins, READ and a number to open one, or ? for commands.", true
}

func (s *session) sendPart(ctx context.Context, prefix []byte, part string) (bool, error) {
	payload, err := sendText(prefix, part)
	if err != nil {
		return false, err
	}
	// Drop a copy of this text if it comes back before the send result.
	// A saved copy would also match a later BYE, whose reply is "Bye".
	s.inflight = part
	defer func() { s.inflight = "" }()
	accept := func(f []byte) bool {
		return len(f) > 0 && (f[0] == respMsgSent || f[0] == respError)
	}
	got, err := s.command(ctx, payload, accept)
	if err != nil {
		return false, err
	}
	if got[0] == respError && len(got) > 1 && got[1] == 3 {
		if !sleep(ctx, time.Second) {
			return false, ctx.Err()
		}
		got, err = s.command(ctx, payload, accept)
		if err != nil {
			return false, err
		}
	}
	if got[0] == respError {
		return true, nil
	}
	return false, nil
}

func (s *session) exchange(ctx context.Context, payload []byte, want byte) ([]byte, error) {
	return s.command(ctx, payload, func(f []byte) bool {
		return len(f) > 0 && f[0] == want
	})
}

func (s *session) command(ctx context.Context, payload []byte, done func([]byte) bool) ([]byte, error) {
	// One budget for the whole wait. A fresh budget on every frame would
	// let a stream of log lines hold the command open.
	cmdCtx, cancel := context.WithTimeout(ctx, commandWait)
	defer cancel()
	if err := writeFrame(s.rw, markToRadio, payload); err != nil {
		return nil, err
	}
	for {
		frame, err := s.read(cmdCtx, false)
		if err != nil {
			return nil, err
		}
		if len(frame) == 0 {
			continue
		}
		if frame[0] == pushMsgWaiting {
			s.waiting = true
		}
		if frame[0] == pushLog {
			continue
		}
		// A chat advert can arrive while a send is waiting for its result.
		// An ack can arrive then too. Neither one is the result.
		if chatAdvert(frame) {
			s.noteAdvert(frame)
			continue
		}
		if done(frame) {
			return frame, nil
		}
		// A contact frame can arrive while this command waits. Keep it,
		// unless it is the text this send just handed to the radio.
		if frame[0] == respContactMsg || frame[0] == respContactMsgV3 {
			if s.inflight != "" {
				if msg, ok := parseContact(frame); ok && msg.text == s.inflight {
					continue
				}
			}
			s.stash(frame)
		}
	}
}

func (s *session) stash(frame []byte) {
	if len(s.pending) >= 8 {
		return
	}
	s.pending = append(s.pending, append([]byte(nil), frame...))
}

func (s *session) noteAdvert(frame []byte) {
	if !chatAdvert(frame) {
		return
	}
	if len(s.adverts) >= 8 {
		return
	}
	s.adverts = append(s.adverts, append([]byte(nil), frame...))
}

// syncAdverts stores chat contacts the companion radio did not keep.
// The push is sent back with the add-contact command in front. Ok and
// a firmware error both let the session continue. The name and the key
// are not logged.
func (s *session) syncAdverts(ctx context.Context) error {
	for n := 0; len(s.adverts) > 0 && n < 8; n++ {
		frame := s.adverts[0]
		s.adverts = s.adverts[1:]
		if _, err := s.command(ctx, addContact(frame), okOrErr); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) flush(ctx context.Context) error {
	for n := 0; len(s.pending) > 0 && n < 8; n++ {
		frame := s.pending[0]
		s.pending = s.pending[1:]
		if err := s.reply(ctx, frame); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) allow(prefix []byte) bool {
	if s.gates == nil {
		s.gates = map[string]*meter{}
	}
	now := time.Now()
	key := hex.EncodeToString(prefix)
	g := s.gates[key]
	if g == nil {
		if !meterRoom(&s.all, now, meshAllMinute) {
			s.noteLimited()
			return false
		}
		if len(s.gates) >= 64 {
			for old := range s.gates {
				delete(s.gates, old)
				break
			}
		}
		g = &meter{}
		s.gates[key] = g
	} else if !meterRoom(&s.all, now, meshAllMinute) || !meterRoom(g, now, meshPerMinute) {
		s.noteLimited()
		return false
	}
	meterTake(&s.all, now)
	meterTake(g, now)
	return true
}

func meterRoom(m *meter, now time.Time, max int) bool {
	if m.start.IsZero() || now.Sub(m.start) >= meshWindow {
		return true
	}
	return m.n < max
}

func meterTake(m *meter, now time.Time) {
	if m.start.IsZero() || now.Sub(m.start) >= meshWindow {
		m.start = now
		m.n = 0
	}
	m.n++
}

func (s *session) noteLimited() {
	now := time.Now()
	if !s.told.IsZero() && now.Sub(s.told) < meshWindow {
		return
	}
	s.told = now
	log.Printf("meshcore: direct messages are arriving too often")
}

func clearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func (s *session) remember(msg contact) bool {
	if s.seen == nil {
		s.seen = map[string]struct{}{}
	}
	key := prefixKey(msg.prefix, msg.when, msg.text)
	if _, ok := s.seen[key]; ok {
		return false
	}
	if len(s.seen) >= 64 {
		for old := range s.seen {
			delete(s.seen, old)
			break
		}
	}
	s.seen[key] = struct{}{}
	return true
}

func (s *session) read(ctx context.Context, idle bool) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	deadline := time.Time{}
	if !idle {
		deadline = time.Now().Add(commandWait)
	}
	if d, ok := ctx.Deadline(); ok && (deadline.IsZero() || d.Before(deadline)) {
		deadline = d
	}
	setDeadline(s.rw, deadline)
	frame, err := readFrame(s.rw, markFromRadio)
	setDeadline(s.rw, time.Time{})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return frame, nil
}

func setDeadline(rw io.ReadWriteCloser, at time.Time) {
	if d, ok := rw.(deadliner); ok {
		_ = d.SetReadDeadline(at)
	}
}

// Run opens the companion serial port and redials it until ctx is done.
// Failures are logged without the device path.
func Run(ctx context.Context, device string, st *store.Store, name string) {
	if ctx == nil {
		ctx = context.Background()
	}
	ann := &announcer{}
	var last string
	note := func(msg string) {
		if msg == last {
			return
		}
		last = msg
		log.Printf("meshcore: %s", msg)
	}
	for {
		if ctx.Err() != nil {
			return
		}
		port, err := Open(device)
		if err != nil {
			note(publicErr(err))
			if !sleep(ctx, 2*time.Second) {
				return
			}
			continue
		}
		note("companion radio is connected")
		stop := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = port.Close()
			case <-stop:
			}
		}()
		err = Session(ctx, port, st, name, ann)
		close(stop)
		_ = port.Close()
		if ctx.Err() != nil {
			return
		}
		note(publicErr(err))
		if !sleep(ctx, 2*time.Second) {
			return
		}
	}
}

func publicErr(err error) string {
	if err == nil || errors.Is(err, ErrDown) {
		return ErrDown.Error()
	}
	if errors.Is(err, ErrDenied) || errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return ErrDenied.Error()
	}
	if errors.Is(err, ErrBusy) {
		return ErrBusy.Error()
	}
	return ErrDown.Error()
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
