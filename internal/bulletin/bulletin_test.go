// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package bulletin

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/article"
	"github.com/bbeamer41/crossbeam/internal/ax25"
	"github.com/bbeamer41/crossbeam/internal/fbb"
	"github.com/bbeamer41/crossbeam/internal/link"
	"github.com/bbeamer41/crossbeam/internal/settings"
	"github.com/bbeamer41/crossbeam/internal/store"
	"github.com/bbeamer41/crossbeam/internal/version"
)

func TestBothDirections(t *testing.T) {
	stA := openStore(t)
	stB := openStore(t)
	old := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	saveArt(t, stA, "NIGHT", "Net tonight", "K5LEB", "Meet at seven.\n\nBring the list.", old)
	saveArt(t, stB, "DAY", "Day note", "W1AW-1", "Day body.", time.Date(2019, 5, 1, 0, 0, 0, 0, time.UTC))
	res := exchange(t, stA, stB, "K5LEB", "W1AW-1")
	if res.Sent != 1 || res.Received != 1 {
		t.Fatalf("result %+v", res)
	}
	got := findArt(t, stB, "Net tonight")
	if got.Article.ID != "NIGHT" || got.Article.Author != "K5LEB" || got.Article.Body != "Meet at seven.\n\nBring the list." {
		t.Fatalf("ingested %+v", got.Article)
	}
	if got.Article.Created.Equal(old) || time.Since(got.Article.Created) > time.Minute {
		t.Fatalf("created was copied from the sender: %s", got.Article.Created)
	}
	back := findArt(t, stA, "Day note")
	if back.Article.ID != "DAY" || back.Article.Author != "W1AW-1" || back.Article.Body != "Day body." {
		t.Fatalf("reverse %+v", back.Article)
	}
	if n := countArt(t, stA); n != 2 {
		t.Fatalf("A has %d", n)
	}
	if n := countArt(t, stB); n != 2 {
		t.Fatalf("B has %d", n)
	}
	again := exchange(t, stA, stB, "K5LEB", "W1AW-1")
	if again.Sent != 0 || again.Received != 0 {
		t.Fatalf("second %+v", again)
	}
	if n := countArt(t, stA); n != 2 {
		t.Fatalf("A grew to %d", n)
	}
	if n := countArt(t, stB); n != 2 {
		t.Fatalf("B grew to %d", n)
	}
}

func TestMoreThanOneBlock(t *testing.T) {
	stA := openStore(t)
	stB := openStore(t)
	for i := 1; i <= 6; i++ {
		saveArt(t, stA, fmt.Sprintf("A%d", i), fmt.Sprintf("From A %d", i), "K5LEB", "a", time.Now().UTC())
		saveArt(t, stB, fmt.Sprintf("B%d", i), fmt.Sprintf("From B %d", i), "W1AW", "b", time.Now().UTC())
	}
	res := exchange(t, stA, stB, "K5LEB", "W1AW")
	if res.Sent != 6 || res.Received != 6 {
		t.Fatalf("result %+v", res)
	}
	if n := countArt(t, stA); n != 12 {
		t.Fatalf("A has %d", n)
	}
	if n := countArt(t, stB); n != 12 {
		t.Fatalf("B has %d", n)
	}
}

func TestLargeMessageAndGeneratedID(t *testing.T) {
	stA := openStore(t)
	stB := openStore(t)
	body := strings.Repeat("A", fbb.MaxBlock+20)
	saveArt(t, stA, "this-id-is-way-too-long", "Generated", "Net Control", body, time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC))
	res := exchange(t, stA, stB, "K5LEB", "W1AW")
	if res.Sent != 1 || res.Received != 0 {
		t.Fatalf("result %+v", res)
	}
	got := findArt(t, stB, "Generated")
	if got.Article.Author != "K5LEB" || got.Article.Body != body {
		t.Fatalf("stored author %q body %d", got.Article.Author, len(got.Article.Body))
	}
	if !strings.HasSuffix(got.Article.ID, "_K5LEB") || len(got.Article.ID) > fbb.MaxBID || got.Article.ID == "THIS-ID-IS-WAY-TOO-LONG" {
		t.Fatalf("bid %q", got.Article.ID)
	}
	if countArt(t, stA) != 1 {
		t.Fatal("origin stored the echo")
	}
	again := exchange(t, stA, stB, "K5LEB", "W1AW")
	if again.Sent != 0 || again.Received != 0 || countArt(t, stA) != 1 || countArt(t, stB) != 1 {
		t.Fatalf("second %+v A %d B %d", again, countArt(t, stA), countArt(t, stB))
	}
}

func TestLongTitleStopsTheForward(t *testing.T) {
	st := openStore(t)
	saveArt(t, st, "LONG", strings.Repeat("T", fbb.MaxSubjectRunes+1), "K5LEB", "x", time.Now().UTC())
	_, err := Forward(context.Background(), st, mustCall(t, "K5LEB"), nil)
	if err == nil || !strings.Contains(err.Error(), "bulletin limit") {
		t.Fatal(err)
	}
}

func TestScriptedPeerReceives(t *testing.T) {
	st := openStore(t)
	if _, err := st.Apply(context.Background(), map[string]string{
		settings.KeyBulletinTo: "texas",
		settings.KeyBulletinAt: "k5leb.#dfw.tx",
	}); err != nil {
		t.Fatal(err)
	}
	saveArt(t, st, "NIGHT", "Net tonight", "K5LEB", "Meet at 7.", time.Now().UTC())
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	errc := make(chan error, 1)
	go func() {
		defer b.Close()
		rd := bufio.NewReader(b)
		if _, err := io.WriteString(b, "[TEST-1-F$]\r\nWelcome\r\nW1AW>\r\n"); err != nil {
			errc <- err
			return
		}
		var proposals []string
		var end string
		for {
			line, err := readCR(rd)
			if err != nil {
				errc <- err
				return
			}
			if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "F>") {
				end = line
				break
			}
			if strings.HasPrefix(line, "[") {
				continue
			}
			proposals = append(proposals, line)
		}
		if len(proposals) != 1 || !strings.Contains(proposals[0], " TEXAS ") || !strings.Contains(proposals[0], " K5LEB.#DFW.TX ") || !strings.Contains(proposals[0], " NIGHT ") {
			errc <- fmt.Errorf("proposal %q end %q", proposals, end)
			return
		}
		if _, err := io.WriteString(b, "FS +\r"); err != nil {
			errc <- err
			return
		}
		size := lastField(proposals[0])
		buf := make([]byte, size)
		if _, err := io.ReadFull(rd, buf); err != nil {
			errc <- err
			return
		}
		if !bytes.HasPrefix(buf, []byte("Net tonight\r")) || buf[len(buf)-1] != fbb.CtrlZ || !bytes.Contains(buf, []byte("Meet at 7.")) {
			errc <- fmt.Errorf("message %q", buf)
			return
		}
		if _, err := io.WriteString(b, "FQ\r"); err != nil {
			errc <- err
			return
		}
		errc <- nil
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := Forward(ctx, st, mustCall(t, "K5LEB"), a)
	a.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.Sent != 1 || res.Received != 0 {
		t.Fatalf("%+v", res)
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

func TestScriptedIngest(t *testing.T) {
	st := openStore(t)
	msg := []byte("Hello net\rMeet at seven.\r\rBring the list.\x1a")
	line := fmt.Sprintf("FB B W1AW WW ALL HELLO-1 %d", len(msg))
	sum := fbb.Checksum([]string{line})
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	errc := make(chan error, 1)
	go func() {
		errc <- Serve(context.Background(), st, mustCall(t, "K5LEB"), b)
	}()
	rd := bufio.NewReader(a)
	if err := readUntilPrompt(rd); err != nil {
		t.Fatal(err)
	}
	payload := "[TEST-1-F$]\r; station\r" + line + "\r" + fmt.Sprintf("F> %02X\r", sum)
	if _, err := io.WriteString(a, payload); err != nil {
		t.Fatal(err)
	}
	fs, err := readCR(rd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(fs) != "FS +" {
		t.Fatalf("fs %q", fs)
	}
	if _, err := a.Write(msg); err != nil {
		t.Fatal(err)
	}
	next, err := readCR(rd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(next) != "FF" {
		t.Fatalf("after body %q", next)
	}
	if _, err := io.WriteString(a, "FQ\r"); err != nil {
		t.Fatal(err)
	}
	a.Close()
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	got := findArt(t, st, "Hello net")
	if got.Article.ID != "HELLO-1" || got.Article.Author != "W1AW" || got.Article.Body != "Meet at seven.\n\nBring the list." {
		t.Fatalf("%+v", got.Article)
	}
	if time.Since(got.Article.Created) > time.Minute {
		t.Fatalf("created %s", got.Article.Created)
	}
}

func TestBadChecksumAndPrivate(t *testing.T) {
	st := openStore(t)
	line := "FB B W1AW WW ALL NEW-1 8"
	sum := fbb.Checksum([]string{line}) ^ 0xFF
	a, b := net.Pipe()
	defer a.Close()
	errc := make(chan error, 1)
	go func() {
		errc <- Serve(context.Background(), st, mustCall(t, "K5LEB"), b)
	}()
	rd := bufio.NewReader(a)
	if err := readUntilPrompt(rd); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(a, "[TEST-1-F$]\r"+line+"\r"+fmt.Sprintf("F> %02X\r", sum)); err != nil {
		t.Fatal(err)
	}
	reply, err := readCR(rd)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply, "bad checksum") {
		t.Fatalf("reply %q", reply)
	}
	a.Close()
	if err := <-errc; err == nil || !strings.Contains(err.Error(), "bad checksum") {
		t.Fatal(err)
	}
	if countArt(t, st) != 0 {
		t.Fatal("checksum failure was stored")
	}

	st = openStore(t)
	a, b = net.Pipe()
	defer a.Close()
	go func() {
		errc <- Serve(context.Background(), st, mustCall(t, "K5LEB"), b)
	}()
	rd = bufio.NewReader(a)
	if err := readUntilPrompt(rd); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(a, "[TEST-1-F$]\rFB P K5LEB WW K5LEB NOTE 8\rF>\r"); err != nil {
		t.Fatal(err)
	}
	fs, err := readCR(rd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(fs) != "FS -" {
		t.Fatalf("private fs %q", fs)
	}
	next, err := readCR(rd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(next) != "FF" {
		t.Fatalf("after private %q", next)
	}
	if _, err := io.WriteString(a, "FQ\r"); err != nil {
		t.Fatal(err)
	}
	a.Close()
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if countArt(t, st) != 0 {
		t.Fatal("private mail was stored")
	}
}

func TestDeferAndMixedResponse(t *testing.T) {
	st := openStore(t)
	saveArt(t, st, "LATER", "Later", "K5LEB", "not yet", time.Now().UTC())
	if sent := scriptedMarks(t, st, "FS =\r", nil); sent != 0 {
		t.Fatalf("deferred sent %d", sent)
	}
	var got []byte
	sent := scriptedMarks(t, st, "FS +\r", func(buf []byte) { got = append([]byte(nil), buf...) })
	if sent != 1 || !bytes.HasPrefix(got, []byte("Later\r")) || got[len(got)-1] != fbb.CtrlZ {
		t.Fatalf("sent %d msg %q", sent, got)
	}

	st = openStore(t)
	saveArt(t, st, "ONE", "First", "K5LEB", "one", time.Now().UTC())
	saveArt(t, st, "TWO", "Second", "K5LEB", "two", time.Now().UTC())
	got = nil
	sent = scriptedMarks(t, st, "FS -+\r", func(buf []byte) { got = append([]byte(nil), buf...) })
	if sent != 1 || !bytes.HasPrefix(got, []byte("Second\r")) || bytes.Contains(got, []byte("First\r")) {
		t.Fatalf("mixed sent %d msg %q", sent, got)
	}
}

func TestHumanPrompt(t *testing.T) {
	st := openStore(t)
	a, b := net.Pipe()
	defer a.Close()
	errc := make(chan error, 1)
	go func() {
		errc <- Serve(context.Background(), st, mustCall(t, "K5LEB"), b)
	}()
	rd := bufio.NewReader(a)
	sid, err := readCR(rd)
	if err != nil {
		t.Fatal(err)
	}
	wantSID, err := fbb.SID(version.String)
	if err != nil || sid != wantSID {
		t.Fatalf("sid %q %v", sid, err)
	}
	welcome, err := readCR(rd)
	if err != nil || welcome != "Crossbeam K5LEB" {
		t.Fatalf("welcome %q %v", welcome, err)
	}
	prompt, err := readCR(rd)
	if err != nil || prompt != "K5LEB>" {
		t.Fatalf("prompt %q %v", prompt, err)
	}
	if _, err := io.WriteString(a, "?\r"); err != nil {
		t.Fatal(err)
	}
	var help string
	for {
		line, err := readCR(rd)
		if err != nil {
			t.Fatal(err)
		}
		help += line + "\n"
		if line == "K5LEB>" {
			break
		}
	}
	if !strings.Contains(help, "Commands:") || !strings.Contains(help, "BYE") {
		t.Fatalf("help %q", help)
	}
	if _, err := io.WriteString(a, "BYE\r"); err != nil {
		t.Fatal(err)
	}
	bye, err := readCR(rd)
	if err != nil || bye != "Bye" {
		t.Fatalf("bye %q %v", bye, err)
	}
	a.Close()
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

func TestForwardOverAXUDP(t *testing.T) {
	stA := openStore(t)
	stB := openStore(t)
	body := strings.Repeat("B", 300)
	saveArt(t, stA, "NIGHT", "Net tonight", "K5LEB", body, time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC))
	saveArt(t, stB, "DAY", "Day note", "W1AW-1", "From the far node.", time.Now().UTC())
	localA := mustCall(t, "K5LEB")
	localB := mustCall(t, "W1AW-1")
	slave, err := link.Listen(link.Config{
		Bind: "127.0.0.1:0", Local: localB, T1: 150 * time.Millisecond, N2: 6, Paclen: 64,
		Handler: Answer(stB, localB),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer slave.Close()
	master, err := link.Listen(link.Config{
		Bind: "127.0.0.1:0", Local: localA, T1: 150 * time.Millisecond, N2: 6, Paclen: 64,
		TerminalPath: t.TempDir() + "/terminal.sock", TerminalMode: 0o600,
		Handler: Answer(stA, localA),
		Forward: func(ctx context.Context, stream *ax25.Stream) (string, error) {
			res, err := Forward(ctx, stA, localA, stream)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("sent %d received %d", res.Sent, res.Received), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	if err := master.SetPeers(localB.String() + "@" + slave.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("unix", master.Terminal())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	if _, err := fmt.Fprintf(conn, "FORWARD %s\n", localB); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if reply != "OK sent 1 received 1\n" {
		t.Fatalf("reply %q", reply)
	}
	got := findArt(t, stB, "Net tonight")
	if got.Article.Body != body || got.Article.ID != "NIGHT" || got.Article.Author != "K5LEB" {
		t.Fatalf("slave %+v", got.Article)
	}
	back := findArt(t, stA, "Day note")
	if back.Article.ID != "DAY" || back.Article.Author != "W1AW-1" || back.Article.Body != "From the far node." {
		t.Fatalf("master %+v", back.Article)
	}
}

func scriptedMarks(t *testing.T, st *store.Store, fs string, take func([]byte)) int {
	t.Helper()
	a, b := net.Pipe()
	defer a.Close()
	errc := make(chan error, 1)
	go func() {
		defer b.Close()
		rd := bufio.NewReader(b)
		if _, err := io.WriteString(b, "[TEST-1-F$]\rW1AW>\r"); err != nil {
			errc <- err
			return
		}
		var size int
		var saw bool
		for {
			line, err := readCR(rd)
			if err != nil {
				errc <- err
				return
			}
			if strings.HasPrefix(line, "FB ") {
				size = lastField(line)
				saw = true
				continue
			}
			if strings.HasPrefix(strings.ToUpper(line), "F>") {
				break
			}
		}
		if !saw {
			errc <- fmt.Errorf("no proposal")
			return
		}
		if _, err := io.WriteString(b, fs); err != nil {
			errc <- err
			return
		}
		if strings.Contains(fs, "+") && take != nil {
			buf := make([]byte, size)
			if _, err := io.ReadFull(rd, buf); err != nil {
				errc <- err
				return
			}
			take(buf)
		}
		_, err := io.WriteString(b, "FQ\r")
		errc <- err
	}()
	res, err := Forward(context.Background(), st, mustCall(t, "K5LEB"), a)
	a.Close()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	return res.Sent
}

func exchange(t *testing.T, stA, stB *store.Store, callA, callB string) Result {
	t.Helper()
	a, b := net.Pipe()
	errc := make(chan error, 1)
	go func() {
		errc <- Serve(context.Background(), stB, mustCall(t, callB), b)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type got struct {
		res Result
		err error
	}
	ch := make(chan got, 1)
	go func() {
		res, err := Forward(ctx, stA, mustCall(t, callA), a)
		ch <- got{res, err}
	}()
	var res Result
	select {
	case item := <-ch:
		if item.err != nil {
			a.Close()
			b.Close()
			t.Fatal(item.err)
		}
		res = item.res
	case <-ctx.Done():
		a.Close()
		b.Close()
		t.Fatal("forward timed out")
	}
	a.Close()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		b.Close()
		t.Fatal("answer timed out")
	}
	return res
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func saveArt(t *testing.T, st *store.Store, id, title, author, body string, created time.Time) {
	t.Helper()
	_, err := st.SaveArticle(context.Background(), store.ArticleIn{
		ID: id, Title: title, Author: author, Body: body, Created: created.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func findArt(t *testing.T, st *store.Store, title string) store.SavedArticle {
	t.Helper()
	rows, err := st.AllArticles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Article.Title == title {
			return row
		}
	}
	t.Fatalf("missing %s", title)
	return store.SavedArticle{}
}

func countArt(t *testing.T, st *store.Store) int {
	t.Helper()
	rows, err := st.AllArticles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

func mustCall(t *testing.T, call string) ax25.Address {
	t.Helper()
	addr, err := ax25.Parse(call)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}

func readCR(r *bufio.Reader) (string, error) {
	var b []byte
	for {
		c, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if c == '\n' {
			continue
		}
		if c == '\r' {
			return string(b), nil
		}
		b = append(b, c)
		if len(b) > 4000 {
			return "", fmt.Errorf("line is too long")
		}
	}
}

func readUntilPrompt(r *bufio.Reader) error {
	saw := false
	for i := 0; i < 10; i++ {
		line, err := readCR(r)
		if err != nil {
			return err
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			saw = true
		}
		if strings.HasSuffix(line, ">") && saw {
			return nil
		}
	}
	return fmt.Errorf("no prompt")
}

func lastField(line string) int {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return 0
	}
	n := 0
	fmt.Sscanf(fields[len(fields)-1], "%d", &n)
	return n
}

func TestMessageCapMatchesArticle(t *testing.T) {
	if fbb.MaxMessage != article.MaxBodyBytes {
		t.Fatalf("caps %d %d", fbb.MaxMessage, article.MaxBodyBytes)
	}
}

func TestFramedMessageNamesTheArticle(t *testing.T) {
	st := openStore(t)
	saveArt(t, st, "BIG", "Hi", "K5LEB", strings.Repeat("A", article.MaxBodyBytes), time.Now().UTC())
	_, err := Forward(context.Background(), st, mustCall(t, "K5LEB"), nil)
	if err == nil || !strings.Contains(err.Error(), "article") || !strings.Contains(err.Error(), "bulletin limit") {
		t.Fatal(err)
	}
	msg := messageLimit(1<<62, fbb.MaxMessage+800)
	if len(msg) > 80 || !safeWire(msg) {
		t.Fatalf("wire error %d %q", len(msg), msg)
	}
}

func TestRemoteTitleErrorIsReturned(t *testing.T) {
	stA := openStore(t)
	stB := openStore(t)
	saveArt(t, stB, "LONG", strings.Repeat("T", fbb.MaxSubjectRunes+1), "W1AW", "x", time.Now().UTC())
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	errc := make(chan error, 1)
	go func() {
		errc <- Serve(context.Background(), stB, mustCall(t, "W1AW"), b)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch := make(chan error, 1)
	go func() {
		_, err := Forward(ctx, stA, mustCall(t, "K5LEB"), a)
		ch <- err
	}()
	var err error
	select {
	case err = <-ch:
	case <-ctx.Done():
		a.Close()
		b.Close()
		t.Fatal("timed out")
	}
	if err == nil || !strings.Contains(err.Error(), "bulletin limit") {
		t.Fatalf("master %v", err)
	}
	a.Close()
	if serveErr := <-errc; serveErr == nil || !strings.Contains(serveErr.Error(), "bulletin limit") {
		t.Fatalf("answer %v", serveErr)
	}
}

func TestPeerErrorIsSanitized(t *testing.T) {
	st := openStore(t)
	a, b := net.Pipe()
	defer a.Close()
	errc := make(chan error, 1)
	go func() {
		defer b.Close()
		rd := bufio.NewReader(b)
		if _, err := io.WriteString(b, "[TEST-1-F$]\rW1AW>\r"); err != nil {
			errc <- err
			return
		}
		// SID, then FF. Read both before the error so the writes do not block each other.
		for i := 0; i < 2; i++ {
			if _, err := readCR(rd); err != nil {
				errc <- err
				return
			}
		}
		_, err := io.WriteString(b, "*** open /var/lib/crossbeam/crossbeam.db\r")
		errc <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch := make(chan error, 1)
	go func() {
		_, err := Forward(ctx, st, mustCall(t, "K5LEB"), a)
		ch <- err
	}()
	var err error
	select {
	case err = <-ch:
	case <-ctx.Done():
		a.Close()
		b.Close()
		t.Fatal("timed out")
	}
	a.Close()
	if err == nil || err.Error() != "bad proposal" {
		t.Fatalf("master %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if oneLine(errors.New("bad checksum")) != "bad checksum" {
		t.Fatal(oneLine(errors.New("bad checksum")))
	}
}

func TestCRLFMessageStoresOneParagraph(t *testing.T) {
	st := openStore(t)
	msg := []byte("Hello net\r\nline one\r\nline two\x1a")
	line := fmt.Sprintf("FB B W1AW WW ALL LINES %d", len(msg))
	sum := fbb.Checksum([]string{line})
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	errc := make(chan error, 1)
	go func() {
		errc <- Serve(context.Background(), st, mustCall(t, "K5LEB"), b)
	}()
	rd := bufio.NewReader(a)
	if err := readUntilPrompt(rd); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(a, "[TEST-1-F$]\r"+line+"\r"+fmt.Sprintf("F> %02X\r", sum)); err != nil {
		t.Fatal(err)
	}
	fs, err := readCR(rd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(fs) != "FS +" {
		t.Fatalf("fs %q", fs)
	}
	if _, err := a.Write(msg); err != nil {
		t.Fatal(err)
	}
	next, err := readCR(rd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(next) != "FF" {
		t.Fatalf("after %q", next)
	}
	if _, err := io.WriteString(a, "FQ\r"); err != nil {
		t.Fatal(err)
	}
	a.Close()
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	got := findArt(t, st, "Hello net")
	if got.Article.Body != "line one line two" {
		t.Fatalf("%q", got.Article.Body)
	}
}

func TestUTF8BodySize(t *testing.T) {
	stA := openStore(t)
	stB := openStore(t)
	saveArt(t, stA, "CAFE", "Cafe", "K5LEB", "café", time.Now().UTC())
	res := exchange(t, stA, stB, "K5LEB", "W1AW")
	if res.Sent != 1 || res.Received != 0 {
		t.Fatalf("%+v", res)
	}
	got := findArt(t, stB, "Cafe")
	if got.Article.Body != "café" || got.Article.ID != "CAFE" {
		t.Fatalf("%+v", got.Article)
	}
}

func TestStoreFullDefers(t *testing.T) {
	oldN, oldB := maxStoreArticles, maxStoreBytes
	maxStoreArticles = 1
	t.Cleanup(func() {
		maxStoreArticles = oldN
		maxStoreBytes = oldB
	})
	st := openStore(t)
	msg := []byte("Hello\rOne.\x1a")
	line1 := fmt.Sprintf("FB B W1AW WW ALL ONE %d", len(msg))
	line2 := fmt.Sprintf("FB B W1AW WW ALL TWO %d", len(msg))
	sum := fbb.Checksum([]string{line1, line2})
	a, b := net.Pipe()
	defer a.Close()
	errc := make(chan error, 1)
	go func() {
		errc <- Serve(context.Background(), st, mustCall(t, "K5LEB"), b)
	}()
	rd := bufio.NewReader(a)
	if err := readUntilPrompt(rd); err != nil {
		t.Fatal(err)
	}
	payload := "[TEST-1-F$]\r" + line1 + "\r" + line2 + "\r" + fmt.Sprintf("F> %02X\r", sum)
	if _, err := io.WriteString(a, payload); err != nil {
		t.Fatal(err)
	}
	fs, err := readCR(rd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(fs) != "FS +=" {
		t.Fatalf("fs %q", fs)
	}
	if _, err := a.Write(msg); err != nil {
		t.Fatal(err)
	}
	next, err := readCR(rd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(next) != "FF" {
		t.Fatalf("after %q", next)
	}
	if _, err := io.WriteString(a, "FQ\r"); err != nil {
		t.Fatal(err)
	}
	a.Close()
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if countArt(t, st) != 1 {
		t.Fatalf("stored %d", countArt(t, st))
	}
	got := findArt(t, st, "Hello")
	if got.Article.ID != "ONE" || got.Article.Body != "One." {
		t.Fatalf("%+v", got.Article)
	}

	maxStoreBytes = 1
	st = openStore(t)
	line := fmt.Sprintf("FB B W1AW WW ALL TINY %d", len(msg))
	sum = fbb.Checksum([]string{line})
	a, b = net.Pipe()
	defer a.Close()
	go func() {
		errc <- Serve(context.Background(), st, mustCall(t, "K5LEB"), b)
	}()
	rd = bufio.NewReader(a)
	if err := readUntilPrompt(rd); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(a, "[TEST-1-F$]\r"+line+"\r"+fmt.Sprintf("F> %02X\r", sum)); err != nil {
		t.Fatal(err)
	}
	fs, err = readCR(rd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(fs) != "FS =" {
		t.Fatalf("byte cap %q", fs)
	}
	next, err = readCR(rd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(next) != "FF" {
		t.Fatalf("after byte cap %q", next)
	}
	if _, err := io.WriteString(a, "FQ\r"); err != nil {
		t.Fatal(err)
	}
	a.Close()
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if countArt(t, st) != 0 {
		t.Fatal("byte cap stored a bulletin")
	}
}
