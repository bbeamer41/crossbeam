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
	"log"
	"strings"
	"sync"
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
	row  int64
}

// Answer handles one inbound AX.25 session. The banner carries the FBB
// SID, then the same prompt a terminal already uses. A human command
// stays on that prompt. An FBB proposal stores bulletins and offers ours.
func Answer(st *store.Store, local ax25.Address) func(ax25.Address, io.ReadWriteCloser) {
	return func(_ ax25.Address, stream io.ReadWriteCloser) {
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
	label, allowed := sessionCaller(rw)
	noteSession("session", label)
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
			if !allowed {
				// FQ before any FS tells a forwarder to stop without sending bodies.
				// A real forwarder sends its station id first. The callsign is on
				// the proposal that follows, so wait briefly for that line.
				who := strings.TrimSpace(forwarderCall(rd, rw, trim) + " " + label)
				noteSession("forward refused", who)
				if err := writeAll(rw, "FQ\r"); err != nil {
					return peerGone(err)
				}
				return nil
			}
			noteSession("forward", label)
			return serveForward(ctx, st, local, rw, rd, line, label)
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
	label := streamLabel(rw)
	if err := readBanner(ctx, rd); err != nil {
		err = peerGone(err)
		noteExchange(label, nil, err)
		return Result{}, err
	}
	sid, err := fbb.SID(version.String)
	if err != nil {
		return Result{}, err
	}
	sess := newSession(ctx, st, rw, rd, queue)
	rememberIdentities(ctx, st, local, sess.known)
	if err := sess.write(sid + "\r"); err != nil {
		err = peerGone(err)
		noteExchange(label, sess, err)
		return Result{}, err
	}
	err = sess.run(true)
	noteExchange(label, sess, err)
	return sess.result(), peerGone(err)
}

func serveForward(ctx context.Context, st *store.Store, local ax25.Address, w io.Writer, rd *reader, first, label string) error {
	queue, err := prepare(ctx, st, local)
	if err != nil {
		// Read the caller's offer before deciding we have nothing to send.
		// FF on our turn lets the caller's forward finish. The ids already
		// stored are still remembered, including when this queue is empty.
		queue = nil
	}
	sess := newSession(ctx, st, w, rd, queue)
	rememberIdentities(ctx, st, local, sess.known)
	sess.localErr = err
	if !isSID(strings.TrimSpace(first)) {
		sess.held = true
		sess.hold = first
	}
	runErr := sess.run(false)
	noteExchange(label, sess, runErr)
	return peerGone(runErr)
}

// sessionCaller reads the optional mark the link attaches to a live session.
// A pipe that does not carry the mark is allowed, so existing tests keep
// exchanging bulletins. Production always sets the mark.
func sessionCaller(rw io.ReadWriteCloser) (string, bool) {
	type info interface {
		ForwardAllowed() bool
		PeerLabel() string
	}
	gate, ok := rw.(info)
	if !ok || gate == nil {
		return "", true
	}
	label := strings.TrimSpace(gate.PeerLabel())
	if label == "" {
		label = "unknown"
	}
	return label, gate.ForwardAllowed()
}

func noteSession(kind, label string) {
	if label == "" {
		return
	}
	log.Printf("crossbeam: %s %s", kind, label)
}

// noteExchange records each bulletin the peer accepted and each one this
// station stored, then the same summary line a refusal or a checksum uses.
func noteExchange(label string, sess *session, runErr error) {
	if label == "" {
		return
	}
	sent, received := 0, 0
	if sess != nil {
		for _, item := range sess.notes {
			way := "received"
			if item.sent {
				way = "sent"
			}
			noteSession("forward proposal "+way+" "+item.bid+" "+item.author, label)
		}
		sent = sess.sent
		received = sess.received
	}
	summary := fmt.Sprintf("forward done sent %d received %d", sent, received)
	if runErr != nil {
		summary += ": " + oneLine(runErr)
	}
	noteSession(summary, label)
}

func streamLabel(rw io.ReadWriter) string {
	type named interface{ PeerLabel() string }
	mark, ok := rw.(named)
	if !ok || mark == nil {
		return ""
	}
	return strings.TrimSpace(mark.PeerLabel())
}

// forwarderCall is the sender on an FB line. A forwarder sends the station
// id first, and the callsign is on the proposal after it. A callsign that
// does not fit AX.25 is still logged when it is a short token.
func forwarderCall(rd *reader, rw io.ReadWriteCloser, first string) string {
	if from := proposalCall(first); from != "" {
		return from
	}
	trim := strings.TrimSpace(first)
	if !isSID(trim) && !strings.HasPrefix(trim, ";") {
		return ""
	}
	return awaitProposalCall(rd, rw)
}

func awaitProposalCall(rd *reader, rw io.ReadWriteCloser) string {
	var mu sync.Mutex
	done := false
	timer := time.AfterFunc(2*time.Second, func() {
		mu.Lock()
		defer mu.Unlock()
		if done {
			return
		}
		done = true
		_ = rw.Close()
	})
	defer func() {
		timer.Stop()
		mu.Lock()
		done = true
		mu.Unlock()
	}()
	for i := 0; i < 32; i++ {
		line, err := rd.readLine(protoLine)
		if err != nil {
			return ""
		}
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, ";") || isSID(trim) {
			continue
		}
		return proposalCall(trim)
	}
	return ""
}

func proposalCall(trim string) string {
	fields := strings.Fields(trim)
	if len(fields) < 3 || !strings.EqualFold(fields[0], "FB") {
		return ""
	}
	return logCall(fields[2])
}

func logCall(raw string) string {
	if addr, err := ax25.Parse(raw); err == nil {
		return addr.String()
	}
	text := strings.ToUpper(strings.TrimSpace(raw))
	if len(text) < 1 || len(text) > 12 || strings.Trim(text, "-") == "" {
		return ""
	}
	for _, c := range text {
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
			return ""
		}
	}
	return text
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

// Check reports whether the stored articles can be forwarded. The
// terminal runs this before it dials, so a local problem does not open
// the link.
func Check(ctx context.Context, st *store.Store, local ax25.Address) error {
	_, err := prepare(ctx, st, local)
	return err
}

// rememberIdentities records every stored bulletin id and the wire id
// this station would offer for it. A failed offer queue must not forget
// those ids, or an echo of a generated id is stored again.
func rememberIdentities(ctx context.Context, st *store.Store, local ax25.Address, known map[string]bool) {
	if known == nil || st == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := st.BulletinSources(ctx)
	if err != nil {
		return
	}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return
		}
		for _, id := range identityOf(ctx, st, local, row) {
			known[id] = true
		}
	}
}

func identityOf(ctx context.Context, st *store.Store, local ax25.Address, row store.BulletinSource) []string {
	id := strings.ToUpper(strings.TrimSpace(row.ID))
	var ids []string
	if id != "" {
		ids = append(ids, id)
	}
	if fbb.ValidBID(id) {
		return ids
	}
	saved, err := st.Article(ctx, row.RowID)
	if err != nil {
		return ids
	}
	art, err := saved.Article.Canonical()
	if err != nil {
		return ids
	}
	bid, err := wireBID(art, fromCall(art.Author, local))
	if err != nil || bid == "" {
		return ids
	}
	ids = append(ids, bid)
	return ids
}

func prepare(ctx context.Context, st *store.Store, local ax25.Address) ([]outgoing, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	current, err := st.Settings(ctx)
	if err != nil {
		return nil, cleanStore(err)
	}
	to := current[settings.KeyBulletinTo]
	at := current[settings.KeyBulletinAt]
	if !fbb.ValidTo(to) || !fbb.ValidAt(at) {
		return nil, errors.New("bulletin settings are not valid")
	}
	rows, err := st.BulletinSources(ctx)
	if err != nil {
		return nil, cleanStore(err)
	}
	seen := map[string]bool{}
	out := make([]outgoing, 0, len(rows))
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item, err := oneOutgoing(ctx, st, local, row, to, at)
		if err != nil {
			return nil, err
		}
		if seen[item.prop.BID] {
			return nil, errors.New("two articles use bulletin id " + item.prop.BID)
		}
		seen[item.prop.BID] = true
		out = append(out, item)
	}
	return out, nil
}

func oneOutgoing(ctx context.Context, st *store.Store, local ax25.Address, row store.BulletinSource, to, at string) (outgoing, error) {
	title := row.Title
	author := row.Author
	framed := len(title) + 1 + row.BodyBytes + 1
	if n := utf8.RuneCountInString(title); n > fbb.MaxSubjectRunes {
		return outgoing{}, errors.New(titleLimit(row.RowID, n))
	}
	from := fromCall(author, local)
	id := strings.ToUpper(strings.TrimSpace(row.ID))
	var bid string
	if fbb.ValidBID(id) {
		bid = id
	} else {
		saved, err := st.Article(ctx, row.RowID)
		if err != nil {
			return outgoing{}, cleanStore(err)
		}
		art, err := saved.Article.Canonical()
		if err != nil {
			return outgoing{}, err
		}
		if n := utf8.RuneCountInString(art.Title); n > fbb.MaxSubjectRunes {
			return outgoing{}, errors.New(titleLimit(saved.RowID, n))
		}
		from = fromCall(art.Author, local)
		framed = len(art.Title) + 1 + len(art.Body) + 1
		var formErr error
		bid, formErr = wireBID(art, from)
		if formErr != nil {
			return outgoing{}, formErr
		}
	}
	if framed > fbb.MaxMessage {
		return outgoing{}, errors.New(messageLimit(row.RowID, framed))
	}
	if !fbb.ValidBID(bid) {
		return outgoing{}, errors.New("bulletin id cannot be formed")
	}
	return outgoing{prop: fbb.Proposal{
		Type: "B", From: from, At: at, To: to, BID: bid, Size: framed,
	}, row: row.RowID}, nil
}

func titleLimit(rowID int64, n int) string {
	return fmt.Sprintf("article %d title is %d runes, above the bulletin limit of %d", rowID, n, fbb.MaxSubjectRunes)
}

func messageLimit(rowID int64, n int) string {
	return fmt.Sprintf("article %d is %d bytes, above the bulletin limit of %d", rowID, n, fbb.MaxMessage)
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
	if err == nil || !safeWire(err.Error()) {
		return "bad proposal"
	}
	return err.Error()
}

// safeWire reports whether text can be shown on the AX.25 session.
// Paths, database details, and control bytes stay off the air.
func safeWire(msg string) bool {
	if msg == "" || len(msg) > 80 || strings.ContainsAny(msg, "\r\n\\/") {
		return false
	}
	for i := 0; i < len(msg); i++ {
		if msg[i] < 0x20 || msg[i] > 0x7e {
			return false
		}
	}
	lower := strings.ToLower(msg)
	for _, bad := range []string{"sqlite", ".db", "password", "token"} {
		if strings.Contains(lower, bad) {
			return false
		}
	}
	return true
}

func cleanStore(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if safeWire(err.Error()) {
		return err
	}
	return errors.New("the article store could not be read")
}
