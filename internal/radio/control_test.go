// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package radio

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/bbeamer41/crossbeam/internal/settings"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "-m" {
		os.Exit(fakeRigctld(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func TestSupervisorTunesAndStops(t *testing.T) {
	logPath := t.TempDir() + "/rig.log"
	t.Setenv("CROSSBEAM_RIG_LOG", logPath)
	s := New()
	s.command = os.Args[0]
	t.Cleanup(s.Close)

	current := map[string]string{
		settings.KeyRadioModel:     "3073",
		settings.KeyRadioDevice:    "usb-Icom_IC-7300_if00",
		settings.KeyRadioFrequency: "145.030",
		settings.KeyRadioMode:      "fm",
	}
	if err := s.Sync(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	note, ok := s.Status()
	if !ok || note != "145.03 MHz FM" {
		t.Fatalf("status %q %v", note, ok)
	}
	if err := s.Sync(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	text := readLog(t, logPath)
	if strings.Count(text, "argv ") != 1 {
		t.Fatalf("second sync started another controller\n%s", text)
	}
	if !strings.Contains(text, "-T 127.0.0.1") || !strings.Contains(text, "-m 3073") || !strings.Contains(text, "-r /dev/serial/by-id/usb-Icom_IC-7300_if00") {
		t.Fatalf("argv %s", text)
	}
	if strings.Contains(text, " -o ") || strings.Contains(text, " -R ") || strings.Contains(text, "-T 0.0.0.0") {
		t.Fatalf("argv %s", text)
	}
	if !strings.Contains(text, "F 145030000") || !strings.Contains(text, "M FM 0") {
		t.Fatalf("commands %s", text)
	}
	if strings.Count(text, "F 145030000") != 1 {
		t.Fatalf("retuned a matching radio\n%s", text)
	}

	current[settings.KeyRadioFrequency] = "145.050"
	if err := s.Sync(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	text = readLog(t, logPath)
	if strings.Count(text, "argv ") != 1 || !strings.Contains(text, "F 145050000") {
		t.Fatalf("frequency change\n%s", text)
	}
	note, ok = s.Status()
	if !ok || note != "145.05 MHz FM" {
		t.Fatalf("retune status %q %v", note, ok)
	}

	current[settings.KeyRadioModel] = ""
	if err := s.Sync(context.Background(), current); err != nil {
		t.Fatal(err)
	}
	note, ok = s.Status()
	if !ok || note != "" {
		t.Fatalf("off %q %v", note, ok)
	}
	s.mu.Lock()
	stopped := s.child == nil
	s.mu.Unlock()
	if !stopped {
		t.Fatal("controller still running")
	}
}

func TestSupervisorReportsARejectedTune(t *testing.T) {
	t.Setenv("CROSSBEAM_RIG_FAIL", "tune")
	t.Setenv("CROSSBEAM_RIG_LOG", t.TempDir()+"/rig.log")
	s := New()
	s.command = os.Args[0]
	t.Cleanup(s.Close)
	err := s.Sync(context.Background(), map[string]string{
		settings.KeyRadioModel:     "3073",
		settings.KeyRadioDevice:    "usb-Icom_IC-7300_if00",
		settings.KeyRadioFrequency: "145.030",
	})
	if err == nil || !strings.Contains(err.Error(), "the radio rejected the frequency") {
		t.Fatal(err)
	}
	note, ok := s.Status()
	if ok || !strings.Contains(note, "the radio rejected the frequency") {
		t.Fatalf("status %q %v", note, ok)
	}
	s.mu.Lock()
	alive := s.aliveLocked()
	s.mu.Unlock()
	if !alive {
		t.Fatal("rejected tune stopped the controller")
	}
}

func TestSupervisorReportsAClosedRadio(t *testing.T) {
	t.Setenv("CROSSBEAM_RIG_FAIL", "open")
	s := New()
	s.command = os.Args[0]
	t.Cleanup(s.Close)
	err := s.Sync(context.Background(), map[string]string{
		settings.KeyRadioModel:     "3073",
		settings.KeyRadioDevice:    "usb-Icom_IC-7300_if00",
		settings.KeyRadioFrequency: "145.030",
	})
	if err == nil || !strings.Contains(err.Error(), "radio unplugged") {
		t.Fatal(err)
	}
	s.mu.Lock()
	stopped := s.child == nil
	s.mu.Unlock()
	if !stopped {
		t.Fatal("failed open left the controller running")
	}
}

func TestFakeRefusesAPublicListen(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-m", "1", "-r", "/dev/null", "-T", "0.0.0.0", "-t", "4532")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "127.0.0.1") {
		t.Fatalf("%v %s", err, out)
	}
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func fakeRigctld(args []string) int {
	fs := flag.NewFlagSet("rigctld", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	model := fs.String("m", "", "")
	device := fs.String("r", "", "")
	listen := fs.String("T", "", "")
	port := fs.String("t", "", "")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *listen != "127.0.0.1" {
		fmt.Fprintln(os.Stderr, "rigctld listens on 127.0.0.1 only")
		return 1
	}
	if *model == "" || *device == "" || *port == "" {
		fmt.Fprintln(os.Stderr, "model, device, and port are required")
		return 1
	}
	if os.Getenv("CROSSBEAM_RIG_FAIL") == "open" {
		fmt.Fprintln(os.Stderr, "radio unplugged")
		return 1
	}
	logPath := os.Getenv("CROSSBEAM_RIG_LOG")
	appendLog(logPath, "argv "+strings.Join(os.Args[1:], " "))
	ln, err := net.Listen("tcp", net.JoinHostPort(*listen, *port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "listen: %v\n", err)
		return 1
	}
	defer ln.Close()
	fail := os.Getenv("CROSSBEAM_RIG_FAIL")
	for {
		conn, err := ln.Accept()
		if err != nil {
			return 0
		}
		serveRig(conn, logPath, fail)
	}
}

func serveRig(conn net.Conn, logPath, fail string) {
	defer conn.Close()
	rd := bufio.NewReader(conn)
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		appendLog(logPath, line)
		reply := "RPRT 0\n"
		if fail == "tune" && strings.HasPrefix(line, "F ") {
			reply = "RPRT 11\n"
		}
		if _, err := io.WriteString(conn, reply); err != nil {
			return
		}
	}
}

func appendLog(path, line string) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}
