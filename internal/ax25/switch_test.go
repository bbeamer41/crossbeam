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
