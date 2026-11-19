// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

const (
	// SendersName is the approved, denied, and pending sender file in the
	// state directory. It is mode 0600.
	SendersName = "ai-senders.json"

	// NetMesh and NetRNS are the network names stored with a sender.
	NetMesh = "meshcore"
	NetRNS  = "reticulum"

	// WaitingApproval is the one reply an unknown MeshCore or Reticulum
	// sender gets while the sysop has not decided.
	WaitingApproval = "Your request is waiting for sysop approval."

	pendingMax  = 200
	pendingText = 160
	noticeEvery = 24 * time.Hour
)

var (
	errBadSenderID = errors.New("a sender id looks like meshcore:ADDRESS or reticulum:ADDRESS")
	errNotApproved = errors.New("that sender is not approved")
)

// Sender is one MeshCore contact or Reticulum address on a list.
type Sender struct {
	ID      string
	Network string
	Address string
	Name    string
	Seen    time.Time
	Text    string
}

// SenderLists is the pending, approved, and denied senders.
type SenderLists struct {
	Pending  []Sender
	Approved []Sender
	Denied   []Sender
}

type senderRecord struct {
	Network string    `json:"network"`
	Address string    `json:"address"`
	Name    string    `json:"name,omitempty"`
	Seen    time.Time `json:"seen,omitempty"`
	Text    string    `json:"text,omitempty"`
	Noticed time.Time `json:"noticed,omitempty"`
}

type book struct {
	Pending  []senderRecord `json:"pending,omitempty"`
	Approved []senderRecord `json:"approved,omitempty"`
	Denied   []senderRecord `json:"denied,omitempty"`
}

// ParseSenderID reads an id such as meshcore:010203040506 or a 32-digit
// Reticulum address with the reticulum: prefix.
func ParseSenderID(id string) (network, address string, err error) {
	id = strings.ToLower(strings.TrimSpace(id))
	network, address, ok := strings.Cut(id, ":")
	if !ok || strings.Contains(address, ":") {
		return "", "", errBadSenderID
	}
	switch network {
	case NetMesh:
		if !meshAddress(address) {
			return "", "", errBadSenderID
		}
	case NetRNS:
		if !lxmfAddress(address) {
			return "", "", errBadSenderID
		}
	default:
		return "", "", errBadSenderID
	}
	return network, address, nil
}

func meshAddress(s string) bool {
	if len(s) < 2 || len(s) > 64 || len(s)%2 != 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func senderOf(who Caller) (network, address string, ok bool) {
	if who.Mesh == who.Reticulum {
		return "", "", false
	}
	address = strings.ToLower(strings.TrimSpace(who.Call))
	if who.Mesh && meshAddress(address) {
		return NetMesh, address, true
	}
	if who.Reticulum && lxmfAddress(address) {
		return NetRNS, address, true
	}
	return "", "", false
}

// LoadSenders reads the sender file. A missing file is an empty list.
func LoadSenders(dir string) (SenderLists, error) {
	var out SenderLists
	err := withBookFile(dir, func(b *book) (bool, error) {
		out = b.lists()
		return false, nil
	})
	return out, err
}

// Senders returns the pending, approved, and denied lists.
func (s *Service) Senders() (SenderLists, error) {
	var out SenderLists
	err := s.withBook(func(b *book) (bool, error) {
		out = b.lists()
		return false, nil
	})
	return out, err
}

// Approve puts id on the approved list and takes them off pending and denied.
func (s *Service) Approve(id string) error {
	return s.decide(id, func(b *book, network, address string, now time.Time) (bool, string, string, error) {
		text := ""
		if i := indexOf(b.Pending, network, address); i >= 0 {
			text = b.Pending[i].Text
		}
		had := indexOf(b.Approved, network, address) >= 0
		removed := indexOf(b.Pending, network, address) >= 0 || indexOf(b.Denied, network, address) >= 0
		b.Pending = removeSender(b.Pending, network, address)
		b.Denied = removeSender(b.Denied, network, address)
		if !had {
			b.Approved = append(b.Approved, senderRecord{Network: network, Address: address, Seen: now})
		}
		if text == "" {
			text = network + ":" + address
		}
		return !had || removed, text, OutApproved, nil
	})
}

// Deny blocks id. A blocked sender gets no reply and is not queued again.
func (s *Service) Deny(id string) error {
	return s.decide(id, func(b *book, network, address string, now time.Time) (bool, string, string, error) {
		text := network + ":" + address
		if i := indexOf(b.Pending, network, address); i >= 0 && b.Pending[i].Text != "" {
			text = b.Pending[i].Text
		}
		had := indexOf(b.Denied, network, address) >= 0
		removed := indexOf(b.Pending, network, address) >= 0 || indexOf(b.Approved, network, address) >= 0
		b.Pending = removeSender(b.Pending, network, address)
		b.Approved = removeSender(b.Approved, network, address)
		if !had {
			b.Denied = append(b.Denied, senderRecord{Network: network, Address: address, Seen: now})
		}
		return !had || removed, text, OutDenied, nil
	})
}

// Revoke takes id off the approved list. The next message needs approval again.
func (s *Service) Revoke(id string) error {
	return s.decide(id, func(b *book, network, address string, now time.Time) (bool, string, string, error) {
		if indexOf(b.Approved, network, address) < 0 {
			return false, "", "", errNotApproved
		}
		b.Approved = removeSender(b.Approved, network, address)
		return true, network + ":" + address, OutRevoked, nil
	})
}

type decision func(b *book, network, address string, now time.Time) (changed bool, text, outcome string, err error)

func (s *Service) decide(id string, fn decision) error {
	network, address, err := ParseSenderID(id)
	if err != nil {
		return err
	}
	var changed bool
	var text, outcome string
	err = s.withBook(func(b *book) (bool, error) {
		var ferr error
		changed, text, outcome, ferr = fn(b, network, address, s.now())
		return changed, ferr
	})
	if err != nil {
		return err
	}
	if changed {
		s.logSysop(network+":"+address, text, outcome)
	}
	return nil
}

// screen decides whether a MeshCore or Reticulum sender may reach the model.
// allow is false when the caller should not be answered by the model. reply
// is the waiting sentence, or empty when the station stays silent.
func (s *Service) screen(who Caller, text string) (reply string, allow bool, err error) {
	network, address, ok := senderOf(who)
	if !ok {
		return "", false, nil
	}
	var entry *Entry
	err = s.withBook(func(b *book) (bool, error) {
		now := s.now()
		if indexOf(b.Denied, network, address) >= 0 {
			return false, nil
		}
		if indexOf(b.Approved, network, address) >= 0 {
			allow = true
			return false, nil
		}
		i := indexOf(b.Pending, network, address)
		if i < 0 {
			if len(b.Pending) >= pendingMax {
				return false, nil
			}
			rec := senderRecord{
				Network: network,
				Address: address,
				Seen:    now,
				Text:    clipText(text, pendingText),
				Noticed: now,
			}
			b.Pending = append(b.Pending, rec)
			reply = WaitingApproval
			entry = &Entry{
				Time:     now,
				Caller:   network + ":" + address,
				Via:      "sysop",
				Question: rec.Text,
				Outcome:  OutPending,
			}
			return true, nil
		}
		if b.Pending[i].Noticed.IsZero() || now.Sub(b.Pending[i].Noticed) >= noticeEvery {
			b.Pending[i].Noticed = now
			reply = WaitingApproval
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return "", false, err
	}
	if entry != nil {
		s.logSysop(entry.Caller, entry.Question, entry.Outcome)
	}
	return reply, allow, nil
}

func (s *Service) logSysop(caller, question, outcome string) {
	if s == nil || s.journal == nil {
		return
	}
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}
	if err := s.journal.Add(Entry{
		Time: now, Caller: caller, Via: "sysop", Question: question, Outcome: outcome,
	}); err != nil {
		log.Printf("crossbeam: ai log could not be written")
	}
}

func (s *Service) withBook(fn func(*book) (bool, error)) error {
	if s == nil || s.dir == "" {
		if s == nil {
			return errors.New("the AI assistant is not set up")
		}
		s.bookMu.Lock()
		defer s.bookMu.Unlock()
		if s.mem == nil {
			s.mem = &book{}
		}
		_, err := fn(s.mem)
		return err
	}
	return withBookFile(s.dir, fn)
}

func withBookFile(dir string, fn func(*book) (bool, error)) error {
	lockPath := filepath.Join(dir, ".ai-senders.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if err := flock(f); err != nil {
		return err
	}
	defer funlock(f)
	b, err := loadBook(filepath.Join(dir, SendersName))
	if err != nil {
		return err
	}
	dirty, err := fn(b)
	if err != nil || !dirty {
		return err
	}
	return saveBook(dir, b)
}

func loadBook(path string) (*book, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &book{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return &book{}, nil
	}
	var b book
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	b.Pending = cleanRecords(b.Pending, true)
	b.Approved = cleanRecords(b.Approved, false)
	b.Denied = cleanRecords(b.Denied, false)
	if len(b.Pending) > pendingMax {
		b.Pending = b.Pending[:pendingMax]
	}
	return &b, nil
}

func cleanRecords(in []senderRecord, pending bool) []senderRecord {
	var out []senderRecord
	seen := map[string]bool{}
	for _, rec := range in {
		rec.Network = strings.ToLower(strings.TrimSpace(rec.Network))
		rec.Address = strings.ToLower(strings.TrimSpace(rec.Address))
		if _, _, err := ParseSenderID(rec.Network + ":" + rec.Address); err != nil {
			continue
		}
		key := rec.Network + ":" + rec.Address
		if seen[key] {
			continue
		}
		seen[key] = true
		if !pending {
			rec.Text = ""
			rec.Noticed = time.Time{}
		} else {
			rec.Text = clipText(rec.Text, pendingText)
		}
		out = append(out, rec)
	}
	return out
}

func saveBook(dir string, b *book) error {
	if len(b.Pending) > pendingMax {
		b.Pending = b.Pending[:pendingMax]
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(dir, ".ai-senders-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if info, err := os.Stat(dir); err == nil {
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				_ = os.Chown(name, int(st.Uid), int(st.Gid))
			}
		}
	}
	return os.Rename(name, filepath.Join(dir, SendersName))
}

func (b *book) lists() SenderLists {
	return SenderLists{
		Pending:  senderViews(b.Pending),
		Approved: senderViews(b.Approved),
		Denied:   senderViews(b.Denied),
	}
}

func senderViews(in []senderRecord) []Sender {
	out := make([]Sender, 0, len(in))
	for _, rec := range in {
		out = append(out, Sender{
			ID:      rec.Network + ":" + rec.Address,
			Network: rec.Network,
			Address: rec.Address,
			Name:    rec.Name,
			Seen:    rec.Seen,
			Text:    rec.Text,
		})
	}
	return out
}

func indexOf(list []senderRecord, network, address string) int {
	for i, rec := range list {
		if rec.Network == network && rec.Address == address {
			return i
		}
	}
	return -1
}

func removeSender(list []senderRecord, network, address string) []senderRecord {
	out := list[:0]
	for _, rec := range list {
		if rec.Network == network && rec.Address == address {
			continue
		}
		out = append(out, rec)
	}
	return out
}

func clipText(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for c := 0; c < n && i < len(s); c++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}
