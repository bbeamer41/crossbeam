// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package bulletin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bbeamer41/crossbeam/internal/article"
	"github.com/bbeamer41/crossbeam/internal/ax25"
	"github.com/bbeamer41/crossbeam/internal/fbb"
	"github.com/bbeamer41/crossbeam/internal/store"
)

const (
	humanLine        = 236
	protoLine        = 512
	maxAccepted      = 100
	maxAcceptedBytes = 2 * 1024 * 1024
	maxRounds        = 200
)

// A peer can reconnect, so the per-session caps are not the disk cap.
// These bounds are what one station will accept into the article store.
// Editing an article down, or deleting it, makes room for a later forward.
var (
	maxStoreArticles = 2000
	maxStoreBytes    = 32 << 20
)

var errLong = errors.New("line is too long")

// reader splits carriage-return lines and exact message bodies.
// A line feed after a carriage return is skipped and is not part of the checksum.
type reader struct {
	r      *bufio.Reader
	skipLF bool
}

func newReader(r io.Reader) *reader {
	return &reader{r: bufio.NewReader(r)}
}

func (r *reader) discardLF() error {
	if !r.skipLF {
		return nil
	}
	r.skipLF = false
	b, err := r.r.ReadByte()
	if err != nil {
		return err
	}
	if b != '\n' {
		return r.r.UnreadByte()
	}
	return nil
}

func (r *reader) readLine(max int) (string, error) {
	if err := r.discardLF(); err != nil {
		return "", err
	}
	var b []byte
	for {
		ch, err := r.r.ReadByte()
		if err != nil {
			return "", err
		}
		if ch == '\r' || ch == '\n' {
			if ch == '\r' {
				r.skipLF = true
			}
			return string(b), nil
		}
		if len(b) == max {
			if err := r.drain(); err != nil {
				return "", err
			}
			return "", errLong
		}
		b = append(b, ch)
	}
}

func (r *reader) drain() error {
	for n := 0; n < 4096; n++ {
		ch, err := r.r.ReadByte()
		if err != nil {
			return err
		}
		if ch == '\r' || ch == '\n' {
			if ch == '\r' {
				r.skipLF = true
			}
			return nil
		}
	}
	return errLong
}

func (r *reader) readExact(n int) ([]byte, error) {
	if err := r.discardLF(); err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r.r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

type session struct {
	ctx           context.Context
	st            *store.Store
	w             io.Writer
	rd            *reader
	queue         []outgoing
	known         map[string]bool
	hold          string
	held          bool
	sent          int
	received      int
	receivedBytes int
	localErr      error
}

func newSession(ctx context.Context, st *store.Store, w io.Writer, rd *reader, queue []outgoing) *session {
	// known is every id this station would send. An echo of one of those
	// is a duplicate, including when the stored id is longer than the wire id.
	known := make(map[string]bool, len(queue))
	for _, item := range queue {
		known[item.prop.BID] = true
	}
	return &session{ctx: ctx, st: st, w: w, rd: rd, queue: queue, known: known}
}

func (s *session) result() Result {
	return Result{Sent: s.sent, Received: s.received}
}

func (s *session) write(text string) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	_, err := io.WriteString(s.w, text)
	return err
}

func (s *session) writeBytes(raw []byte) error {
	if err := s.ctx.Err(); err != nil {
		return err
	}
	_, err := s.w.Write(raw)
	return err
}

// run offers when offerFirst is set. The station that was called offers
// after the caller. FF means the sender has nothing more. FQ ends it.
// A deferred bulletin stays in the store and is offered on a later forward.
func (s *session) run(offerFirst bool) error {
	expectOffer := offerFirst
	peerSaidFF := false
	for round := 0; round < maxRounds; round++ {
		if err := s.ctx.Err(); err != nil {
			return err
		}
		if expectOffer {
			// A local problem is reported on our turn. The caller has
			// finished writing its offer and is waiting to read.
			if s.localErr != nil {
				return s.fail(s.localErr)
			}
			if len(s.queue) == 0 {
				if peerSaidFF {
					return s.write("FQ\r")
				}
				if err := s.write("FF\r"); err != nil {
					return err
				}
				expectOffer = false
				continue
			}
			if err := s.sendBatch(); err != nil {
				return s.fail(err)
			}
			peerSaidFF = false
			expectOffer = false
			continue
		}
		kind, err := s.receive()
		if err != nil {
			return s.fail(err)
		}
		switch kind {
		case "fq":
			if s.localErr != nil {
				return s.localErr
			}
			return nil
		case "ff":
			peerSaidFF = true
			expectOffer = true
		default:
			peerSaidFF = false
			expectOffer = true
		}
	}
	return s.fail(errors.New("too many proposal blocks"))
}

type peerErr struct {
	text string
}

func (e peerErr) Error() string { return e.text }

func (s *session) fail(err error) error {
	var peer peerErr
	if err == nil || errors.As(err, &peer) || quietErr(err) {
		return err
	}
	_ = s.write("*** " + oneLine(err) + "\r")
	return err
}

func quietErr(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, ax25.ErrClosed) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (s *session) nextLine() (string, error) {
	if s.held {
		s.held = false
		return s.hold, nil
	}
	for {
		if err := s.ctx.Err(); err != nil {
			return "", err
		}
		line, err := s.rd.readLine(protoLine)
		if err != nil {
			if errors.Is(err, errLong) {
				return "", errors.New("bad proposal")
			}
			return "", err
		}
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, ";") {
			continue
		}
		if text, ok := peerError(trim); ok {
			return "", peerErr{text: text}
		}
		return line, nil
	}
}

func peerError(trim string) (string, bool) {
	if !strings.HasPrefix(trim, "***") {
		return "", false
	}
	msg := strings.TrimSpace(strings.TrimPrefix(trim, "***"))
	if !safeWire(msg) {
		return "bad proposal", true
	}
	return msg, true
}

func (s *session) sendBatch() error {
	batch, rest := takeBatch(s.queue)
	s.queue = rest
	props := make([]fbb.Proposal, len(batch))
	wires := make([][]byte, len(batch))
	for i := range batch {
		wire, err := s.encodeRow(batch[i])
		if err != nil {
			return err
		}
		wires[i] = wire
		props[i] = batch[i].prop
	}
	raw, err := fbb.EncodeProposals(props)
	if err != nil {
		return err
	}
	if err := s.writeBytes(raw); err != nil {
		return err
	}
	answers, err := s.readFS(len(batch))
	if err != nil {
		return err
	}
	for i, mark := range answers {
		if mark != '+' {
			continue
		}
		if err := s.writeBytes(wires[i]); err != nil {
			return err
		}
		s.sent++
	}
	return nil
}

func (s *session) encodeRow(item outgoing) ([]byte, error) {
	saved, err := s.st.Article(s.ctx, item.row)
	if err != nil {
		return nil, cleanStore(err)
	}
	art, err := saved.Article.Canonical()
	if err != nil {
		return nil, err
	}
	wire, err := fbb.EncodeMessage(art.Title, art.Body)
	if err != nil {
		return nil, err
	}
	if len(wire) != item.prop.Size {
		return nil, fmt.Errorf("article %d message size changed", item.row)
	}
	return wire, nil
}

func (s *session) readFS(n int) ([]byte, error) {
	for {
		line, err := s.nextLine()
		if err != nil {
			return nil, err
		}
		trim := strings.TrimSpace(line)
		if trim == "" || isSID(trim) {
			continue
		}
		return fbb.ParseFS(trim, n)
	}
}

func (s *session) receive() (string, error) {
	var lines []string
	for {
		line, err := s.nextLine()
		if err != nil {
			return "", err
		}
		trim := strings.TrimSpace(line)
		if trim == "" || isSID(trim) {
			continue
		}
		upper := strings.ToUpper(trim)
		switch {
		case upper == "FF":
			if len(lines) > 0 {
				return "", errors.New("bad proposal")
			}
			return "ff", nil
		case upper == "FQ":
			if len(lines) > 0 {
				return "", errors.New("bad proposal")
			}
			return "fq", nil
		case strings.HasPrefix(upper, "F>"):
			if err := s.takeProposal(lines, line); err != nil {
				return "", err
			}
			return "batch", nil
		default:
			fields := strings.Fields(upper)
			if len(fields) == 0 || fields[0] != "FB" {
				return "", errors.New("bad proposal")
			}
			lines = append(lines, line)
			if len(lines) > fbb.MaxProposals {
				return "", errors.New("bad proposal")
			}
		}
	}
}

func (s *session) takeProposal(lines []string, end string) error {
	if len(lines) == 0 {
		return errors.New("bad proposal")
	}
	has, sum, err := fbb.ParseForwardEnd(end)
	if err != nil {
		return err
	}
	if has && sum != fbb.Checksum(lines) {
		return errors.New("bad checksum")
	}
	props := make([]fbb.Proposal, 0, len(lines))
	for _, line := range lines {
		p, err := fbb.ParseProposal(line)
		if err != nil {
			return err
		}
		if p.Size > article.MaxBodyBytes {
			return errors.New("message is too large")
		}
		props = append(props, p)
	}
	answers := make([]byte, len(props))
	yes := make([]fbb.Proposal, 0, len(props))
	seen := map[string]bool{}
	count := s.received
	total := s.receivedBytes
	usedN, usedB, err := s.st.BulletinUsage(s.ctx)
	if err != nil {
		return cleanStore(err)
	}
	pendN := 0
	pendB := 0
	for i, p := range props {
		if p.Type != "B" {
			answers[i] = '-'
			continue
		}
		have, err := s.st.HasBulletinID(s.ctx, p.BID)
		if err != nil {
			return cleanStore(err)
		}
		if have || seen[p.BID] || s.known[p.BID] {
			answers[i] = '-'
			continue
		}
		if usedN+pendN >= maxStoreArticles || usedB+int64(pendB)+int64(p.Size) > int64(maxStoreBytes) {
			answers[i] = '='
			continue
		}
		if count >= maxAccepted || total+p.Size > maxAcceptedBytes {
			return errors.New("too many bulletins in one session")
		}
		seen[p.BID] = true
		count++
		total += p.Size
		pendN++
		pendB += p.Size
		answers[i] = '+'
		yes = append(yes, p)
	}
	if err := s.write(fmt.Sprintf("FS %s\r", string(answers))); err != nil {
		return err
	}
	for _, p := range yes {
		raw, err := s.rd.readExact(p.Size)
		if err != nil {
			return err
		}
		title, body, err := fbb.DecodeMessage(raw)
		if err != nil {
			return err
		}
		if err := s.save(p, title, body); err != nil {
			if errors.Is(err, store.ErrDuplicateID{}) {
				continue
			}
			return cleanStore(err)
		}
		s.received++
		s.receivedBytes += p.Size
		s.known[p.BID] = true
	}
	return nil
}

func (s *session) save(p fbb.Proposal, title, body string) error {
	_, err := s.st.SaveArticle(s.ctx, store.ArticleIn{
		ID:     p.BID,
		Title:  title,
		Author: p.From,
		Body:   body,
		Now:    time.Now().UTC(),
	})
	return err
}

func takeBatch(queue []outgoing) (batch, rest []outgoing) {
	total := 0
	for len(queue) > 0 && len(batch) < fbb.MaxProposals {
		if len(batch) > 0 && total+queue[0].prop.Size > fbb.MaxBlock {
			break
		}
		total += queue[0].prop.Size
		batch = append(batch, queue[0])
		queue = queue[1:]
	}
	return batch, queue
}
