// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ax25

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

const (
	DefaultPaclen = 256
	DefaultWindow = 4
	DefaultN2     = 10
	DefaultT1     = 5 * time.Second
	DefaultIdle   = 15 * time.Minute
	MaxSessions   = 32
	maxRead       = 64 * 1024
	maxWrite      = 64 * 1024
)

// ErrClosed is returned after the switch has stopped.
var ErrClosed = errors.New("link is closed")

// ErrNotConnected is returned when a NET/ROM frame has no open session
// to that neighbor.
var ErrNotConnected = errors.New("not connected")

// Switch is one station's modulo-8 connections. Acknowledgements are sent
// as soon as a frame is accepted. T1 stays at several seconds so a peer that
// delays its own acknowledgement, the way BPQ's RESPTIME does, still fits.
type Switch struct {
	local   Address
	paclen  int
	window  int
	n2      int
	t1      time.Duration
	send    func(Address, Frame)
	accept  func(Address, *Stream)
	netrom  func(Address, []byte)
	onBegin func(Address)
	onEnd   func(Address)
	idle    time.Duration
	events  chan event
	quit    chan struct{}
	done    chan struct{}
	once    sync.Once
	conns   map[string]*conn
	now     func() time.Time
}

// Config is the station and the timers for a switch.
type Config struct {
	Local  Address
	Paclen int
	Window int
	N2     int
	T1     time.Duration
	Send   func(Address, Frame)
	Accept func(Address, *Stream)
	// Netrom receives the information field of one PID CF I-frame.
	// It runs on the engine goroutine, in arrival order, and must
	// only enqueue the frame. It must not call this switch.
	Netrom func(Address, []byte)
	// Begin and End run on the engine goroutine and must not call the switch.
	Begin func(Address)
	End   func(Address)
	// Idle is how long a quiet session stays up. Zero uses DefaultIdle.
	Idle time.Duration
}

type phase int

const (
	phaseSetup phase = iota + 1
	phaseInfo
	phaseRelease
)

type dialResult struct {
	stream *Stream
	err    error
}

type event struct {
	kind    int
	frame   Frame
	remote  Address
	result  chan dialResult
	stream  *Stream
	done    chan struct{}
	err     error
	payload []byte
	note    chan error
}

const (
	evFrame = iota + 1
	evDial
	evClose
	evAbort
	evKick
	evNetrom
)

// slot is one sent I-frame. NET/ROM keeps its own PID so a retransmission
// is not turned into text.
type slot struct {
	pid  byte
	data []byte
}

type conn struct {
	remote    Address
	phase     phase
	vs        int
	vr        int
	va        int
	sent      [8]slot
	netq      [][]byte
	hold      []byte
	retries   int
	deadline  time.Time
	poll      bool
	peerBusy  bool
	localBusy bool
	rejecting bool
	needAck   bool
	closing   bool
	touched   time.Time
	stream    *Stream
	waiter    chan dialResult
	done      chan struct{}
}

// Stream is the bytes of one AX.25 connection.
type Stream struct {
	sw     *Switch
	remote Address
	mu     sync.Mutex
	cond   *sync.Cond
	rbuf   []byte
	wbuf   []byte
	err    error
	once   sync.Once
}

// NewSwitch starts the connection engine. Send must not call back into the
// switch. It may queue the frame and return.
func NewSwitch(cfg Config) *Switch {
	if cfg.Paclen < 1 || cfg.Paclen > DefaultPaclen {
		cfg.Paclen = DefaultPaclen
	}
	if cfg.Window < 1 || cfg.Window > 7 {
		cfg.Window = DefaultWindow
	}
	if cfg.N2 < 1 {
		cfg.N2 = DefaultN2
	}
	if cfg.T1 <= 0 {
		cfg.T1 = DefaultT1
	}
	if cfg.Idle <= 0 {
		cfg.Idle = DefaultIdle
	}
	s := &Switch{
		local:   cfg.Local,
		paclen:  cfg.Paclen,
		window:  cfg.Window,
		n2:      cfg.N2,
		t1:      cfg.T1,
		idle:    cfg.Idle,
		send:    cfg.Send,
		accept:  cfg.Accept,
		netrom:  cfg.Netrom,
		onBegin: cfg.Begin,
		onEnd:   cfg.End,
		events:  make(chan event, 64),
		quit:    make(chan struct{}),
		done:    make(chan struct{}),
		conns:   map[string]*conn{},
		now:     time.Now,
	}
	go s.loop()
	return s
}

// Close stops the engine and unblocks every stream.
func (s *Switch) Close() {
	s.once.Do(func() { close(s.quit) })
	<-s.done
}

// Inject delivers one decoded frame to the engine.
func (s *Switch) Inject(frame Frame) {
	select {
	case s.events <- event{kind: evFrame, frame: frame}:
	case <-s.quit:
	}
}

// Dial opens a connection. The remote station must already be routable by Send.
func (s *Switch) Dial(ctx context.Context, remote Address) (*Stream, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	res := make(chan dialResult, 1)
	select {
	case s.events <- event{kind: evDial, remote: remote, result: res}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.quit:
		return nil, ErrClosed
	}
	select {
	case got := <-res:
		return got.stream, got.err
	case <-ctx.Done():
		return s.cancelDial(remote, res, ctx.Err())
	case <-s.quit:
		return nil, ErrClosed
	}
}

// cancelDial drops a setup that is still in progress. A connection that
// completed in the same moment is returned instead of being left open
// with nobody reading it.
func (s *Switch) cancelDial(remote Address, res chan dialResult, err error) (*Stream, error) {
	select {
	case got := <-res:
		return got.stream, got.err
	default:
	}
	select {
	case s.events <- event{kind: evAbort, remote: remote, err: err}:
	case got := <-res:
		return got.stream, got.err
	case <-s.quit:
		return nil, ErrClosed
	}
	select {
	case got := <-res:
		return got.stream, got.err
	case <-s.quit:
		return nil, ErrClosed
	}
}

// Read returns the next bytes from the peer. Buffered data is returned
// before a later disconnect.
func (st *Stream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	st.mu.Lock()
	for len(st.rbuf) == 0 && st.err == nil {
		st.cond.Wait()
	}
	if len(st.rbuf) == 0 {
		err := st.err
		st.mu.Unlock()
		return 0, err
	}
	before := len(st.rbuf)
	n := copy(p, st.rbuf)
	st.rbuf = compact(st.rbuf[n:])
	wake := before >= maxRead/2 && len(st.rbuf) < maxRead/2
	st.mu.Unlock()
	if wake {
		st.kick()
	}
	return n, nil
}

// Write queues bytes. It returns when they are queued, not when they are
// acknowledged. A full queue waits.
func (st *Stream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	st.mu.Lock()
	off := 0
	for off < len(p) {
		for len(st.wbuf) >= maxWrite && st.err == nil {
			st.cond.Wait()
		}
		if st.err != nil {
			st.mu.Unlock()
			if off > 0 {
				st.kick()
				return off, nil
			}
			return 0, st.err
		}
		n := maxWrite - len(st.wbuf)
		if n > len(p)-off {
			n = len(p) - off
		}
		st.wbuf = append(st.wbuf, p[off:off+n]...)
		off += n
	}
	st.mu.Unlock()
	st.kick()
	return off, nil
}

// Close disconnects the link.
func (st *Stream) Close() error {
	st.once.Do(func() {
		done := make(chan struct{})
		select {
		case st.sw.events <- event{kind: evClose, stream: st, done: done}:
			select {
			case <-done:
			case <-st.sw.quit:
			}
		case <-st.sw.quit:
		}
	})
	return nil
}

func (st *Stream) kick() {
	select {
	case st.sw.events <- event{kind: evKick, stream: st}:
	default:
	}
}

func (st *Stream) fail(err error) {
	st.mu.Lock()
	if st.err == nil {
		st.err = err
	}
	st.cond.Broadcast()
	st.mu.Unlock()
}

func (st *Stream) take(n int) []byte {
	st.mu.Lock()
	defer st.mu.Unlock()
	if n > len(st.wbuf) {
		n = len(st.wbuf)
	}
	if n == 0 {
		return nil
	}
	out := make([]byte, n)
	copy(out, st.wbuf[:n])
	st.wbuf = compact(st.wbuf[n:])
	st.cond.Signal()
	return out
}

func (st *Stream) deliver(b []byte) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.err != nil {
		return false
	}
	if len(b) > 0 && len(st.rbuf)+len(b) > maxRead {
		return false
	}
	if len(b) > 0 {
		st.rbuf = append(st.rbuf, b...)
		st.cond.Signal()
	}
	return true
}

func (st *Stream) hasRoom() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.rbuf) < maxRead/2
}

func (st *Stream) pending() bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return len(st.wbuf) > 0
}

func compact(b []byte) []byte {
	if cap(b) > 4096 && cap(b) > 4*len(b) {
		return append([]byte(nil), b...)
	}
	return b
}

func (s *Switch) loop() {
	defer close(s.done)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-s.quit:
			s.shutdown()
			return
		case ev := <-s.events:
			s.handle(ev)
			s.pumpAll()
		case <-tick.C:
			s.tick()
			s.pumpAll()
		}
	}
}

func (s *Switch) shutdown() {
	for _, c := range s.conns {
		s.end(c, ErrClosed)
	}
}

func (s *Switch) handle(ev event) {
	switch ev.kind {
	case evFrame:
		s.onFrame(ev.frame)
	case evDial:
		s.onDial(ev.remote, ev.result)
	case evClose:
		s.onClose(ev.stream, ev.done)
	case evAbort:
		s.onAbort(ev.remote, ev.err)
	case evKick:
		if ev.stream != nil {
			if c := s.conns[ev.stream.remote.String()]; c != nil {
				s.recoverBusy(c)
			}
		}
	case evNetrom:
		if ev.note != nil {
			ev.note <- s.enqueueNetrom(ev.remote, ev.payload)
		}
	}
}

// SendNetrom queues one PID CF information field on the session to remote.
// The session must already be up. The field is one I-frame and is not split
// across the text stream.
func (s *Switch) SendNetrom(remote Address, info []byte) error {
	if len(info) == 0 {
		return errors.New("frame is empty")
	}
	if len(info) > s.paclen {
		return errors.New("frame is too long")
	}
	note := make(chan error, 1)
	ev := event{kind: evNetrom, remote: remote, payload: append([]byte(nil), info...), note: note}
	select {
	case s.events <- ev:
	case <-s.quit:
		return ErrClosed
	}
	select {
	case err := <-note:
		return err
	case <-s.quit:
		return ErrClosed
	}
}

func (s *Switch) enqueueNetrom(remote Address, info []byte) error {
	c := s.conns[remote.String()]
	if c == nil || c.phase != phaseInfo || c.closing {
		return ErrNotConnected
	}
	if len(info) > s.paclen {
		return errors.New("frame is too long")
	}
	c.netq = append(c.netq, info)
	return nil
}

func (s *Switch) onDial(remote Address, result chan dialResult) {
	if remote.Equal(s.local) {
		result <- dialResult{err: errors.New("cannot connect to this station")}
		return
	}
	if c := s.conns[remote.String()]; c != nil {
		result <- dialResult{err: errors.New(remote.String() + " is already connected")}
		return
	}
	if len(s.conns) >= MaxSessions {
		result <- dialResult{err: errors.New("too many connections")}
		return
	}
	c := s.newConn(remote)
	c.phase = phaseSetup
	c.waiter = result
	s.conns[remote.String()] = c
	s.began(remote)
	s.sendU(remote, SABM, true, true)
	c.arm(s.now())
}

func (s *Switch) onAbort(remote Address, err error) {
	c := s.conns[remote.String()]
	if c == nil || c.phase != phaseSetup || c.waiter == nil {
		return
	}
	if err == nil {
		err = context.Canceled
	}
	s.end(c, err)
}

func (s *Switch) onClose(st *Stream, done chan struct{}) {
	if st == nil {
		if done != nil {
			close(done)
		}
		return
	}
	c := s.conns[st.remote.String()]
	if c == nil || c.stream != st {
		if done != nil {
			close(done)
		}
		return
	}
	if c.phase == phaseSetup {
		if done != nil {
			c.done = done
		}
		s.sendU(c.remote, DM, false, true)
		s.end(c, io.EOF)
		return
	}
	if c.phase == phaseRelease {
		if c.done == nil {
			c.done = done
		} else if done != nil {
			close(done)
		}
		return
	}
	if c.done == nil {
		c.done = done
	} else if done != nil {
		close(done)
	}
	// Stay in the information phase until queued text is acknowledged, then DISC.
	c.closing = true
	c.pump(s)
	s.maybeDisc(c)
}

func (s *Switch) onFrame(f Frame) {
	if !f.Dest.Equal(s.local) || f.Src.Equal(s.local) || len(f.Digis) > 0 || IsUI(f.Control) {
		return
	}
	c := s.conns[f.Src.String()]
	switch {
	case IsSABME(f.Control):
		s.sendU(f.Src, DM, false, PF(f.Control))
		if c != nil {
			s.end(c, errors.New("peer requested modulo 128"))
		}
	case IsSABM(f.Control):
		s.onSABM(f, c)
	case IsDISC(f.Control):
		s.sendU(f.Src, UA, false, PF(f.Control))
		if c != nil {
			s.end(c, io.EOF)
		}
	case IsUA(f.Control):
		s.onUA(c)
	case IsDM(f.Control):
		if c != nil {
			err := error(io.EOF)
			if c.phase == phaseSetup {
				err = errors.New("connection refused")
			}
			s.end(c, err)
		}
	case IsFRMR(f.Control):
		if c != nil {
			s.end(c, errors.New("frame rejected"))
		}
	case IsI(f.Control):
		if c == nil || c.phase != phaseInfo {
			s.sendU(f.Src, DM, false, PF(f.Control))
			return
		}
		s.onI(c, f)
	case IsS(f.Control):
		if c == nil || c.phase != phaseInfo {
			s.sendU(f.Src, DM, false, PF(f.Control))
			return
		}
		s.onS(c, f)
	default:
		if c == nil {
			s.sendU(f.Src, DM, false, PF(f.Control))
		}
	}
}

func (s *Switch) onSABM(f Frame, c *conn) {
	if c != nil && (c.phase == phaseRelease || c.closing) {
		s.sendU(f.Src, DM, false, PF(f.Control))
		return
	}
	if c == nil && len(s.conns) >= MaxSessions {
		s.sendU(f.Src, DM, false, PF(f.Control))
		return
	}
	fresh := c == nil
	if fresh {
		c = s.newConn(f.Src)
		s.conns[f.Src.String()] = c
		s.began(f.Src)
	}
	c.vs, c.vr, c.va = 0, 0, 0
	c.sent = [8]slot{}
	c.netq = nil
	c.hold = nil
	c.retries = 0
	c.poll = false
	c.peerBusy = false
	c.localBusy = false
	c.rejecting = false
	c.needAck = false
	c.closing = false
	c.phase = phaseInfo
	c.touched = s.now()
	c.stopT1()
	s.sendU(f.Src, UA, false, PF(f.Control))
	if c.waiter != nil {
		c.waiter <- dialResult{stream: c.stream}
		c.waiter = nil
		return
	}
	if fresh && s.accept != nil {
		go s.accept(c.remote, c.stream)
	}
}

func (s *Switch) onUA(c *conn) {
	if c == nil {
		return
	}
	switch c.phase {
	case phaseSetup:
		c.phase = phaseInfo
		c.retries = 0
		c.touched = s.now()
		c.stopT1()
		if c.waiter != nil {
			c.waiter <- dialResult{stream: c.stream}
			c.waiter = nil
		}
	case phaseRelease:
		s.end(c, io.EOF)
	}
}

func (s *Switch) onI(c *conn, f Frame) {
	if !c.ack(NR(f.Control), s.now()) {
		return
	}
	c.touched = s.now()
	must := f.Command && PF(f.Control)
	ns := NS(f.Control)
	if ns == c.vr {
		accepted := false
		if f.PID == PIDNetrom {
			// Routing frames share this connection and stay out of the
			// terminal byte stream. A full text buffer does not refuse them.
			accepted = true
			if s.netrom != nil {
				info := append([]byte(nil), f.Info...)
				s.netrom(c.remote, info)
			}
		} else if c.stream.deliver(f.Info) {
			accepted = true
			c.localBusy = false
		}
		if accepted {
			c.vr = (c.vr + 1) & 7
			c.rejecting = false
			c.needAck = true
		} else {
			c.localBusy = true
			s.sendS(c, RNR, false, must)
			must = false
			c.needAck = false
		}
	} else if duplicate(ns, c.vr, s.window) {
		c.needAck = true
	} else if !c.rejecting {
		s.sendS(c, REJ, false, must)
		c.rejecting = true
		must = false
		c.needAck = false
	}
	if must {
		s.ackNow(c, true)
	}
}

func (s *Switch) onS(c *conn, f Frame) {
	if !c.ack(NR(f.Control), s.now()) {
		return
	}
	c.touched = s.now()
	base := f.Control & 0x0F
	pf := PF(f.Control)
	response := !f.Command
	if f.Legacy && c.poll && pf {
		response = true
	}
	sentAgain := false
	switch base {
	case RNR:
		c.peerBusy = true
	case REJ:
		c.peerBusy = false
		s.retransmit(c)
		sentAgain = true
	default:
		c.peerBusy = false
	}
	if response && pf && c.poll {
		c.poll = false
		if !sentAgain {
			s.retransmit(c)
		}
	}
	if f.Command && pf {
		s.ackNow(c, true)
	}
}

func (s *Switch) ackNow(c *conn, final bool) {
	base := byte(RR)
	if c.localBusy {
		base = RNR
	}
	s.sendS(c, base, false, final)
	c.needAck = false
}

func (s *Switch) tick() {
	now := s.now()
	for _, c := range s.conns {
		if c.phase == phaseInfo && !c.closing && c.outstanding() == 0 && len(c.hold) == 0 && len(c.netq) == 0 && !c.stream.pending() && !c.touched.IsZero() && now.Sub(c.touched) > s.idle {
			c.closing = true
			c.retries = 0
			s.maybeDisc(c)
			continue
		}
		if c.deadline.IsZero() || now.Before(c.deadline) {
			continue
		}
		c.retries++
		if c.retries > s.n2 {
			err := errors.New("link failed")
			if c.phase == phaseSetup {
				err = errors.New("connection timed out")
			}
			s.end(c, err)
			continue
		}
		switch c.phase {
		case phaseSetup:
			s.sendU(c.remote, SABM, true, true)
		case phaseRelease:
			s.sendU(c.remote, DISC, true, true)
		case phaseInfo:
			s.sendS(c, RR, true, true)
			c.poll = true
		}
		c.arm(now)
	}
}

func (s *Switch) pumpAll() {
	for _, c := range s.conns {
		if c.phase != phaseInfo {
			continue
		}
		s.recoverBusy(c)
		if c.pump(s) && c.needAck {
			c.needAck = false
		}
		if c.needAck {
			s.ackNow(c, false)
		}
		s.maybeDisc(c)
	}
}

func (s *Switch) recoverBusy(c *conn) {
	if c.localBusy && c.stream.hasRoom() {
		c.localBusy = false
		s.ackNow(c, false)
	}
}

func (c *conn) pump(s *Switch) bool {
	if c.phase != phaseInfo || c.peerBusy {
		return false
	}
	sent := false
	for c.outstanding() < s.window {
		var payload []byte
		pid := PIDText
		if len(c.netq) > 0 {
			payload = c.netq[0]
			c.netq = c.netq[1:]
			pid = PIDNetrom
		} else {
			if len(c.hold) == 0 {
				c.hold = c.stream.take(s.paclen)
			}
			if len(c.hold) == 0 {
				break
			}
			n := s.paclen
			if n > len(c.hold) {
				n = len(c.hold)
			}
			payload = append([]byte(nil), c.hold[:n]...)
			c.hold = c.hold[n:]
		}
		c.sent[c.vs] = slot{pid: pid, data: payload}
		s.sendI(c, c.vs)
		c.vs = (c.vs + 1) & 7
		if c.deadline.IsZero() {
			c.arm(s.now())
		}
		c.touched = s.now()
		sent = true
	}
	return sent
}

func (s *Switch) maybeDisc(c *conn) {
	if !c.closing || c.phase != phaseInfo {
		return
	}
	if c.outstanding() == 0 && len(c.hold) == 0 && len(c.netq) == 0 && !c.stream.pending() {
		c.phase = phaseRelease
		c.closing = false
		c.retries = 0
		c.poll = false
		c.needAck = false
		s.sendU(c.remote, DISC, true, true)
		c.arm(s.now())
		return
	}
	if c.deadline.IsZero() {
		c.arm(s.now())
	}
}

func (c *conn) outstanding() int { return (c.vs - c.va) & 7 }

func (c *conn) ack(nr int, now time.Time) bool {
	if !seqOK(c.va, nr, c.vs) {
		return false
	}
	advanced := nr != c.va
	for c.va != nr {
		c.sent[c.va] = slot{}
		c.va = (c.va + 1) & 7
	}
	if c.va == c.vs {
		c.stopT1()
		c.poll = false
		c.retries = 0
	} else if advanced {
		c.retries = 0
		c.arm(now)
	}
	return true
}

func (s *Switch) retransmit(c *conn) {
	for ns := c.va; ns != c.vs; ns = (ns + 1) & 7 {
		s.sendI(c, ns)
	}
	if c.va != c.vs && c.deadline.IsZero() {
		c.arm(s.now())
	}
}

func seqOK(va, nr, vs int) bool {
	outstanding := (vs - va) & 7
	dist := (nr - va) & 7
	return dist <= outstanding
}

func duplicate(ns, vr, window int) bool {
	d := (vr - ns) & 7
	return d > 0 && d <= window
}

func (s *Switch) newConn(remote Address) *conn {
	st := &Stream{sw: s, remote: remote}
	st.cond = sync.NewCond(&st.mu)
	return &conn{remote: remote, stream: st}
}

func (s *Switch) began(remote Address) {
	if s.onBegin != nil {
		s.onBegin(remote)
	}
}

func (s *Switch) end(c *conn, err error) {
	if s.conns[c.remote.String()] != c {
		return
	}
	delete(s.conns, c.remote.String())
	s.finish(c, err)
	if s.onEnd != nil {
		s.onEnd(c.remote)
	}
}

func (s *Switch) finish(c *conn, err error) {
	if c.waiter != nil {
		c.waiter <- dialResult{err: err}
		c.waiter = nil
	}
	if c.done != nil {
		close(c.done)
		c.done = nil
	}
	c.stream.fail(err)
	c.stopT1()
}

func (c *conn) arm(now time.Time) { c.deadline = now.Add(c.t1()) }

func (c *conn) t1() time.Duration { return c.stream.sw.t1 }

func (c *conn) stopT1() { c.deadline = time.Time{} }

func (s *Switch) sendI(c *conn, ns int) {
	s.emit(c.remote, Frame{
		Dest:    c.remote,
		Src:     s.local,
		Command: true,
		Control: IControl(ns, c.vr, false),
		PID:     c.sent[ns].pid,
		Info:    c.sent[ns].data,
	})
}

func (s *Switch) sendS(c *conn, base byte, command, pf bool) {
	s.emit(c.remote, Frame{
		Dest:    c.remote,
		Src:     s.local,
		Command: command,
		Control: SControl(base, c.vr, pf),
	})
}

func (s *Switch) sendU(remote Address, base byte, command, pf bool) {
	control := base &^ 0x10
	if pf {
		control |= 0x10
	}
	s.emit(remote, Frame{
		Dest:    remote,
		Src:     s.local,
		Command: command,
		Control: control,
	})
}

func (s *Switch) emit(remote Address, f Frame) {
	if s.send != nil {
		s.send(remote, f)
	}
}
