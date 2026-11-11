// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package meshcore

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bbeamer41/crossbeam/internal/store"
)

func TestContactListAndRead(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.SaveArticle(context.Background(), store.ArticleIn{
		Title:   "Net night notes",
		Author:  "K5LEB",
		Body:    "The Denton net meets on Thursday.",
		Created: "2026-11-09T18:04:05Z",
	}); err != nil {
		t.Fatal(err)
	}
	host, radio := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	scriptErr := make(chan error, 1)
	go func() {
		defer radio.Close()
		scriptErr <- radioScript(radio)
	}()
	err = Session(ctx, host, st, "K5LEB", nil)
	_ = host.Close()
	if serr := <-scriptErr; serr != nil {
		t.Fatal(serr)
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func radioScript(rw io.ReadWriter) error {
	if err := setupRadio(rw, "K5LEB"); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
		return err
	}
	channel := []byte{respChannelMsg, 0, 0, 0, 1, 0, 0, 0, 'L', 'I', 'S', 'T'}
	if err := writeFrame(rw, markFromRadio, channel); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
		return fmt.Errorf("channel was not ignored: %w", err)
	}
	prefix := []byte{1, 2, 3, 4, 5, 6}
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "LIST", 0, 11)); err != nil {
		return err
	}
	sent, err := expectCmd(rw, cmdSendText)
	if err != nil {
		return err
	}
	if err := checkText(sent, prefix, "Net night notes"); err != nil {
		return err
	}
	// A copy of the outbound text is not a new command. The READ that
	// follows it, in the same wait, still has to be answered.
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, string(sent[13:]), 0, 99)); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "READ 1", 0, 12)); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respMsgSent, 0}); err != nil {
		return err
	}
	sent, err = expectCmd(rw, cmdSendText)
	if err != nil {
		return err
	}
	if err := checkText(sent, prefix, "Title: Net night notes"); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respMsgSent, 0}); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respNoMore}); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{0x80, 1}); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "?", 0, 13)); err != nil {
		return err
	}
	sent, err = expectCmd(rw, cmdSendText)
	if err != nil {
		return err
	}
	if err := checkText(sent, prefix, "LIST"); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respMsgSent, 0}); err != nil {
		return err
	}
	// BYE's reply is the word Bye. A second BYE is still a command.
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "BYE", 0, 14)); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSendText); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respMsgSent, 0}); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "BYE", 0, 15)); err != nil {
		return err
	}
	sent, err = expectCmd(rw, cmdSendText)
	if err != nil {
		return err
	}
	if err := checkText(sent, prefix, "Bye"); err != nil {
		return err
	}
	return writeFrame(rw, markFromRadio, []byte{respMsgSent, 0})
}

func TestDeviceQueryErrorContinues(t *testing.T) {
	host, radio := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	scriptErr := make(chan error, 1)
	go func() {
		defer radio.Close()
		scriptErr <- queryErrorScript(radio)
	}()
	err := Session(ctx, host, nil, "BBS", nil)
	_ = host.Close()
	if serr := <-scriptErr; serr != nil {
		t.Fatal(serr)
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func TestTableFullRetriesOnce(t *testing.T) {
	host, radio := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	scriptErr := make(chan error, 1)
	go func() {
		defer radio.Close()
		scriptErr <- tableFullScript(radio)
	}()
	err := Session(ctx, host, nil, "BBS", nil)
	_ = host.Close()
	if serr := <-scriptErr; serr != nil {
		t.Fatal(serr)
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func queryErrorScript(rw io.ReadWriter) error {
	if err := setupRadioReply(rw, "BBS", []byte{respError, 1}, []byte{respOK}); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
		return fmt.Errorf("device query error ended the session: %w", err)
	}
	return writeFrame(rw, markFromRadio, []byte{respNoMore})
}

func tableFullScript(rw io.ReadWriter) error {
	if err := setupRadio(rw, "BBS"); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
		return err
	}
	prefix := []byte{1, 2, 3, 4, 5, 6}
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "LIST", 0, 21)); err != nil {
		return err
	}
	first, err := expectCmd(rw, cmdSendText)
	if err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respError, 3}); err != nil {
		return err
	}
	second, err := expectCmd(rw, cmdSendText)
	if err != nil {
		return fmt.Errorf("table full was not retried: %w", err)
	}
	if !bytes.Equal(first, second) {
		return fmt.Errorf("retry changed the send")
	}
	if err := writeFrame(rw, markFromRadio, []byte{respMsgSent, 0}); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
		return err
	}
	return writeFrame(rw, markFromRadio, []byte{respNoMore})
}

func TestContactLimits(t *testing.T) {
	host, radio := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	scriptErr := make(chan error, 1)
	go func() {
		defer radio.Close()
		scriptErr <- limitScript(radio)
	}()
	err := Session(ctx, host, nil, "BBS", nil)
	_ = host.Close()
	if serr := <-scriptErr; serr != nil {
		t.Fatal(serr)
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func limitScript(rw io.ReadWriter) error {
	if err := setupRadio(rw, "BBS"); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respNoMore}); err != nil {
		return err
	}
	prefix := []byte{1, 2, 3, 4, 5, 6}
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "LIST\x00", 0, 1)); err != nil {
		return err
	}
	if err := expectQuiet(rw); err != nil {
		return fmt.Errorf("nul command: %w", err)
	}
	long := strings.Repeat("A", maxTextBytes+1)
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, long, 0, 2)); err != nil {
		return err
	}
	if err := expectQuiet(rw); err != nil {
		return fmt.Errorf("long command: %w", err)
	}
	for i := 0; i < meshPerMinute; i++ {
		if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "LIST", 0, uint32(10+i))); err != nil {
			return err
		}
		if _, err := expectCmd(rw, cmdSendText); err != nil {
			return err
		}
		if err := writeFrame(rw, markFromRadio, []byte{respMsgSent, 0}); err != nil {
			return err
		}
	}
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "LIST", 0, 1000)); err != nil {
		return err
	}
	if err := expectQuiet(rw); err != nil {
		return fmt.Errorf("past the per-station cap: %w", err)
	}
	return nil
}

func TestClockErrorStillSyncs(t *testing.T) {
	host, radio := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	scriptErr := make(chan error, 1)
	go func() {
		defer radio.Close()
		scriptErr <- clockErrorScript(radio)
	}()
	err := Session(ctx, host, nil, "BBS", nil)
	_ = host.Close()
	if serr := <-scriptErr; serr != nil {
		t.Fatal(serr)
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func clockErrorScript(rw io.ReadWriter) error {
	if err := setupRadioReply(rw, "BBS", []byte{respDeviceInfo, 3}, []byte{respError, 6}); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
		return fmt.Errorf("clock error ended the session: %w", err)
	}
	return writeFrame(rw, markFromRadio, []byte{respNoMore})
}

func TestPlainMessageGetsAGreeting(t *testing.T) {
	host, radio := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	scriptErr := make(chan error, 1)
	go func() {
		defer radio.Close()
		scriptErr <- greetingScript(radio)
	}()
	err := Session(ctx, host, nil, "K5LEB", nil)
	_ = host.Close()
	if serr := <-scriptErr; serr != nil {
		t.Fatal(serr)
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func greetingScript(rw io.ReadWriter) error {
	if err := setupRadio(rw, "K5LEB"); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respNoMore}); err != nil {
		return err
	}
	prefix := []byte{1, 2, 3, 4, 5, 6}
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "hello", 0, 40)); err != nil {
		return err
	}
	sent, err := expectCmd(rw, cmdSendText)
	if err != nil {
		return err
	}
	text := string(sent[13:])
	if strings.Contains(text, "Unknown command") || !strings.Contains(text, "K5LEB") || !strings.Contains(text, "LIST") || !strings.Contains(text, "READ") {
		return fmt.Errorf("greeting %q", text)
	}
	if err := writeFrame(rw, markFromRadio, []byte{respMsgSent, 0}); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "help", 0, 41)); err != nil {
		return err
	}
	sent, err = expectCmd(rw, cmdSendText)
	if err != nil {
		return err
	}
	if err := checkText(sent, prefix, "This list"); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respMsgSent, 0}); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "LIST", 0, 42)); err != nil {
		return err
	}
	sent, err = expectCmd(rw, cmdSendText)
	if err != nil {
		return err
	}
	if err := checkText(sent, prefix, "not available"); err != nil {
		return err
	}
	return writeFrame(rw, markFromRadio, []byte{respMsgSent, 0})
}

func TestChatAdvertIsAdded(t *testing.T) {
	host, radio := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	scriptErr := make(chan error, 1)
	go func() {
		defer radio.Close()
		scriptErr <- advertScript(radio)
	}()
	err := Session(ctx, host, nil, "K5LEB", nil)
	_ = host.Close()
	if serr := <-scriptErr; serr != nil {
		t.Fatal(serr)
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func advertScript(rw io.ReadWriter) error {
	if err := setupRadio(rw, "K5LEB"); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respNoMore}); err != nil {
		return err
	}
	repeater := bytes.Repeat([]byte{0x22}, contactPushLen)
	repeater[0] = pushNewAdvert
	repeater[advertTypeAt] = 2
	if err := writeFrame(rw, markFromRadio, repeater); err != nil {
		return err
	}
	if err := expectQuiet(rw); err != nil {
		return fmt.Errorf("repeater advert was added: %w", err)
	}
	chat := bytes.Repeat([]byte{0x5A}, contactPushLen)
	chat[0] = pushNewAdvert
	chat[advertTypeAt] = advTypeChat
	if err := writeFrame(rw, markFromRadio, chat); err != nil {
		return err
	}
	if err := expectAdded(rw, chat); err != nil {
		return err
	}
	prefix := []byte{1, 2, 3, 4, 5, 6}
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, "LIST", 0, 50)); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSendText); err != nil {
		return err
	}
	held := bytes.Repeat([]byte{0x44}, contactPushLen)
	held[0] = pushNewAdvert
	held[advertTypeAt] = advTypeChat
	if err := writeFrame(rw, markFromRadio, held); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respMsgSent, 0}); err != nil {
		return err
	}
	return expectAdded(rw, held)
}

func expectAdded(rw io.ReadWriter, chat []byte) error {
	got, err := expectCmd(rw, cmdAddContact)
	if err != nil {
		return err
	}
	if len(got) != len(chat) || got[advertTypeAt] != advTypeChat || !bytes.Equal(got[1:], chat[1:]) {
		return fmt.Errorf("contact add changed the advert")
	}
	return writeFrame(rw, markFromRadio, []byte{respOK})
}

func TestFloodAdvertWaits(t *testing.T) {
	ann := &announcer{}
	host, radio := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	scriptErr := make(chan error, 1)
	go func() {
		defer radio.Close()
		scriptErr <- floodOnceScript(radio)
	}()
	err := Session(ctx, host, nil, "K5LEB", ann)
	_ = host.Close()
	if serr := <-scriptErr; serr != nil {
		t.Fatal(serr)
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
	host, radio = net.Pipe()
	ctx, cancel = context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	scriptErr = make(chan error, 1)
	go func() {
		defer radio.Close()
		scriptErr <- noFloodScript(radio)
	}()
	err = Session(ctx, host, nil, "K5LEB", ann)
	_ = host.Close()
	if serr := <-scriptErr; serr != nil {
		t.Fatal(serr)
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func floodOnceScript(rw io.ReadWriter) error {
	if err := setupRadio(rw, "K5LEB"); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
		return err
	}
	return writeFrame(rw, markFromRadio, []byte{respNoMore})
}

func noFloodScript(rw io.ReadWriter) error {
	if _, err := expectCmd(rw, cmdAppStart); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respSelfInfo, 0}); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdDeviceQuery); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respDeviceInfo, 3}); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSetTime); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respOK}); err != nil {
		return err
	}
	got, err := expectCmd(rw, cmdSetAdvertName)
	if err != nil {
		return err
	}
	if string(got[1:]) != "K5LEB" {
		return fmt.Errorf("advert name %q", got[1:])
	}
	if err := writeFrame(rw, markFromRadio, []byte{respOK}); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
		return fmt.Errorf("advert was sent again: %w", err)
	}
	return writeFrame(rw, markFromRadio, []byte{respNoMore})
}

func setupRadio(rw io.ReadWriter, name string) error {
	return setupRadioReply(rw, name, []byte{respDeviceInfo, 3}, []byte{respOK})
}

func setupRadioReply(rw io.ReadWriter, name string, device, clock []byte) error {
	if _, err := expectCmd(rw, cmdAppStart); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respSelfInfo, 0}); err != nil {
		return err
	}
	if _, err := expectCmd(rw, cmdDeviceQuery); err != nil {
		return err
	}
	if err := writeFrame(rw, markFromRadio, device); err != nil {
		return err
	}
	got, err := expectCmd(rw, cmdSetTime)
	if err != nil {
		return err
	}
	if len(got) != 5 {
		return fmt.Errorf("set time %x", got)
	}
	secs := binary.LittleEndian.Uint32(got[1:])
	now := uint32(time.Now().Unix())
	if secs+30 < now || secs > now+30 {
		return fmt.Errorf("set time %d", secs)
	}
	if err := writeFrame(rw, markFromRadio, clock); err != nil {
		return err
	}
	got, err = expectCmd(rw, cmdSetAdvertName)
	if err != nil {
		return err
	}
	if string(got[1:]) != name {
		return fmt.Errorf("advert name %q", got[1:])
	}
	if err := writeFrame(rw, markFromRadio, []byte{respOK}); err != nil {
		return err
	}
	got, err = expectCmd(rw, cmdSelfAdvert)
	if err != nil {
		return err
	}
	if len(got) != 2 || got[1] != 1 {
		return fmt.Errorf("flood advert %x", got)
	}
	return writeFrame(rw, markFromRadio, []byte{respOK})
}

func expectQuiet(rw io.ReadWriter) error {
	conn, ok := rw.(net.Conn)
	if !ok {
		return fmt.Errorf("need a connection")
	}
	if err := conn.SetReadDeadline(time.Now().Add(400 * time.Millisecond)); err != nil {
		return err
	}
	defer conn.SetReadDeadline(time.Time{})
	frame, err := readFrame(rw, markToRadio)
	if err == nil {
		return fmt.Errorf("unexpected reply %x", frame)
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return nil
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return nil
	}
	return err
}

func expectCmd(r io.Reader, code byte) ([]byte, error) {
	frame, err := readFrame(r, markToRadio)
	if err != nil {
		return nil, err
	}
	if len(frame) == 0 || frame[0] != code {
		return frame, fmt.Errorf("command %d, got %x", code, frame)
	}
	return frame, nil
}

func checkText(frame, prefix []byte, want string) error {
	if len(frame) < 13 || !bytes.Equal(frame[7:13], prefix) {
		return fmt.Errorf("send prefix %x", frame)
	}
	text := string(frame[13:])
	if len(text) > maxTextBytes || utf8.RuneCountInString(text) > maxTextRunes {
		return fmt.Errorf("text is %d bytes", len(text))
	}
	if !strings.Contains(text, want) {
		return fmt.Errorf("text %q", text)
	}
	return nil
}
