// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
	"github.com/bbeamer41/crossbeam/internal/bulletin"
	"github.com/bbeamer41/crossbeam/internal/link"
	"github.com/bbeamer41/crossbeam/internal/store"
)

func TestArticleCommands(t *testing.T) {
	stdout, stderr, code := capture([]string{"article", "preview", "--title", "Hello", "--author", "K5LEB", "--created", "2026-10-18T15:04:05Z", "--body", "The net meets."})
	if code != 0 || stderr != "" || !strings.Contains(stdout, "--- packet ---") || !strings.Contains(stdout, "--- micron ---") || !strings.Contains(stdout, "Title: Hello") || !strings.Contains(stdout, "----") {
		t.Fatalf("preview code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = capture([]string{"article", "preview", "--title", "Hello"})
	if code != 1 || stdout != "" || !strings.Contains(stderr, "author is empty") {
		t.Fatalf("invalid preview code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	_, stderr, code = capture([]string{"article", "preview", "--title", "Hello", "--author", "K5LEB", "--body", "x", "--body-file", "also.txt"})
	if code != 2 || !strings.Contains(stderr, "--body") {
		t.Fatalf("both bodies code=%d stderr=%q", code, stderr)
	}

	dir := filepath.Join(t.TempDir(), "state")
	body := filepath.Join(t.TempDir(), "body.txt")
	if err := os.WriteFile(body, []byte("The net meets Thursday."), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code = capture([]string{"article", "save", "--state", dir, "--title", "Hello", "--author", "K5LEB", "--created", "2026-10-18T15:04:05Z", "--body-file", body})
	if code != 0 || stderr != "" || !strings.Contains(stdout, "saved 1") {
		t.Fatalf("save code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = capture([]string{"article", "list", "--state", dir})
	if code != 0 || !strings.Contains(stdout, "Hello") || !strings.Contains(stdout, "K5LEB") {
		t.Fatalf("list code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = capture([]string{"article", "delete", "--state", dir, "1"})
	if code != 0 || stderr != "" || stdout != "deleted 1\n" {
		t.Fatalf("delete code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = capture([]string{"article", "list", "--state", dir})
	if code != 0 || strings.Contains(stdout, "Hello") {
		t.Fatalf("list after delete code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	_, stderr, code = capture([]string{"article", "delete", "--state", dir, "1"})
	if code != 1 || !strings.Contains(stderr, "article not found") {
		t.Fatalf("missing code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = capture([]string{"article", "delete", "--state", dir, "0"})
	if code != 2 || !strings.Contains(stderr, "positive integer") {
		t.Fatalf("zero code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = capture([]string{"article", "save", "--title", "Hello", "extra"})
	if code != 2 || !strings.Contains(stderr, "does not take arguments") {
		t.Fatalf("save extra code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = capture([]string{"article", "list", "extra"})
	if code != 2 || !strings.Contains(stderr, "does not take arguments") {
		t.Fatalf("list extra code=%d stderr=%q", code, stderr)
	}
}

func TestBulletinSendWaitsUntilStopped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "terminal.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		rd := bufio.NewReader(conn)
		line, err := rd.ReadString('\n')
		if err != nil || line != "FORWARD W1AW-1\n" {
			return
		}
		time.Sleep(300 * time.Millisecond)
		_, _ = conn.Write([]byte("OK sent 1 received 0\n"))
	}()
	stdout, stderr, code := capture([]string{"bulletin", "send", "--socket", path, "W1AW-1"})
	if code != 0 || stderr != "" || stdout != "sent 1 received 0\n" {
		t.Fatalf("wait code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan struct{})
	closed := make(chan struct{})
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		rd := bufio.NewReader(conn)
		if _, err := rd.ReadString('\n'); err != nil {
			return
		}
		close(got)
		one := []byte{0}
		_, _ = conn.Read(one)
		close(closed)
	}()
	type result struct {
		stdout string
		stderr string
		code   int
	}
	done := make(chan result, 1)
	go func() {
		stdout, stderr, code := captureIn(ctx, []string{"bulletin", "send", "--socket", path, "W1AW-1"}, strings.NewReader(""))
		done <- result{stdout, stderr, code}
	}()
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("forward did not start")
	}
	cancel()
	select {
	case res := <-done:
		if res.code != 1 || res.stdout != "" || !strings.Contains(res.stderr, "forward stopped") {
			t.Fatalf("stop code=%d stdout=%q stderr=%q", res.code, res.stdout, res.stderr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("command hung")
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("socket stayed open")
	}
}

func TestSysopPassword(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	stdout, stderr, code := captureIn(context.Background(), []string{"sysop", "password", "--state", dir}, strings.NewReader(strings.Repeat("a", 11)+"\n"))
	if code != 1 || !strings.Contains(stderr, "12") {
		t.Fatalf("short code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = captureIn(context.Background(), []string{"sysop", "password", "--state", dir}, strings.NewReader("  test-password-1  \r\n"))
	if code != 0 || !strings.Contains(stdout, "saved") {
		t.Fatalf("set code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ok, err := st.CheckPassword(context.Background(), "  test-password-1  ")
	if err != nil || !ok {
		t.Fatalf("spaces were trimmed: %v %v", ok, err)
	}
	stdout, stderr, code = capture([]string{"sysop"})
	if code != 2 || !strings.Contains(stderr, "sysop password") {
		t.Fatalf("usage code=%d stderr=%q", code, stderr)
	}
	stdout, stderr, code = capture([]string{"serve", "--state", filepath.Join(t.TempDir(), "empty"), "--listen", "127.0.0.1:0"})
	if code != 78 || !strings.Contains(stderr, "sysop password --state") {
		t.Fatalf("no password code=%d stderr=%q", code, stderr)
	}
	_, stderr, code = capture([]string{"serve", "--socket-mode", "0666"})
	if code != 2 || !strings.Contains(stderr, "0600 or 0660") {
		t.Fatalf("mode code=%d stderr=%q", code, stderr)
	}
}

func TestConnectCommand(t *testing.T) {
	_, stderr, code := capture([]string{"connect"})
	if code != 2 || !strings.Contains(stderr, "usage") {
		t.Fatalf("usage %d %s", code, stderr)
	}
	_, stderr, code = capture([]string{"connect", "--socket", filepath.Join(t.TempDir(), "missing.sock"), "NOTACALL"})
	if code != 2 || !strings.Contains(stderr, "callsign") {
		t.Fatalf("call %d %s", code, stderr)
	}
	_, stderr, code = capture([]string{"connect", "--socket", filepath.Join(t.TempDir(), "missing.sock"), "W1AW-1"})
	if code != 1 || !strings.Contains(stderr, "not running") {
		t.Fatalf("down %d %s", code, stderr)
	}

	call, err := ax25.Parse("W1AW-1")
	if err != nil {
		t.Fatal(err)
	}
	remote, err := link.Listen(link.Config{Bind: "127.0.0.1:0", Local: call, T1: 200 * time.Millisecond, N2: 5})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	dir := filepath.Join(t.TempDir(), "state")
	stdout, stderr, code := captureIn(context.Background(), []string{"sysop", "password", "--state", dir}, strings.NewReader("test-password-1\n"))
	if code != 0 {
		t.Fatalf("password %d %s %s", code, stdout, stderr)
	}
	peer := "W1AW-1@" + remote.LocalAddr().String()
	if _, stderr, code = capture([]string{"settings", "set", "--state", dir, "station.callsign", "K5LEB"}); code != 0 {
		t.Fatal(stderr)
	}
	if _, stderr, code = capture([]string{"settings", "set", "--state", dir, "link.peers", peer}); code != 0 {
		t.Fatal(stderr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out lockedBuffer
	var errb lockedBuffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"serve", "--state", dir, "--listen", "127.0.0.1:0", "--link", "127.0.0.1:0"}, strings.NewReader(""), &out, &errb)
	}()
	term := filepath.Join(dir, "terminal.sock")
	deadline := time.Now().Add(8 * time.Second)
	for {
		if strings.Contains(out.String(), "ax/udp 127.0.0.1:") && dialUnix(term) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("serve did not start the link: %s %s", out.String(), errb.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	connectCtx, connectCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer connectCancel()
	stdout, stderr, code = captureIn(connectCtx, []string{"connect", "--socket", term, "W1AW-1"}, strings.NewReader("?\rBYE\r"))
	if code != 0 || !strings.Contains(stdout, "W1AW-1>\r") || !strings.Contains(stdout, "Commands:\r") || !strings.Contains(stdout, "Bye\r") {
		t.Fatalf("connect %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	cancel()
	select {
	case code = <-done:
		if code != 0 {
			t.Fatalf("serve exit %d %s", code, errb.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop")
	}
}

func TestSocketSettings(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	stdout, stderr, code := captureIn(context.Background(), []string{"sysop", "password", "--state", dir}, strings.NewReader("test-password-1\n"))
	if code != 0 {
		t.Fatalf("password %d %s %s", code, stdout, stderr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out lockedBuffer
	var errb lockedBuffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"serve", "--state", dir, "--listen", "127.0.0.1:0"}, strings.NewReader(""), &out, &errb)
	}()
	sock := filepath.Join(dir, "crossbeam.sock")
	deadline := time.Now().Add(8 * time.Second)
	for {
		if strings.Contains(out.String(), "certificate sha256 ") && dialUnix(sock) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("serve did not start: %s %s", out.String(), errb.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(out.String(), "https://127.0.0.1:") || !strings.Contains(out.String(), "unix socket "+sock) {
		t.Fatalf("banner %s", out.String())
	}
	stdout, stderr, code = capture([]string{"settings", "set", "--socket", sock, "station.name", "Socket home"})
	if code != 0 {
		t.Fatalf("set %d %s %s", code, stdout, stderr)
	}
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Settings(context.Background())
	st.Close()
	if err != nil || got["station.name"] != "Socket home" {
		t.Fatalf("stored %v %v", got, err)
	}
	_, stderr, code = capture([]string{"settings", "set", "--socket", sock, "admin.public", "yes"})
	if code != 1 || !strings.Contains(stderr, "true or false") {
		t.Fatalf("reject %d %s", code, stderr)
	}
	stdout, stderr, code = capture([]string{"status", "--socket", sock})
	if code != 0 || !strings.Contains(stdout, "crossbeam "+version) || !strings.Contains(stdout, "certificate sha256: ") {
		t.Fatalf("status %d %s %s", code, stdout, stderr)
	}
	cancel()
	select {
	case code = <-done:
		if code != 0 {
			t.Fatalf("serve exit %d %s", code, errb.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop")
	}
}

func TestBulletinCommand(t *testing.T) {
	_, stderr, code := capture([]string{"bulletin"})
	if code != 2 || !strings.Contains(stderr, "usage") {
		t.Fatalf("usage %d %s", code, stderr)
	}
	_, stderr, code = capture([]string{"bulletin", "send", "--socket", filepath.Join(t.TempDir(), "missing.sock"), "W1AW-1"})
	if code != 1 || !strings.Contains(stderr, "not running") {
		t.Fatalf("down %d %s", code, stderr)
	}

	slaveStore, err := store.Open(filepath.Join(t.TempDir(), "slave"))
	if err != nil {
		t.Fatal(err)
	}
	defer slaveStore.Close()
	if _, err := slaveStore.SaveArticle(context.Background(), store.ArticleIn{
		ID: "DAY", Title: "Day note", Author: "W1AW-1", Body: "From the far node.",
		Created: "2019-05-01T00:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}
	call, err := ax25.Parse("W1AW-1")
	if err != nil {
		t.Fatal(err)
	}
	remote, err := link.Listen(link.Config{
		Bind: "127.0.0.1:0", Local: call, T1: 200 * time.Millisecond, N2: 5, Paclen: 128,
		Handler: bulletin.Answer(slaveStore, call),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()

	dir := filepath.Join(t.TempDir(), "state")
	stdout, stderr, code := captureIn(context.Background(), []string{"sysop", "password", "--state", dir}, strings.NewReader("test-password-1\n"))
	if code != 0 {
		t.Fatalf("password %d %s %s", code, stdout, stderr)
	}
	body := strings.Repeat("A", 500)
	if _, stderr, code = capture([]string{"settings", "set", "--state", dir, "station.callsign", "K5LEB"}); code != 0 {
		t.Fatal(stderr)
	}
	if _, stderr, code = capture([]string{"settings", "set", "--state", dir, "link.peers", "W1AW-1@" + remote.LocalAddr().String()}); code != 0 {
		t.Fatal(stderr)
	}
	if _, stderr, code = capture([]string{"article", "save", "--state", dir, "--title", "Net tonight", "--author", "K5LEB", "--id", "NIGHT", "--created", "2020-01-02T03:04:05Z", "--body", body}); code != 0 {
		t.Fatal(stderr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out lockedBuffer
	var errb lockedBuffer
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"serve", "--state", dir, "--listen", "127.0.0.1:0", "--link", "127.0.0.1:0"}, strings.NewReader(""), &out, &errb)
	}()
	term := filepath.Join(dir, "terminal.sock")
	deadline := time.Now().Add(8 * time.Second)
	for {
		if strings.Contains(out.String(), "ax/udp 127.0.0.1:") && dialUnix(term) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("serve did not start the link: %s %s", out.String(), errb.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
	sendCtx, sendCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer sendCancel()
	stdout, stderr, code = captureIn(sendCtx, []string{"bulletin", "send", "--socket", term, "W1AW-1"}, strings.NewReader(""))
	if code != 0 || stdout != "sent 1 received 1\n" {
		t.Fatalf("send %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	got := mustTitle(t, slaveStore, "Net tonight")
	if got.ID != "NIGHT" || got.Author != "K5LEB" || got.Body != body || got.Created.Format(time.RFC3339) == "2020-01-02T03:04:05Z" {
		t.Fatalf("ingested %+v", got)
	}
	master, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	back := mustTitle(t, master, "Day note")
	master.Close()
	if back.ID != "DAY" || back.Author != "W1AW-1" || back.Body != "From the far node." || back.Created.Format(time.RFC3339) == "2019-05-01T00:00:00Z" {
		t.Fatalf("reverse %+v", back)
	}
	stdout, stderr, code = captureIn(sendCtx, []string{"bulletin", "send", "--socket", term, "W1AW-1"}, strings.NewReader(""))
	if code != 0 || stdout != "sent 0 received 0\n" {
		t.Fatalf("second %d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if n := countTitles(t, slaveStore); n != 2 {
		t.Fatalf("slave has %d", n)
	}
	cancel()
	select {
	case code = <-done:
		if code != 0 {
			t.Fatalf("serve exit %d %s", code, errb.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop")
	}
}

func mustTitle(t *testing.T, st *store.Store, title string) storeArticle {
	t.Helper()
	rows, err := st.AllArticles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Article.Title == title {
			return storeArticle{ID: row.Article.ID, Title: row.Article.Title, Author: row.Article.Author, Body: row.Article.Body, Created: row.Article.Created}
		}
	}
	t.Fatalf("missing %s", title)
	return storeArticle{}
}

func countTitles(t *testing.T, st *store.Store) int {
	t.Helper()
	rows, err := st.AllArticles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

type storeArticle struct {
	ID      string
	Title   string
	Author  string
	Body    string
	Created time.Time
}

func dialUnix(path string) bool {
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
