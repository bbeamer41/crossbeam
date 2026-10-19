// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package bulletin exchanges articles as ASCII FBB bulletins on an
// AX.25 connection. The caller offers its articles, then the station
// that answered offers its own. Created time is not on the wire.
package bulletin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bbeamer41/crossbeam/internal/article"
	"github.com/bbeamer41/crossbeam/internal/ax25"
	"github.com/bbeamer41/crossbeam/internal/fbb"
	"github.com/bbeamer41/crossbeam/internal/settings"
	"github.com/bbeamer41/crossbeam/internal/store"
	"github.com/bbeamer41/crossbeam/internal/version"
)

// Result counts bulletins accepted by the peer and stored from the peer.
type Result struct {
	Sent     int
	Received int
}

type outgoing struct {
	prop fbb.Proposal
	wire []byte
}

// Answer handles one inbound AX.25 session. The banner carries the FBB
// SID, then the same prompt a terminal already uses. A human command
// stays on that prompt. An FBB proposal stores bulletins and offers ours.
func Answer(st *store.Store, local ax25.Address) func(ax25.Address, *ax25.Stream) {
	return func(_ ax25.Address, stream *ax25.Stream) {
		_ = Serve(context.Background(), st, local, stream)
	}
}

// Serve writes the banner and runs either the prompt or forwarding.
// It closes rw when it returns.
func Serve(ctx context.Context, st *store.Store, local ax25.Address, rw io.ReadWriteCloser) error {
	if rw != nil {
		defer rw.Close()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if st == nil {
		return errors.New("article store is missing")
	}
	name := local.String()
	sid, err := fbb.SID(version.String)
	if err != nil {
		return err
	}
	rd := newReader(rw)
	if err := writeAll(rw, sid+"\rCrossbeam "+name+"\r"+name+">\r"); err != nil {
		return peerGone(err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := rd.readLine(humanLine)
		if err != nil {
			if errors.Is(err, errLong) {
				if werr := writeAll(rw, "The line is too long.\r"+name+">\r"); werr != nil {
					return peerGone(werr)
				}
				continue
			}
			if errors.Is(err, io.EOF) || errors.Is(err, ax25.ErrClosed) {
				return nil
			}
			return err
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			if err := writeAll(rw, name+">\r"); err != nil {
				return peerGone(err)
			}
			continue
		}
		if strings.HasPrefix(trim, ";") {
			continue
		}
		if isForwardStart(trim) {
			return serveForward(ctx, st, local, rw, rd, line)
		}
		return human(rw, rd, name, line)
	}
}

// Forward dials as the master. It publishes every stored article and
// ingests the bulletins the peer offers in the same session.
func Forward(ctx context.Context, st *store.Store, local ax25.Address, rw io.ReadWriter) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if st == nil {
		return Result{}, errors.New("article store is missing")
	}
	queue, err := prepare(ctx, st, local)
	if err != nil {
		return Result{}, err
	}
	rd := newReader(rw)
	if err := readBanner(ctx, rd); err != nil {
		return Result{}, peerGone(err)
	}
	sid, err := fbb.SID(version.String)
	if err != nil {
		return Result{}, err
	}
	sess := newSession(ctx, st, rw, rd, queue)
	if err := sess.write(sid + "\r"); err != nil {
		return Result{}, peerGone(err)
	}
	err = sess.run(true)
	return sess.result(), peerGone(err)
}

func serveForward(ctx context.Context, st *store.Store, local ax25.Address, w io.Writer, rd *reader, first string) error {
	queue, err := prepare(ctx, st, local)
	if err != nil {
		_ = writeAll(w, "*** "+oneLine(err)+"\r")
		return err
	}
	sess := newSession(ctx, st, w, rd, queue)
	if !isSID(strings.TrimSpace(first)) {
		sess.held = true
		sess.hold = first
	}
	return peerGone(sess.run(false))
}

func human(w io.Writer, rd *reader, name, first string) error {
	line := first
	for {
		bye, err := humanCommand(w, name, line)
		if err != nil || bye {
			return peerGone(err)
		}
		next, err := rd.readLine(humanLine)
		if errors.Is(err, errLong) {
			if werr := writeAll(w, "The line is too long.\r"+name+">\r"); werr != nil {
				return peerGone(werr)
			}
			line = ""
			continue
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, ax25.ErrClosed) {
				return nil
			}
			return err
		}
		line = next
	}
}

func humanCommand(w io.Writer, name, line string) (bool, error) {
	switch strings.ToUpper(strings.TrimSpace(line)) {
	case "":
		return false, writeAll(w, name+">\r")
	case "?", "HELP":
		return false, writeAll(w, "Commands:\r?        This list\rBYE      Disconnect\r"+name+">\r")
	case "BYE", "QUIT", "EXIT":
		return true, writeAll(w, "Bye\r")
	default:
		return false, writeAll(w, "Unknown command. Send ? for help.\r"+name+">\r")
	}
}

func isForwardStart(trim string) bool {
	if isSID(trim) {
		return true
	}
	upper := strings.ToUpper(trim)
	if upper == "FF" || upper == "FQ" || strings.HasPrefix(upper, "F>") {
		return true
	}
	fields := strings.Fields(upper)
	return len(fields) > 0 && fields[0] == "FB"
}

func isSID(trim string) bool {
	return strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") && len(trim) >= 3
}

func prepare(ctx context.Context, st *store.Store, local ax25.Address) ([]outgoing, error) {
	current, err := st.Settings(ctx)
	if err != nil {
		return nil, err
	}
	to := current[settings.KeyBulletinTo]
	at := current[settings.KeyBulletinAt]
	if !fbb.ValidTo(to) || !fbb.ValidAt(at) {
		return nil, errors.New("bulletin settings are not valid")
	}
	rows, err := st.AllArticles(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]outgoing, 0, len(rows))
	for _, row := range rows {
		art, err := row.Article.Canonical()
		if err != nil {
			return nil, err
		}
		if n := utf8.RuneCountInString(art.Title); n > fbb.MaxSubjectRunes {
			return nil, errors.New(titleLimit(row.RowID, n))
		}
		from := fromCall(art.Author, local)
		bid, err := wireBID(art, from)
		if err != nil {
			return nil, err
		}
		if seen[bid] {
			return nil, errors.New("two articles use bulletin id " + bid)
		}
		seen[bid] = true
		wire, err := fbb.EncodeMessage(art.Title, art.Body)
		if err != nil {
			return nil, err
		}
		item := outgoing{prop: fbb.Proposal{
			Type: "B", From: from, At: at, To: to, BID: bid, Size: len(wire),
		}, wire: wire}
		out = append(out, item)
	}
	return out, nil
}

func titleLimit(rowID int64, n int) string {
	return fmt.Sprintf("article %d title is %d runes, above the bulletin limit of %d", rowID, n, fbb.MaxSubjectRunes)
}

func fromCall(author string, local ax25.Address) string {
	if addr, err := ax25.Parse(author); err == nil {
		return addr.String()
	}
	return local.String()
}

// wireBID keeps a short id that already fits the wire. Anything else
// becomes a stable id from the article text and the sending callsign.
func wireBID(art article.Article, from string) (string, error) {
	id := strings.ToUpper(strings.TrimSpace(art.ID))
	if fbb.ValidBID(id) {
		return id, nil
	}
	sum := sha256.Sum256([]byte(art.ID + "\n" + art.Title + "\n" + art.Created.UTC().Format(time.RFC3339) + "\n" + art.Body))
	all := strings.ToUpper(hex.EncodeToString(sum[:]))
	suffix := "_" + from
	room := fbb.MaxBID - len(suffix)
	var bid string
	if room < 2 {
		bid = all[:fbb.MaxBID]
	} else {
		bid = all[:room] + suffix
	}
	if !fbb.ValidBID(bid) {
		return "", errors.New("bulletin id cannot be formed")
	}
	return bid, nil
}

func readBanner(ctx context.Context, rd *reader) error {
	sawSID := false
	for i := 0; i < 20; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := rd.readLine(protoLine)
		if errors.Is(err, errLong) {
			return errors.New("the peer did not send a prompt")
		}
		if err != nil {
			return err
		}
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, ";") {
			continue
		}
		if isSID(trim) {
			sawSID = true
			continue
		}
		if strings.HasSuffix(trim, ">") {
			if !sawSID {
				return errors.New("the peer did not send a SID")
			}
			return nil
		}
	}
	return errors.New("the peer did not send a prompt")
}

func writeAll(w io.Writer, text string) error {
	_, err := io.WriteString(w, text)
	return err
}

func peerGone(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, ax25.ErrClosed) {
		return errors.New("the peer disconnected")
	}
	return err
}

func oneLine(err error) string {
	msg := err.Error()
	if msg == "" || len(msg) > 80 || strings.ContainsAny(msg, "\r\n") {
		return "bad proposal"
	}
	return msg
}
