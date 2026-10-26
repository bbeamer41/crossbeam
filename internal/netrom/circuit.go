// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package netrom routes a connection across AX.25 neighbors.
// Frames, qualities, and node broadcasts follow the published NET/ROM 1.3
// rules. The bytes of a circuit are the user's text. The network header
// is the routing envelope around that text.
package netrom

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
)

const (
	stConnecting = iota + 1
	stUp
	stClosing
	stDead

	maxBuf = 64 * 1024
)

// Config is one node's routing and circuit limits. Zero values use the
// published defaults.
type Config struct {
	Local           ax25.Address
	PathQuality     int
	MinQuality      int
	Obsolescence    int
	MinBroadcast    int
	MaxDestinations int
	TTL             int
	Window          int
	MaxCircuits     int
	Payload         int
	AckDelay        time.Duration
	Timeout         time.Duration
	Tries           int
	Idle            time.Duration
	Busy            time.Duration
	Send            func(ax25.Address, []byte) error
	Accept          func(ax25.Address, io.ReadWriteCloser)
	Retain          func(context.Context, ax25.Address) error
	Release         func(ax25.Address)
}

// Router is the destination list and the circuits that end here.
type Router struct {
	local        ax25.Address
	alias        [6]byte
	pathQuality  int
	minQuality   int
	obsInit      int
	minBroadcast int
	maxDest      int
	ttl          byte
	window       int
	maxCircuits  int
	payload      int
	ackDelay     time.Duration
	timeout      time.Duration
	tries        int
	idle         time.Duration
	busy         time.Duration
	tickEvery    time.Duration
	send         func(ax25.Address, []byte) error
	accept       func(ax25.Address, io.ReadWriteCloser)
	retain       func(context.Context, ax25.Address) error
	release      func(ax25.Address)

	mu        sync.Mutex
	neighbors map[string]ax25.Address
	dests     map[string]*destination
	circuits  map[uint16]*circuit
	byPeer    map[string]*circuit
	serial    byte
	quit      chan struct{}
	stopped   bool
}

type circuit struct {
	origin     bool
	state      int
	localIndex byte
	localID    byte
	peerIndex  byte
	peerID     byte
	peerName   string
	far        ax25.Address
	user       ax25.Address
	via        ax25.Address
	window     int
	retained   bool
	syn        []byte

	vs          byte
	va          byte
	vr          byte
	acked       byte
	unacked     []outFrame
	pending     []outFrame
	early       map[byte][]byte
	tries       int
	last        time.Time
	ackDue      time.Time
	nak         bool
	remoteChoke bool
	chokeSince  time.Time
	active      time.Time

	rbuf      []byte
	err       error
	cond      *sync.Cond
	ready     chan struct{}
	readyOnce sync.Once
	fail      error
}

type outFrame struct {
	seq  byte
	data []byte
	more bool
}

type pipe struct {
	r *Router
	c *circuit
}

// New starts the circuit timers.
func New(cfg Config) *Router {
	if cfg.PathQuality < 1 || cfg.PathQuality > 255 {
		cfg.PathQuality = DefaultPathQuality
	}
	if cfg.MinQuality < 1 || cfg.MinQuality > 255 {
		cfg.MinQuality = DefaultMinQuality
	}
	if cfg.Obsolescence < 1 || cfg.Obsolescence > 255 {
		cfg.Obsolescence = DefaultObsolescence
	}
	if cfg.MinBroadcast < 1 || cfg.MinBroadcast > 255 {
		cfg.MinBroadcast = DefaultMinBroadcast
	}
	if cfg.MaxDestinations < 1 {
		cfg.MaxDestinations = DefaultMaxDestination
	}
	if cfg.TTL < 1 || cfg.TTL > 255 {
		cfg.TTL = DefaultTTL
	}
	if cfg.Window < 1 || cfg.Window > 127 {
		cfg.Window = 4
	}
	if cfg.MaxCircuits < 1 {
		cfg.MaxCircuits = 20
	}
	if cfg.Payload < 1 || cfg.Payload > MaxInfo {
		cfg.Payload = MaxInfo
	}
	if cfg.AckDelay <= 0 {
		cfg.AckDelay = 3 * time.Second
	}
	if cfg.Timeout < 5*time.Millisecond {
		cfg.Timeout = 60 * time.Second
	}
	if cfg.Tries < 2 {
		cfg.Tries = 3
	}
	if cfg.Idle <= 0 {
		cfg.Idle = 15 * time.Minute
	}
	if cfg.Busy <= 0 {
		cfg.Busy = 180 * time.Second
	}
	every := 50 * time.Millisecond
	if cfg.AckDelay < every {
		every = cfg.AckDelay / 2
		if every < 5*time.Millisecond {
			every = 5 * time.Millisecond
		}
	}
	r := &Router{
		local:        cfg.Local,
		alias:        Alias(cfg.Local.Call),
		pathQuality:  cfg.PathQuality,
		minQuality:   cfg.MinQuality,
		obsInit:      cfg.Obsolescence,
		minBroadcast: cfg.MinBroadcast,
		maxDest:      cfg.MaxDestinations,
		ttl:          byte(cfg.TTL),
		window:       cfg.Window,
		maxCircuits:  cfg.MaxCircuits,
		payload:      cfg.Payload,
		ackDelay:     cfg.AckDelay,
		timeout:      cfg.Timeout,
		tries:        cfg.Tries,
		idle:         cfg.Idle,
		busy:         cfg.Busy,
		tickEvery:    every,
		send:         cfg.Send,
		accept:       cfg.Accept,
		retain:       cfg.Retain,
		release:      cfg.Release,
		neighbors:    map[string]ax25.Address{},
		dests:        map[string]*destination{},
		circuits:     map[uint16]*circuit{},
		byPeer:       map[string]*circuit{},
		quit:         make(chan struct{}),
	}
	go r.loop()
	return r
}

// Close stops timers and open circuits.
func (r *Router) Close() {
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.stopped = true
	close(r.quit)
	list := make([]*circuit, 0, len(r.circuits))
	for _, c := range r.circuits {
		list = append(list, c)
	}
	r.mu.Unlock()
	for _, c := range list {
		r.finish(c, errors.New("link is closed"))
	}
}

func (r *Router) loop() {
	tick := time.NewTicker(r.tickEvery)
	defer tick.Stop()
	for {
		select {
		case <-r.quit:
			return
		case <-tick.C:
			r.onTick(time.Now())
		}
	}
}

// Deliver handles one PID CF information field received from an adjacent node.
// Frames from a station that is not a listed neighbor are ignored, so a guest
// cannot use this node as a relay or open a circuit in someone else's name.
func (r *Router) Deliver(from ax25.Address, frame []byte) {
	if len(frame) < headerLen {
		return
	}
	origin, _, err := ax25.DecodeAddress(frame[0:7])
	if err != nil {
		return
	}
	dest, _, err := ax25.DecodeAddress(frame[7:14])
	if err != nil {
		return
	}
	if frame[14] == 0 || origin.Equal(r.local) {
		return
	}
	r.mu.Lock()
	_, neighbor := r.neighbors[from.String()]
	stopped := r.stopped
	r.mu.Unlock()
	if stopped || !neighbor {
		return
	}
	if !dest.Equal(r.local) {
		r.relay(from, dest, frame)
		return
	}
	msg, err := Decode(frame)
	if err != nil {
		return
	}
	switch msg.Op {
	case OpConnect:
		r.onConnect(from, msg)
	case OpAccept:
		r.onAccept(from, msg)
	case OpDisc:
		r.onDisc(from, msg)
	case OpDiscAck:
		r.onDiscAck(from, msg)
	case OpInfo:
		r.onInfo(from, msg)
	case OpAck:
		r.onAck(from, msg)
	}
}

func (r *Router) relay(from, dest ax25.Address, frame []byte) {
	if frame[14] <= 1 {
		return
	}
	r.mu.Lock()
	via, quality, ok := r.bestLocked(dest)
	send := r.send
	stopped := r.stopped
	r.mu.Unlock()
	if stopped || !ok || quality < 1 || via.Equal(from) || send == nil {
		return
	}
	out := append([]byte(nil), frame...)
	out[14]--
	_ = send(via, out)
}

// Connect opens a circuit to a node that is not necessarily a neighbor.
// The caller reads and writes the remote terminal.
func (r *Router) Connect(ctx context.Context, dest ax25.Address) (io.ReadWriteCloser, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if dest.Call == "" || dest.Equal(r.local) {
		return nil, fmt.Errorf("%s is not listed", dest)
	}
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return nil, errors.New("link is closed")
	}
	via, quality, ok := r.bestLocked(dest)
	if !ok || quality < 1 {
		r.mu.Unlock()
		return nil, fmt.Errorf("%s is not listed", dest)
	}
	index, id, ok := r.allocLocked()
	if !ok {
		r.mu.Unlock()
		return nil, errors.New("too many connections")
	}
	c := r.newCircuit(index, id, dest, via, r.window)
	c.origin = true
	retain := r.retain
	r.mu.Unlock()

	if retain != nil {
		if err := retain(ctx, via); err != nil {
			r.finish(c, err)
			return nil, err
		}
		r.mu.Lock()
		c.retained = true
		r.mu.Unlock()
	}
	if err := r.sendConnect(c); err != nil {
		r.finish(c, err)
		return nil, err
	}
	select {
	case <-ctx.Done():
		r.finish(c, ctx.Err())
		return nil, ctx.Err()
	case <-c.ready:
	}
	r.mu.Lock()
	state := c.state
	fail := c.fail
	r.mu.Unlock()
	if state != stUp {
		if fail == nil {
			fail = errors.New("the connection failed")
		}
		return nil, fail
	}
	return &pipe{r: r, c: c}, nil
}

func (r *Router) onConnect(from ax25.Address, m Message) {
	if m.Node.Call == "" || !m.Node.Equal(m.Origin) || m.Window < 1 || m.Window > 127 {
		r.refuse(from, m)
		return
	}
	key := peerKey(m.Node, m.Index, m.ID)
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	if c := r.byPeer[key]; c != nil && len(c.syn) > 0 {
		if !from.Equal(c.via) {
			r.mu.Unlock()
			return
		}
		ack := append([]byte(nil), c.syn...)
		via := c.via
		send := r.send
		r.mu.Unlock()
		if send != nil {
			_ = send(via, ack)
		}
		return
	}
	index, id, ok := r.allocLocked()
	if !ok {
		r.mu.Unlock()
		r.refuse(from, m)
		return
	}
	window := int(m.Window)
	if window > r.window {
		window = r.window
	}
	c := r.newCircuit(index, id, m.Origin, from, window)
	c.state = stUp
	c.user = m.User
	c.peerIndex = m.Index
	c.peerID = m.ID
	c.peerName = key
	c.active = time.Now()
	r.byPeer[key] = c
	ack, err := Encode(Message{
		Origin: r.local,
		Dest:   m.Origin,
		TTL:    r.ttl,
		Index:  m.Index,
		ID:     m.ID,
		Tx:     index,
		Rx:     id,
		Op:     OpAccept,
		Window: byte(window),
	})
	if err != nil {
		r.mu.Unlock()
		r.finish(c, errors.New("the connection failed"))
		return
	}
	c.syn = ack
	send := r.send
	accept := r.accept
	user := m.User
	r.mu.Unlock()
	if send != nil {
		_ = send(from, ack)
	}
	if accept != nil {
		go accept(user, &pipe{r: r, c: c})
	}
}

func (r *Router) refuse(from ax25.Address, m Message) {
	if r.send == nil || m.Origin.Call == "" {
		return
	}
	ack, err := Encode(Message{
		Origin: r.local,
		Dest:   m.Origin,
		TTL:    r.ttl,
		Index:  m.Index,
		ID:     m.ID,
		Op:     OpAccept,
		Choke:  true,
	})
	if err != nil {
		return
	}
	_ = r.send(from, ack)
}

func (r *Router) onAccept(from ax25.Address, m Message) {
	r.mu.Lock()
	c := r.circuits[localKey(m.Index, m.ID)]
	if c == nil || !c.origin || c.state != stConnecting || !arrives(c, from, m) {
		r.mu.Unlock()
		return
	}
	if m.Choke || m.Window < 1 || m.Window > 127 {
		r.mu.Unlock()
		r.finish(c, fmt.Errorf("%s refused the connection", c.far))
		return
	}
	window := int(m.Window)
	if window > c.window {
		window = c.window
	}
	c.window = window
	c.peerIndex = m.Tx
	c.peerID = m.Rx
	c.state = stUp
	c.active = time.Now()
	c.fail = nil
	c.readyOnce.Do(func() { close(c.ready) })
	r.mu.Unlock()
}

func (r *Router) onInfo(from ax25.Address, m Message) {
	r.mu.Lock()
	c := r.circuitLocked(m.Index, m.ID)
	if !arrives(c, from, m) {
		r.mu.Unlock()
		return
	}
	r.applyAckLocked(c, m.Rx, m.Choke)
	r.takeInfoLocked(c, m.Tx, m.Info)
	if m.NAK {
		c.last = time.Time{}
	}
	r.mu.Unlock()
	r.flush(c)
}

func (r *Router) onAck(from ax25.Address, m Message) {
	r.mu.Lock()
	c := r.circuitLocked(m.Index, m.ID)
	if !arrives(c, from, m) {
		r.mu.Unlock()
		return
	}
	r.applyAckLocked(c, m.Rx, m.Choke)
	if m.NAK {
		c.last = time.Time{}
	}
	r.mu.Unlock()
	r.flush(c)
}

func (r *Router) onDisc(from ax25.Address, m Message) {
	r.mu.Lock()
	c := r.circuitLocked(m.Index, m.ID)
	if !arrives(c, from, m) {
		r.mu.Unlock()
		return
	}
	frame, err := Encode(Message{
		Origin: r.local,
		Dest:   c.far,
		TTL:    r.ttl,
		Index:  c.peerIndex,
		ID:     c.peerID,
		Op:     OpDiscAck,
	})
	via := c.via
	send := r.send
	r.mu.Unlock()
	// Acknowledge before the crosslink is released. A send after release
	// finds the session gone and would dial the neighbor again.
	if err == nil && send != nil {
		_ = send(via, frame)
	}
	r.finish(c, io.EOF)
}

func (r *Router) onDiscAck(from ax25.Address, m Message) {
	r.mu.Lock()
	c := r.circuits[localKey(m.Index, m.ID)]
	if c == nil || (c.state != stClosing && c.state != stUp) || !arrives(c, from, m) {
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()
	r.finish(c, io.EOF)
}

// arrives reports that the frame came from the neighbor this circuit uses
// and names the station at the other end. Another neighbor that guesses
// the circuit index cannot write into it.
func arrives(c *circuit, from ax25.Address, m Message) bool {
	return c != nil && from.Equal(c.via) && m.Origin.Equal(c.far)
}

func (r *Router) circuitLocked(index, id byte) *circuit {
	c := r.circuits[localKey(index, id)]
	if c == nil || (c.state != stUp && c.state != stClosing) {
		return nil
	}
	return c
}

func (r *Router) applyAckLocked(c *circuit, nr byte, choke bool) {
	if ahead(nr, c.va) > ahead(c.vs, c.va) {
		return
	}
	for c.va != nr && len(c.unacked) > 0 {
		c.unacked = c.unacked[1:]
		c.va++
	}
	if c.va == c.vs {
		c.tries = 0
	}
	if choke {
		if !c.remoteChoke {
			c.chokeSince = time.Now()
		}
		c.remoteChoke = true
	} else {
		c.remoteChoke = false
	}
	c.cond.Broadcast()
}

func (r *Router) takeInfoLocked(c *circuit, seq byte, data []byte) {
	if c.state != stUp {
		return
	}
	if seq == c.vr {
		if !r.appendLocked(c, data) {
			c.nak = true
			c.ackDue = time.Now()
			return
		}
		c.vr++
		for {
			nxt, ok := c.early[c.vr]
			if !ok {
				break
			}
			if !r.appendLocked(c, nxt) {
				c.nak = true
				c.ackDue = time.Now()
				break
			}
			delete(c.early, c.vr)
			c.vr++
		}
		c.active = time.Now()
		if c.ackDue.IsZero() {
			c.ackDue = time.Now().Add(r.ackDelay)
		}
		c.cond.Broadcast()
		return
	}
	if ahead(seq, c.vr) < c.window {
		if _, ok := c.early[seq]; !ok && len(c.early) < c.window {
			c.early[seq] = append([]byte(nil), data...)
		}
		c.nak = true
		c.ackDue = time.Now()
		return
	}
	if ahead(c.vr, seq) > 0 && ahead(c.vr, seq) <= c.window && c.ackDue.IsZero() {
		c.ackDue = time.Now().Add(r.ackDelay)
	}
}

func (r *Router) appendLocked(c *circuit, data []byte) bool {
	if len(c.rbuf)+len(data) > maxBuf {
		return false
	}
	c.rbuf = append(c.rbuf, data...)
	return true
}

func (r *Router) sendConnect(c *circuit) error {
	r.mu.Lock()
	if r.stopped || c.state == stDead {
		r.mu.Unlock()
		return errors.New("link is closed")
	}
	frame, err := Encode(Message{
		Origin: r.local,
		Dest:   c.far,
		TTL:    r.ttl,
		Index:  c.localIndex,
		ID:     c.localID,
		Op:     OpConnect,
		Window: byte(r.window),
		User:   r.local,
		Node:   r.local,
	})
	if err != nil {
		r.mu.Unlock()
		return err
	}
	if c.tries < 1 {
		c.tries = 1
	}
	c.last = time.Now()
	via := c.via
	send := r.send
	r.mu.Unlock()
	if send == nil {
		return errors.New("the connection failed")
	}
	return send(via, frame)
}

func (r *Router) flush(c *circuit) {
	r.mu.Lock()
	frames, via := r.pullLocked(c, time.Now())
	send := r.send
	r.mu.Unlock()
	r.emit(send, via, frames)
}

func (r *Router) pullLocked(c *circuit, now time.Time) ([][]byte, ax25.Address) {
	if c.state != stUp {
		return nil, c.via
	}
	var frames [][]byte
	for len(c.pending) > 0 && ahead(c.vs, c.va) < c.window && !r.holding(c, now) {
		part := c.pending[0]
		c.pending = c.pending[1:]
		part.seq = c.vs
		c.vs++
		c.unacked = append(c.unacked, part)
		if len(c.unacked) == 1 {
			c.last = now
			c.tries = 1
		}
		frames = append(frames, r.infoFrame(c, part))
		c.acked = c.vr
		c.ackDue = time.Time{}
		c.nak = false
		c.active = now
	}
	c.cond.Broadcast()
	return frames, c.via
}

func (r *Router) infoFrame(c *circuit, part outFrame) []byte {
	frame, err := Encode(Message{
		Origin: r.local,
		Dest:   c.far,
		TTL:    r.ttl,
		Index:  c.peerIndex,
		ID:     c.peerID,
		Tx:     part.seq,
		Rx:     c.vr,
		Op:     OpInfo,
		More:   part.more,
		Choke:  len(c.rbuf) > r.payload*4,
		Info:   part.data,
	})
	if err != nil {
		return nil
	}
	return frame
}

func (r *Router) holding(c *circuit, now time.Time) bool {
	if !c.remoteChoke {
		return false
	}
	if !c.chokeSince.IsZero() && now.Sub(c.chokeSince) >= r.busy {
		c.remoteChoke = false
		return false
	}
	return true
}

func (r *Router) emit(send func(ax25.Address, []byte) error, via ax25.Address, frames [][]byte) {
	if send == nil {
		return
	}
	for _, frame := range frames {
		if len(frame) == 0 {
			continue
		}
		_ = send(via, frame)
	}
}

func (r *Router) onTick(now time.Time) {
	r.mu.Lock()
	list := make([]*circuit, 0, len(r.circuits))
	for _, c := range r.circuits {
		list = append(list, c)
	}
	r.mu.Unlock()
	for _, c := range list {
		r.service(c, now)
	}
}

func (r *Router) service(c *circuit, now time.Time) {
	r.mu.Lock()
	if c.state == stDead {
		r.mu.Unlock()
		return
	}
	if c.state == stConnecting && !c.last.IsZero() && now.Sub(c.last) >= r.timeout {
		if c.tries >= r.tries {
			r.mu.Unlock()
			r.finish(c, errors.New("connection timed out"))
			return
		}
		c.tries++
		r.mu.Unlock()
		_ = r.sendConnect(c)
		return
	}
	if c.state == stUp && len(c.unacked) > 0 && (c.last.IsZero() || now.Sub(c.last) >= r.timeout) {
		if !c.last.IsZero() && c.tries >= r.tries {
			r.mu.Unlock()
			r.finish(c, errors.New("connection timed out"))
			return
		}
		frames, via := r.resendLocked(c, now)
		send := r.send
		r.mu.Unlock()
		r.emit(send, via, frames)
		return
	}
	if c.state == stUp && !c.ackDue.IsZero() && !now.Before(c.ackDue) {
		frame, via, send := r.ackLocked(c)
		r.mu.Unlock()
		r.emit(send, via, [][]byte{frame})
		return
	}
	if c.state == stUp && len(c.unacked) == 0 && len(c.pending) == 0 && !c.active.IsZero() && now.Sub(c.active) >= r.idle {
		r.mu.Unlock()
		r.beginClose(c)
		return
	}
	if c.state == stClosing && now.Sub(c.last) >= r.timeout {
		if c.tries >= r.tries {
			r.mu.Unlock()
			r.finish(c, io.EOF)
			return
		}
		frame, via, send := r.discLocked(c, now)
		r.mu.Unlock()
		r.emit(send, via, [][]byte{frame})
		return
	}
	frames, via := r.pullLocked(c, now)
	send := r.send
	r.mu.Unlock()
	r.emit(send, via, frames)
}

func (r *Router) resendLocked(c *circuit, now time.Time) ([][]byte, ax25.Address) {
	c.tries++
	c.last = now
	frames := make([][]byte, 0, len(c.unacked))
	for _, part := range c.unacked {
		frames = append(frames, r.infoFrame(c, part))
	}
	return frames, c.via
}

func (r *Router) ackLocked(c *circuit) ([]byte, ax25.Address, func(ax25.Address, []byte) error) {
	nak := c.nak
	c.nak = false
	c.acked = c.vr
	c.ackDue = time.Time{}
	frame, err := Encode(Message{
		Origin: r.local,
		Dest:   c.far,
		TTL:    r.ttl,
		Index:  c.peerIndex,
		ID:     c.peerID,
		Rx:     c.vr,
		Op:     OpAck,
		NAK:    nak,
		Choke:  len(c.rbuf) > r.payload*4,
	})
	if err != nil {
		frame = nil
	}
	return frame, c.via, r.send
}

func (r *Router) discLocked(c *circuit, now time.Time) ([]byte, ax25.Address, func(ax25.Address, []byte) error) {
	c.tries++
	c.last = now
	frame, err := Encode(Message{
		Origin: r.local,
		Dest:   c.far,
		TTL:    r.ttl,
		Index:  c.peerIndex,
		ID:     c.peerID,
		Op:     OpDisc,
	})
	if err != nil {
		frame = nil
	}
	return frame, c.via, r.send
}

func (r *Router) beginClose(c *circuit) {
	r.mu.Lock()
	if c.state == stDead || c.state == stClosing {
		r.mu.Unlock()
		return
	}
	if c.state == stConnecting {
		r.mu.Unlock()
		r.finish(c, io.EOF)
		return
	}
	c.state = stClosing
	if c.err == nil {
		c.err = io.EOF
	}
	c.cond.Broadcast()
	frame, via, send := r.discLocked(c, time.Now())
	// discLocked increments tries. The first disc should count as try 1.
	c.tries = 1
	r.mu.Unlock()
	r.emit(send, via, [][]byte{frame})
}

func (r *Router) finish(c *circuit, err error) {
	r.mu.Lock()
	if c.state == stDead {
		r.mu.Unlock()
		return
	}
	c.state = stDead
	if c.err == nil {
		c.err = err
	}
	if c.fail == nil {
		c.fail = err
	}
	delete(r.circuits, localKey(c.localIndex, c.localID))
	if c.peerName != "" {
		delete(r.byPeer, c.peerName)
	}
	c.cond.Broadcast()
	retained := c.retained
	c.retained = false
	via := c.via
	release := r.release
	c.readyOnce.Do(func() { close(c.ready) })
	r.mu.Unlock()
	if retained && release != nil {
		release(via)
	}
}

func (r *Router) newCircuit(index, id byte, far, via ax25.Address, window int) *circuit {
	c := &circuit{
		state:      stConnecting,
		localIndex: index,
		localID:    id,
		far:        far,
		via:        via,
		window:     window,
		ready:      make(chan struct{}),
		early:      map[byte][]byte{},
		active:     time.Now(),
	}
	c.cond = sync.NewCond(&r.mu)
	r.circuits[localKey(index, id)] = c
	return c
}

func (r *Router) allocLocked() (byte, byte, bool) {
	if len(r.circuits) >= r.maxCircuits {
		return 0, 0, false
	}
	used := make(map[byte]bool, len(r.circuits))
	for _, c := range r.circuits {
		used[c.localIndex] = true
	}
	var index byte
	for i := 1; i <= 255; i++ {
		if !used[byte(i)] {
			index = byte(i)
			break
		}
	}
	if index == 0 {
		return 0, 0, false
	}
	for n := 0; n < 255; n++ {
		r.serial++
		if r.serial == 0 {
			r.serial = 1
		}
		if _, exists := r.circuits[localKey(index, r.serial)]; !exists {
			return index, r.serial, true
		}
	}
	return 0, 0, false
}

func (p *pipe) Read(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	p.r.mu.Lock()
	defer p.r.mu.Unlock()
	c := p.c
	for len(c.rbuf) == 0 && c.err == nil && (c.state == stUp || c.state == stClosing || c.state == stConnecting) {
		c.cond.Wait()
	}
	if len(c.rbuf) == 0 {
		if c.err != nil {
			return 0, c.err
		}
		return 0, io.EOF
	}
	n := copy(b, c.rbuf)
	c.rbuf = c.rbuf[n:]
	if len(c.rbuf) == 0 {
		c.rbuf = nil
	}
	if len(c.rbuf) <= p.r.payload*2 && c.ackDue.IsZero() {
		c.ackDue = time.Now()
	}
	return n, nil
}

func (p *pipe) Write(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	off := 0
	for off < len(b) {
		p.r.mu.Lock()
		c := p.c
		for c.state == stUp && len(c.pending) >= 32 && c.err == nil {
			c.cond.Wait()
		}
		if c.state != stUp || c.err != nil {
			err := c.err
			p.r.mu.Unlock()
			if off > 0 {
				return off, nil
			}
			if err == nil {
				err = io.ErrClosedPipe
			}
			return 0, err
		}
		n := p.r.payload
		if n > len(b)-off {
			n = len(b) - off
		}
		c.pending = append(c.pending, outFrame{
			data: append([]byte(nil), b[off:off+n]...),
			more: off+n < len(b),
		})
		off += n
		p.r.mu.Unlock()
		p.r.flush(c)
	}
	return len(b), nil
}

func (p *pipe) Close() error {
	p.r.beginClose(p.c)
	return nil
}

func ahead(seq, base byte) int {
	return int(seq - base)
}

func localKey(index, id byte) uint16 {
	return uint16(index)<<8 | uint16(id)
}

func peerKey(node ax25.Address, index, id byte) string {
	return fmt.Sprintf("%s/%d/%d", node.String(), index, id)
}
