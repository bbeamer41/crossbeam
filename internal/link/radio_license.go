// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package link

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
)

const (
	// radioDenyFor is how long a failed check is reused so a retry does
	// not ask the FCC again for the same callsign.
	radioDenyFor = time.Minute
	// radioAllowFor is how long a passed check waits for the AX.25
	// session to open. The open session keeps that approval until it ends.
	radioAllowFor = 2 * time.Minute
	radioBusyMax  = 4
	radioMapMax   = 64
)

// radioJob is one license check shared by every frame for that callsign.
type radioJob struct {
	done chan struct{}
	ok   bool
}

func (n *Node) kissLife() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-n.quit:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// beginRadio starts a new approval for this TNC connection.
func (n *Node) beginRadio() int {
	n.radioMu.Lock()
	defer n.radioMu.Unlock()
	n.radioGen++
	n.radioReady = false
	n.radioDecided = false
	n.wakeRadioLocked()
	return n.radioGen
}

func (n *Node) wakeRadioLocked() {
	if n.radioWait != nil {
		close(n.radioWait)
		n.radioWait = nil
	}
}

// approveSelf checks this station once per TNC connection. The kiss loop
// waits for it before reading connected frames. A denial closes the TNC
// so the next attempt waits out the quiet period.
func (n *Node) approveSelf(gen int) {
	base := n.local.Call
	if n.selfCached(base) {
		n.finishSelf(gen, true)
		return
	}
	job := n.startRadio(base, true)
	if job == nil {
		n.finishSelf(gen, false)
		return
	}
	select {
	case <-job.done:
	case <-n.quit:
		return
	}
	n.finishSelf(gen, job.ok)
}

func (n *Node) finishSelf(gen int, ok bool) {
	n.radioMu.Lock()
	if gen != n.radioGen {
		n.radioMu.Unlock()
		return
	}
	n.radioDecided = true
	n.radioReady = ok
	n.wakeRadioLocked()
	conn := n.kissConn
	n.radioMu.Unlock()
	if ok || conn == nil || !n.alive() {
		return
	}
	n.clearKissConn(conn)
	_ = conn.Close()
}

func (n *Node) selfCached(call string) bool {
	n.radioMu.Lock()
	defer n.radioMu.Unlock()
	return n.radioSelfCall == call && time.Now().Before(n.radioSelfUntil)
}

func (n *Node) selfQuiet() time.Duration {
	n.radioMu.Lock()
	until := n.radioDeny[n.local.Call]
	n.radioMu.Unlock()
	d := time.Until(until)
	if d < 0 {
		return 0
	}
	return d
}

func (n *Node) radioUp() bool {
	n.radioMu.Lock()
	ok := n.radioReady
	n.radioMu.Unlock()
	return ok
}

// radioGranted reports whether a connected frame may be addressed to call.
// An open radio session or a recent approval both count. The SSID is not
// a separate license.
func (n *Node) radioGranted(call string) bool {
	base, ok := baseCall(call)
	if !ok {
		return false
	}
	n.radioMu.Lock()
	defer n.radioMu.Unlock()
	if n.radioOpen[base] > 0 {
		return true
	}
	until, ok := n.radioAllow[base]
	return ok && time.Now().Before(until)
}

// waitRadio blocks until this TNC connection has a license decision.
func (n *Node) waitRadio(ctx context.Context) bool {
	for {
		n.radioMu.Lock()
		if n.radioDecided {
			ok := n.radioReady
			n.radioMu.Unlock()
			return ok
		}
		ch := n.radioWait
		if ch == nil {
			ch = make(chan struct{})
			n.radioWait = ch
		}
		n.radioMu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return false
		case <-n.quit:
			return false
		}
	}
}

// radioAdmit reports whether an inbound connected frame may be delivered.
// It does not wait on the FCC. A frame that still needs a check is dropped
// so the TNC is not keyed for a UA or a DM.
func (n *Node) radioAdmit(call string) bool {
	base, ok := baseCall(call)
	if !ok {
		return false
	}
	n.radioMu.Lock()
	now := time.Now()
	open := n.radioOpen[base] > 0
	allowed := false
	if until, ok := n.radioAllow[base]; ok && now.Before(until) {
		allowed = true
	}
	denied := false
	if until, ok := n.radioDeny[base]; ok && now.Before(until) {
		denied = true
	}
	n.radioMu.Unlock()
	if open || allowed {
		return true
	}
	if denied {
		return false
	}
	n.startRadio(base, false)
	return false
}

// approveRadio waits for one outbound approval.
func (n *Node) approveRadio(ctx context.Context, call string) bool {
	base, ok := baseCall(call)
	if !ok {
		return false
	}
	n.radioMu.Lock()
	now := time.Now()
	if n.radioOpen[base] > 0 {
		n.radioMu.Unlock()
		return true
	}
	if until, ok := n.radioAllow[base]; ok && now.Before(until) {
		n.radioMu.Unlock()
		return true
	}
	if until, ok := n.radioDeny[base]; ok && now.Before(until) {
		n.radioMu.Unlock()
		return false
	}
	n.radioMu.Unlock()
	job := n.startRadio(base, false)
	if job == nil {
		return false
	}
	select {
	case <-job.done:
		return job.ok
	case <-ctx.Done():
		return false
	case <-n.quit:
		return false
	}
}

func (n *Node) startRadio(base string, self bool) *radioJob {
	base, ok := baseCall(base)
	if !ok {
		return nil
	}
	n.radioMu.Lock()
	defer n.radioMu.Unlock()
	if job := n.radioJobs[base]; job != nil {
		return job
	}
	if !self && n.radioBusy >= radioBusyMax {
		return nil
	}
	job := &radioJob{done: make(chan struct{})}
	if n.radioJobs == nil {
		n.radioJobs = map[string]*radioJob{}
	}
	n.radioJobs[base] = job
	if !self {
		n.radioBusy++
	}
	go n.finishRadio(base, self, job)
	return job
}

func (n *Node) finishRadio(base string, self bool, job *radioJob) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-n.quit:
			cancel()
		case <-ctx.Done():
		}
	}()
	ok := false
	failed := false
	if n.license == nil {
		failed = true
	} else {
		var err error
		ok, err = n.license(ctx, base)
		if err != nil || ctx.Err() != nil {
			ok = false
			failed = ctx.Err() == nil
		}
	}
	n.radioMu.Lock()
	now := time.Now()
	if !ok && ctx.Err() == nil {
		delete(n.radioAllow, base)
		n.rememberLocked(&n.radioDeny, base, now.Add(radioDenyFor))
		n.noteRadioLocked(base, failed, now)
	} else if ok {
		delete(n.radioDeny, base)
		if self {
			n.radioSelfCall = base
			n.radioSelfUntil = now.Add(radioDenyFor)
		} else {
			n.rememberLocked(&n.radioAllow, base, now.Add(radioAllowFor))
		}
	}
	job.ok = ok
	delete(n.radioJobs, base)
	if !self && n.radioBusy > 0 {
		n.radioBusy--
	}
	n.radioMu.Unlock()
	close(job.done)
}

func (n *Node) rememberLocked(m *map[string]time.Time, key string, until time.Time) {
	if *m == nil {
		*m = map[string]time.Time{}
	}
	if len(*m) >= radioMapMax {
		for old := range *m {
			delete(*m, old)
			break
		}
	}
	(*m)[key] = until
}

func (n *Node) noteRadioLocked(call string, failed bool, now time.Time) {
	if n.radioNote == nil {
		n.radioNote = map[string]time.Time{}
	}
	if prev, ok := n.radioNote[call]; ok && now.Sub(prev) < radioDenyFor {
		return
	}
	if len(n.radioNote) >= radioMapMax {
		n.radioNote = map[string]time.Time{}
	}
	n.radioNote[call] = now
	text := "was not confirmed for the radio"
	if failed {
		text = "could not be checked for the radio"
	}
	// The callsign is the on-air address. The licensee name stays out.
	log.Printf("crossbeam: %s %s", call, text)
}

// trackRadio remembers a radio session so the next frame of that session
// does not ask the FCC again. The next session, after this one ends, does.
func (n *Node) trackRadio(remote ax25.Address, on bool) {
	if on && (n.kissAddr == "" || n.lookupAddr(remote) != nil) {
		return
	}
	if !on && n.kissAddr == "" {
		return
	}
	base, ok := baseCall(remote.Call)
	if !ok {
		return
	}
	n.radioMu.Lock()
	defer n.radioMu.Unlock()
	key := remote.String()
	if n.radioSess == nil {
		n.radioSess = map[string]int{}
	}
	if on {
		n.radioSess[key]++
		if n.radioOpen == nil {
			n.radioOpen = map[string]int{}
		}
		n.radioOpen[base]++
		return
	}
	if n.radioSess[key] == 0 {
		return
	}
	n.radioSess[key]--
	if n.radioSess[key] == 0 {
		delete(n.radioSess, key)
	}
	if n.radioOpen[base] <= 1 {
		delete(n.radioOpen, base)
		delete(n.radioAllow, base)
		return
	}
	n.radioOpen[base]--
}

func baseCall(call string) (string, bool) {
	call = strings.ToUpper(strings.TrimSpace(call))
	if i := strings.IndexByte(call, '-'); i >= 0 {
		call = call[:i]
	}
	if len(call) < 1 || len(call) > 6 {
		return "", false
	}
	for i := 0; i < len(call); i++ {
		c := call[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return "", false
		}
	}
	return call, true
}
