// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ax25

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRNRFinalDoesNotRetransmit(t *testing.T) {
	var frames []Frame
	sw := &Switch{
		local:  Address{Call: "K5LEB"},
		t1:     time.Second,
		window: 4,
		now:    time.Now,
		send: func(_ Address, f Frame) {
			frames = append(frames, f)
		},
	}
	c := sw.newConn(Address{Call: "W1AW"})
	c.phase = phaseInfo
	c.vs = 1
	c.poll = true
	c.sent[0] = slot{pid: PIDText, data: []byte("hi")}
	sw.onS(c, Frame{Command: false, Control: SControl(RNR, 0, true)})
	if !c.peerBusy || c.poll {
		t.Fatalf("busy %v poll %v", c.peerBusy, c.poll)
	}
	for _, f := range frames {
		if f.Control&1 == 0 {
			t.Fatal("RNR final retransmitted an I frame")
		}
	}
	frames = nil
	c.poll = true
	sw.onS(c, Frame{Command: false, Control: SControl(RR, 0, true)})
	sawI := false
	for _, f := range frames {
		if f.Control&1 == 0 {
			sawI = true
		}
	}
	if !sawI || c.poll {
		t.Fatal("RR final did not retransmit")
	}
}

func TestCanceledDialReleasesTheCall(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	sw := NewSwitch(Config{
		Local: Address{Call: "K5LEB"},
		T1:    time.Hour,
		Send: func(_ Address, f Frame) {
			if IsSABM(f.Control) {
				once.Do(func() { close(started) })
			}
		},
	})
	t.Cleanup(func() { sw.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := sw.Dial(ctx, Address{Call: "W1AW"})
		errc <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("no sabm")
	}
	cancel()
	if err := <-errc; err == nil {
		t.Fatal("dial returned a stream after cancel")
	}
	sw.t1 = 40 * time.Millisecond
	sw.n2 = 2
	ctx, cancel = context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_, err := sw.Dial(ctx, Address{Call: "W1AW"})
	if err == nil || strings.Contains(err.Error(), "already connected") {
		t.Fatalf("second dial %v", err)
	}
}

func TestSendNetromQueueIsBounded(t *testing.T) {
	local := Address{Call: "K5LEB"}
	remote := Address{Call: "W1AW"}
	sw := NewSwitch(Config{
		Local: local,
		T1:    time.Hour,
		Send:  func(Address, Frame) {},
	})
	t.Cleanup(func() { sw.Close() })
	sw.Inject(Frame{Dest: local, Src: remote, Command: true, Control: SABM | 0x10})
	var err error
	deadline := time.Now().Add(time.Second)
	for {
		err = sw.SendNetrom(remote, []byte{1})
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	sent := 1
	for sent <= sw.window+maxNetromQueue+5 {
		err = sw.SendNetrom(remote, []byte{2})
		if err != nil {
			break
		}
		sent++
	}
	if err == nil || !strings.Contains(err.Error(), "routing queue is full") {
		t.Fatalf("after %d: %v", sent, err)
	}
	if sent > sw.window+maxNetromQueue {
		t.Fatalf("accepted %d", sent)
	}
}

func TestIdleSessionDisconnects(t *testing.T) {
	frames := make(chan Frame, 4)
	local := Address{Call: "K5LEB"}
	sw := NewSwitch(Config{
		Local: local,
		T1:    time.Hour,
		Idle:  40 * time.Millisecond,
		Send: func(_ Address, f Frame) {
			frames <- f
		},
	})
	t.Cleanup(func() { sw.Close() })
	sw.Inject(Frame{Dest: local, Src: Address{Call: "N0CALL"}, Command: true, Control: SABM | 0x10})
	select {
	case f := <-frames:
		if !IsUA(f.Control) {
			t.Fatalf("ua %x", f.Control)
		}
	case <-time.After(time.Second):
		t.Fatal("no ua")
	}
	deadline := time.After(time.Second)
	for {
		select {
		case f := <-frames:
			if IsDISC(f.Control) && f.Command {
				return
			}
		case <-deadline:
			t.Fatal("idle session stayed up")
		}
	}
}
