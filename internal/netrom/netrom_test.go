// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package netrom

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
)

func TestQualityRoundsToTheNearestInteger(t *testing.T) {
	if got := Combine(192, 192); got != 144 {
		t.Fatalf("quality %d", got)
	}
	if got := Combine(0, 192); got != 0 {
		t.Fatalf("zero %d", got)
	}
}

func TestConnectFrameUsesThePublishedHeader(t *testing.T) {
	origin := mustCall(t, "K5LEB")
	dest := mustCall(t, "W1AW-1")
	frame, err := Encode(Message{
		Origin: origin,
		Dest:   dest,
		TTL:    64,
		Index:  1,
		ID:     2,
		Op:     OpConnect,
		Window: 4,
		User:   origin,
		Node:   origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) != 35 || frame[14] != 64 || frame[19] != OpConnect || frame[20] != 4 {
		t.Fatalf("header %d %x", len(frame), frame[:21])
	}
	if frame[13]&0x01 != 1 || frame[6]&0x01 != 0 {
		t.Fatalf("extension bits %02x %02x", frame[6], frame[13])
	}
	back, err := Decode(frame)
	if err != nil || !back.Origin.Equal(origin) || !back.Dest.Equal(dest) || back.Window != 4 || !back.User.Equal(origin) {
		t.Fatalf("decode %+v %v", back, err)
	}
	if _, err := Decode([]byte{0x01}); err == nil {
		t.Fatal("short frame")
	}
}

func TestRoutesFollowBroadcastsAndDropLoops(t *testing.T) {
	a := mustCall(t, "K5LEB")
	b := mustCall(t, "W1AW-1")
	c := mustCall(t, "W5BBS")
	n1 := mustCall(t, "N1AAA")
	n2 := mustCall(t, "N2BBB")
	n3 := mustCall(t, "N3CCC")
	n4 := mustCall(t, "N4DDD")
	far := mustCall(t, "K0FAR")
	r := newTestRouter(t, a, nil, nil)
	r.SetNeighbors([]ax25.Address{b})

	if _, _, ok := r.Lookup(c); ok {
		t.Fatal("route before a broadcast")
	}
	r.Observe(n1, EncodeBroadcast(Alias("N1AAA"), []Record{{
		Call: c, Alias: Alias("W5BBS"), Neighbor: b, Quality: 200,
	}})[0])
	if _, _, ok := r.Lookup(c); ok {
		t.Fatal("unlisted broadcast installed a route")
	}

	loop := EncodeBroadcast(Alias("W1AW"), []Record{{
		Call: c, Alias: Alias("W5BBS"), Neighbor: a, Quality: 200,
	}})
	r.Observe(b, loop[0])
	if _, _, ok := r.Lookup(c); ok {
		t.Fatal("loop installed")
	}

	good := EncodeBroadcast(Alias("W1AW"), []Record{
		{Call: a, Alias: Alias("K5LEB"), Neighbor: b, Quality: 255},
		{Call: c, Alias: Alias("W5BBS"), Neighbor: n1, Quality: 192},
		{Call: b, Alias: Alias("W1AW"), Neighbor: b, Quality: 192},
	})
	r.Observe(b, good[0])
	via, quality, ok := r.Lookup(c)
	if !ok || !via.Equal(b) || quality != 144 {
		t.Fatalf("route %s %d %v", via, quality, ok)
	}
	if _, _, ok := r.Lookup(a); ok {
		t.Fatal("route to self")
	}

	r.SetNeighbors([]ax25.Address{n1, n2, n3, n4})
	offer := []struct {
		from    ax25.Address
		quality byte
	}{
		{n1, 200},
		{n2, 180},
		{n3, 100},
		{n4, 50},
	}
	for _, item := range offer {
		body := EncodeBroadcast(Alias(item.from.Call), []Record{{
			Call: far, Alias: Alias("K0FAR"), Neighbor: item.from, Quality: item.quality,
		}})
		r.Observe(item.from, body[0])
	}
	via, _, ok = r.Lookup(far)
	if !ok || !via.Equal(n1) {
		t.Fatalf("best %s", via)
	}
	r.SetNeighbors([]ax25.Address{n4})
	if _, _, ok := r.Lookup(far); ok {
		t.Fatal("fourth route was kept")
	}
}

func TestLearnedRouteAgesOutAndIsNotBroadcastEarly(t *testing.T) {
	a := mustCall(t, "K5LEB")
	b := mustCall(t, "W1AW-1")
	c := mustCall(t, "W5BBS")
	r := newTestRouter(t, a, nil, nil)
	r.SetNeighbors([]ax25.Address{b})
	body := EncodeBroadcast(Alias("W1AW"), []Record{{
		Call: c, Alias: Alias("W5BBS"), Neighbor: b, Quality: 192,
	}})
	r.Observe(b, body[0])
	for i := 0; i < 2; i++ {
		r.Age()
	}
	found := false
	for _, payload := range r.Payloads() {
		got, ok := DecodeBroadcast(payload)
		if !ok {
			t.Fatal("payload")
		}
		for _, rec := range got.Dests {
			if rec.Call.Equal(c) {
				found = true
			}
			if rec.Quality == 0 {
				t.Fatal("quality zero was broadcast")
			}
		}
	}
	if found {
		t.Fatal("stale route was broadcast")
	}
	for i := 0; i < 4; i++ {
		r.Age()
	}
	if _, _, ok := r.Lookup(c); ok {
		t.Fatal("aged route remains")
	}
	if _, quality, ok := r.Lookup(b); !ok || quality != DefaultPathQuality {
		t.Fatalf("neighbor %d %v", quality, ok)
	}
}

func TestBroadcastPacksElevenDestinations(t *testing.T) {
	a := mustCall(t, "K5LEB")
	b := mustCall(t, "W1AW-1")
	r := newTestRouter(t, a, nil, nil)
	r.SetNeighbors([]ax25.Address{b})
	var recs []Record
	for i := 0; i < 12; i++ {
		call := mustCall(t, string(rune('A'+i))+"1ONE")
		recs = append(recs, Record{Call: call, Alias: Alias(call.Call), Neighbor: b, Quality: 200})
	}
	for _, part := range EncodeBroadcast(Alias("W1AW"), recs) {
		r.Observe(b, part)
	}
	payloads := r.Payloads()
	if len(payloads) != 2 {
		t.Fatalf("frames %d", len(payloads))
	}
	first, ok := DecodeBroadcast(payloads[0])
	if !ok || len(first.Dests) != perFrame || aliasText(first.Alias) != "K5LEB" {
		t.Fatalf("first %+v", first)
	}
	if _, ok := DecodeBroadcast([]byte{0x00, 'K', '5', 'L', 'E', 'B', ' '}); ok {
		t.Fatal("bad signature")
	}
}

func TestUnlistedStationCannotRelayOrConnect(t *testing.T) {
	a := mustCall(t, "K5LEB")
	b := mustCall(t, "W1AW-1")
	c := mustCall(t, "W5BBS")
	guest := mustCall(t, "N0CALL")
	var forwarded []byte
	accepted := make(chan struct{}, 1)
	router := newTestRouter(t, b, func(_ ax25.Address, info []byte) error {
		forwarded = append([]byte(nil), info...)
		return nil
	}, func(_ ax25.Address, rw io.ReadWriteCloser) {
		accepted <- struct{}{}
		_ = rw.Close()
	})
	router.SetNeighbors([]ax25.Address{a, c})
	relay, err := Encode(Message{Origin: guest, Dest: c, TTL: 8, Index: 1, ID: 1, Op: OpDisc})
	if err != nil {
		t.Fatal(err)
	}
	router.Deliver(guest, relay)
	if forwarded != nil {
		t.Fatal("unlisted station relayed a frame")
	}
	connect, err := Encode(Message{
		Origin: guest, Dest: b, TTL: 8, Index: 1, ID: 1,
		Op: OpConnect, Window: 4, User: a, Node: guest,
	})
	if err != nil {
		t.Fatal(err)
	}
	router.Deliver(guest, connect)
	select {
	case <-accepted:
		t.Fatal("unlisted station opened a circuit")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestCircuitIgnoresTheWrongNeighbor(t *testing.T) {
	a := mustCall(t, "K5LEB")
	b := mustCall(t, "W1AW-1")
	c := mustCall(t, "W5BBS")
	got := make(chan string, 1)
	var accept []byte
	router := newTestRouter(t, b, func(to ax25.Address, info []byte) error {
		if to.Equal(a) {
			accept = append([]byte(nil), info...)
		}
		return nil
	}, func(_ ax25.Address, rw io.ReadWriteCloser) {
		defer rw.Close()
		buf := make([]byte, 16)
		n, _ := rw.Read(buf)
		got <- string(buf[:n])
	})
	router.SetNeighbors([]ax25.Address{a, c})
	connect, err := Encode(Message{
		Origin: a, Dest: b, TTL: 8, Index: 3, ID: 4,
		Op: OpConnect, Window: 4, User: a, Node: a,
	})
	if err != nil {
		t.Fatal(err)
	}
	router.Deliver(a, connect)
	ack, err := Decode(accept)
	if err != nil || ack.Op != OpAccept || ack.Choke {
		t.Fatalf("accept %+v %v", ack, err)
	}
	spoof, err := Encode(Message{
		Origin: a, Dest: b, TTL: 8, Index: ack.Tx, ID: ack.Rx, Op: OpInfo, Info: []byte("PWN"),
	})
	if err != nil {
		t.Fatal(err)
	}
	router.Deliver(c, spoof)
	real, err := Encode(Message{
		Origin: a, Dest: b, TTL: 8, Index: ack.Tx, ID: ack.Rx, Op: OpInfo, Info: []byte("OK"),
	})
	if err != nil {
		t.Fatal(err)
	}
	router.Deliver(a, real)
	select {
	case text := <-got:
		if strings.Contains(text, "PWN") || !strings.Contains(text, "OK") {
			t.Fatalf("payload %q", text)
		}
	case <-time.After(time.Second):
		t.Fatal("circuit did not deliver the real frame")
	}
}

func TestRelayDropsAnExpiredFrame(t *testing.T) {
	a := mustCall(t, "K5LEB")
	b := mustCall(t, "W1AW-1")
	c := mustCall(t, "W5BBS")
	var got []byte
	router := newTestRouter(t, b, func(to ax25.Address, info []byte) error {
		got = append([]byte(nil), info...)
		return nil
	}, nil)
	router.SetNeighbors([]ax25.Address{a, c})
	frame, err := Encode(Message{Origin: a, Dest: c, TTL: 1, Index: 1, ID: 1, Op: OpDisc})
	if err != nil {
		t.Fatal(err)
	}
	router.Deliver(a, frame)
	if got != nil {
		t.Fatalf("forwarded %x", got)
	}
	frame, err = Encode(Message{Origin: a, Dest: c, TTL: 2, Index: 1, ID: 1, Op: OpDisc})
	if err != nil {
		t.Fatal(err)
	}
	router.Deliver(a, frame)
	if len(got) < headerLen || got[14] != 1 {
		t.Fatalf("ttl %x", got)
	}
}

func TestCircuitCarriesBytesAcrossTwoHops(t *testing.T) {
	aCall := mustCall(t, "K5LEB")
	bCall := mustCall(t, "W1AW-1")
	cCall := mustCall(t, "W5BBS")
	var a, b, c *Router
	accept := func(_ ax25.Address, rw io.ReadWriteCloser) {
		defer rw.Close()
		if _, err := io.WriteString(rw, "W5BBS>\r"); err != nil {
			return
		}
		buf := make([]byte, 400)
		got, err := readFull(rw, len(bytes.Repeat([]byte("abc"), 100)), buf)
		if err != nil || !bytes.Equal(got, bytes.Repeat([]byte("abc"), 100)) {
			return
		}
		_, _ = io.WriteString(rw, "OK\r")
	}
	a = newTestRouter(t, aCall, func(_ ax25.Address, info []byte) error {
		b.Deliver(aCall, info)
		return nil
	}, nil)
	c = newTestRouter(t, cCall, func(_ ax25.Address, info []byte) error {
		b.Deliver(cCall, info)
		return nil
	}, accept)
	b = newTestRouter(t, bCall, func(to ax25.Address, info []byte) error {
		if to.Equal(aCall) {
			a.Deliver(bCall, info)
		}
		if to.Equal(cCall) {
			c.Deliver(bCall, info)
		}
		return nil
	}, nil)
	a.SetNeighbors([]ax25.Address{bCall})
	b.SetNeighbors([]ax25.Address{aCall, cCall})
	c.SetNeighbors([]ax25.Address{bCall})
	for _, part := range b.Payloads() {
		a.Observe(bCall, part)
	}
	if _, quality, ok := a.Lookup(cCall); !ok || quality != 144 {
		t.Fatalf("learned %d %v", quality, ok)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rw, err := a.Connect(ctx, cCall)
	if err != nil {
		t.Fatal(err)
	}
	defer rw.Close()
	got := readUntil(t, rw, "W5BBS>\r", 2*time.Second)
	if !strings.Contains(got, "W5BBS>\r") {
		t.Fatalf("prompt %q", got)
	}
	payload := bytes.Repeat([]byte("abc"), 100)
	if _, err := rw.Write(payload); err != nil {
		t.Fatal(err)
	}
	back := readUntil(t, rw, "OK\r", 2*time.Second)
	if !strings.Contains(back, "OK\r") {
		t.Fatalf("reply %q", back)
	}
}

func TestSecondCircuitIsRefusedAtTheCap(t *testing.T) {
	aCall := mustCall(t, "K5LEB")
	bCall := mustCall(t, "W1AW-1")
	hold := make(chan struct{})
	var a, b *Router
	a = newTestRouter(t, aCall, func(_ ax25.Address, info []byte) error {
		b.Deliver(aCall, info)
		return nil
	}, nil)
	b = New(Config{
		Local:       bCall,
		AckDelay:    20 * time.Millisecond,
		Timeout:     200 * time.Millisecond,
		Idle:        time.Hour,
		MaxCircuits: 1,
		Send: func(_ ax25.Address, info []byte) error {
			a.Deliver(bCall, info)
			return nil
		},
		Accept: func(_ ax25.Address, rw io.ReadWriteCloser) {
			defer rw.Close()
			_, _ = io.WriteString(rw, "UP\r")
			<-hold
		},
	})
	t.Cleanup(func() { close(hold); b.Close() })
	a.SetNeighbors([]ax25.Address{bCall})
	b.SetNeighbors([]ax25.Address{aCall})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	first, err := a.Connect(ctx, bCall)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if got := readUntil(t, first, "UP\r", time.Second); !strings.Contains(got, "UP\r") {
		t.Fatalf("first %q", got)
	}
	second, err := a.Connect(ctx, bCall)
	if err == nil || !strings.Contains(err.Error(), "refused") {
		if second != nil {
			second.Close()
		}
		t.Fatalf("second %v", err)
	}
}

func TestNAKResendStopsAndAFullBufferWaits(t *testing.T) {
	far := mustCall(t, "W1AW-1")
	var frames [][]byte
	r := &Router{
		local:   mustCall(t, "K5LEB"),
		ttl:     8,
		tries:   3,
		timeout: time.Hour,
		payload: 236,
		window:  4,
		send: func(_ ax25.Address, frame []byte) error {
			frames = append(frames, append([]byte(nil), frame...))
			return nil
		},
		circuits: map[uint16]*circuit{},
	}
	c := &circuit{
		state:      stUp,
		localIndex: 1,
		localID:    2,
		far:        far,
		via:        far,
		window:     4,
		vs:         1,
		unacked:    []outFrame{{data: []byte("hi")}},
		tries:      1,
		last:       time.Now().Add(-time.Minute),
		early:      map[byte][]byte{},
		cond:       sync.NewCond(&sync.Mutex{}),
	}
	r.circuits[localKey(c.localIndex, c.localID)] = c
	stamp := c.last
	r.onAck(far, Message{Origin: far, Index: c.localIndex, ID: c.localID, Op: OpAck, NAK: true})
	if !c.last.Equal(stamp) || !c.forceResend {
		t.Fatalf("NAK last %v force %v", c.last, c.forceResend)
	}
	for i := 0; i < 2; i++ {
		r.service(c, time.Now())
		r.onAck(far, Message{Origin: far, Index: c.localIndex, ID: c.localID, Op: OpAck, NAK: true})
	}
	r.service(c, time.Now())
	if c.state != stClosing {
		t.Fatalf("state %d", c.state)
	}
	info, disc := 0, 0
	for _, frame := range frames {
		msg, err := Decode(frame)
		if err != nil {
			t.Fatal(err)
		}
		switch msg.Op {
		case OpInfo:
			info++
		case OpDisc:
			disc++
		}
	}
	if info != 2 || disc != 1 {
		t.Fatalf("info %d disc %d", info, disc)
	}

	full := &circuit{state: stUp, window: 4, rbuf: bytes.Repeat([]byte{'x'}, maxBuf), early: map[byte][]byte{}}
	r.takeInfoLocked(full, 0, []byte("more"))
	if full.nak || full.ackDue.IsZero() {
		t.Fatalf("full nak %v ack %v", full.nak, full.ackDue)
	}
	r.takeInfoLocked(full, 1, []byte("later"))
	if !full.nak {
		t.Fatal("out of order did not NAK")
	}
}

func newTestRouter(t *testing.T, local ax25.Address, send func(ax25.Address, []byte) error, accept func(ax25.Address, io.ReadWriteCloser)) *Router {
	t.Helper()
	r := New(Config{
		Local:    local,
		AckDelay: 20 * time.Millisecond,
		Timeout:  200 * time.Millisecond,
		Idle:     time.Hour,
		Payload:  50,
		Send:     send,
		Accept:   accept,
	})
	t.Cleanup(r.Close)
	return r
}

func mustCall(t *testing.T, call string) ax25.Address {
	t.Helper()
	addr, err := ax25.Parse(call)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func readUntil(t *testing.T, r io.Reader, needle string, d time.Duration) string {
	t.Helper()
	got := make(chan string, 1)
	fail := make(chan error, 1)
	go func() {
		var buf bytes.Buffer
		tmp := make([]byte, 128)
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
				if strings.Contains(buf.String(), needle) {
					got <- buf.String()
					return
				}
			}
			if err != nil {
				fail <- err
				return
			}
		}
	}()
	select {
	case text := <-got:
		return text
	case err := <-fail:
		t.Fatalf("read %v", err)
	case <-time.After(d):
		t.Fatal("timed out waiting for " + needle)
	}
	return ""
}

func readFull(r io.Reader, n int, buf []byte) ([]byte, error) {
	got := buf[:0]
	tmp := make([]byte, n)
	for len(got) < n {
		nn, err := r.Read(tmp)
		got = append(got, tmp[:nn]...)
		if err != nil {
			return got, err
		}
	}
	return got, nil
}
