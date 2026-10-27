// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package telnet

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestGreetingAndLine(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	wrap := Wrap(left)
	gotGreet := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 9)
		_, err := io.ReadFull(right, buf)
		gotGreet <- append([]byte(nil), buf...)
		if err != nil {
			return
		}
		_, _ = right.Write([]byte{iac, will, optSGA, iac, do, optEcho, 'h', 'i', '\r', 0})
	}()
	buf := make([]byte, 8)
	n, err := wrap.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "hi\r" {
		t.Fatalf("line %q", buf[:n])
	}
	greet := <-gotGreet
	want := []byte{iac, will, optSGA, iac, wont, optEcho, iac, do, optSGA}
	if string(greet) != string(want) {
		t.Fatalf("greeting %v", greet)
	}
}

func TestData255IsEscaped(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	wrap := Wrap(left)
	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 16)
		n, _ := io.ReadFull(right, buf[:9])
		n2, _ := io.ReadFull(right, buf[n:n+3])
		done <- append([]byte(nil), buf[:n+n2]...)
	}()
	if _, err := wrap.Write([]byte{iac, 'A'}); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if len(got) != 12 || got[9] != iac || got[10] != iac || got[11] != 'A' {
		t.Fatalf("wire %v", got)
	}
}

func TestReplyOnce(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	wrap := Wrap(left)
	go func() {
		buf := make([]byte, 8)
		_, _ = wrap.Read(buf)
	}()
	greet := make([]byte, 9)
	if _, err := io.ReadFull(right, greet); err != nil {
		t.Fatal(err)
	}
	var msg []byte
	for i := 0; i < 10; i++ {
		msg = append(msg, iac, will, 0)
	}
	msg = append(msg, 'Z')
	if _, err := right.Write(msg); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 3)
	if _, err := io.ReadFull(right, reply); err != nil {
		t.Fatal(err)
	}
	if reply[0] != iac || reply[1] != dont || reply[2] != 0 {
		t.Fatalf("reply %v", reply)
	}
	extra := make(chan struct{})
	go func() {
		one := []byte{0}
		_, _ = right.Read(one)
		close(extra)
	}()
	select {
	case <-extra:
		t.Fatal("repeated option got another reply")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestTwoSidesExchange(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		srv := Wrap(conn)
		defer srv.Close()
		if _, err := srv.Write([]byte("Hello\r")); err != nil {
			return
		}
		buf := make([]byte, 8)
		n, err := srv.Read(buf)
		if err != nil {
			return
		}
		got <- string(buf[:n])
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := Wrap(conn)
	buf := make([]byte, 16)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "Hello\r" {
		t.Fatalf("banner %q", buf[:n])
	}
	if _, err := client.Write([]byte{iac, '\r', '\n'}); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-got:
		if line != string([]byte{iac, '\r'}) {
			t.Fatalf("server saw %q", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not read")
	}
}
