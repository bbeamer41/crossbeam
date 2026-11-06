// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package radio opens a plugged-in radio through rigctld and sets its
// frequency and mode. The modem still sends the audio and keys the
// transmitter.
package radio

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bbeamer41/crossbeam/internal/settings"
)

// Supervisor owns the rigctld process for the saved radio settings.
type Supervisor struct {
	command string

	mu      sync.Mutex
	child   *child
	applied target
	note    string
	ok      bool
}

type target struct {
	model  string
	device string
	freq   string
	mode   string
	hz     int64
}

type child struct {
	cmd  *exec.Cmd
	done chan struct{}
	err  error
	tail *tailWriter
	addr string
}

// New returns a supervisor that runs the rigctld on PATH.
func New() *Supervisor {
	return &Supervisor{command: "rigctld", ok: true}
}

// Status reports the last attempt. ok is false when the radio did not
// accept the settings. An empty note means radio control is off.
func (s *Supervisor) Status() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.note, s.ok
}

// Sync starts, retunes, or stops rigctld so it matches the saved settings.
func (s *Supervisor) Sync(ctx context.Context, current map[string]string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	want, err := targetFrom(current)
	if err != nil {
		s.fail(err)
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if want.model == "" {
		s.stopLocked()
		s.note = ""
		s.ok = true
		s.applied = target{}
		return nil
	}
	if s.child != nil && s.applied == want && s.aliveLocked() {
		s.note = want.freq + " MHz " + want.mode
		s.ok = true
		return nil
	}
	if s.child == nil || !s.aliveLocked() || s.applied.model != want.model || s.applied.device != want.device {
		s.stopLocked()
		if err := s.startLocked(ctx, want); err != nil {
			s.failLocked(err)
			return err
		}
	}
	if err := tune(s.child.addr, want.hz, want.mode); err != nil {
		s.failLocked(err)
		return err
	}
	s.applied = want
	s.note = want.freq + " MHz " + want.mode
	s.ok = true
	return nil
}

// Close stops rigctld.
func (s *Supervisor) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *Supervisor) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failLocked(err)
}

func (s *Supervisor) failLocked(err error) {
	s.note = err.Error()
	s.ok = false
}

func (s *Supervisor) aliveLocked() bool {
	if s.child == nil {
		return false
	}
	select {
	case <-s.child.done:
		return false
	default:
		return true
	}
}

func (s *Supervisor) startLocked(ctx context.Context, want target) error {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	path, err := exec.LookPath(s.command)
	if err != nil {
		return fmt.Errorf("rigctld is not installed")
	}
	cmd := exec.Command(path, "-m", want.model, "-r", want.device, "-T", "127.0.0.1", "-t", fmt.Sprint(port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	tail := &tailWriter{}
	cmd.Stderr = tail
	proc := &child{
		cmd:  cmd,
		done: make(chan struct{}),
		tail: tail,
		addr: fmt.Sprintf("127.0.0.1:%d", port),
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("the radio did not open: %s", oneLine(err.Error()))
	}
	go func() {
		proc.err = cmd.Wait()
		close(proc.done)
	}()
	s.child = proc
	if err := s.waitReady(ctx, proc); err != nil {
		s.stopLocked()
		return err
	}
	return nil
}

func (s *Supervisor) waitReady(ctx context.Context, proc *child) error {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("the radio did not open")
		case <-proc.done:
			return openError(proc)
		default:
		}
		conn, err := net.DialTimeout("tcp", proc.addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the radio did not open")
		case <-proc.done:
			return openError(proc)
		case <-tick.C:
		}
	}
}

func openError(proc *child) error {
	detail := oneLine(proc.tail.String())
	if detail == "" && proc.err != nil {
		detail = oneLine(proc.err.Error())
	}
	if detail == "" {
		return fmt.Errorf("the radio did not open")
	}
	return fmt.Errorf("the radio did not open: %s", detail)
}

func (s *Supervisor) stopLocked() {
	proc := s.child
	s.child = nil
	s.applied = target{}
	if proc == nil || proc.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-proc.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-proc.done:
	case <-time.After(2 * time.Second):
		_ = syscall.Kill(-proc.cmd.Process.Pid, syscall.SIGKILL)
		<-proc.done
	}
}

func targetFrom(current map[string]string) (target, error) {
	checked, err := settings.Validate(current)
	if err != nil {
		return target{}, err
	}
	want := target{
		model:  checked[settings.KeyRadioModel],
		device: checked[settings.KeyRadioDevice],
		freq:   checked[settings.KeyRadioFrequency],
		mode:   checked[settings.KeyRadioMode],
	}
	if want.model == "" {
		return want, nil
	}
	hz, err := settings.FrequencyHz(want.freq)
	if err != nil {
		return target{}, err
	}
	want.hz = hz
	return want, nil
}

func oneLine(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 200 {
		text = text[:200]
	}
	return text
}

type tailWriter struct {
	mu  sync.Mutex
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > 4096 {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-4096:]...)
	}
	return len(p), nil
}

func (w *tailWriter) String() string {
	if w == nil {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}
