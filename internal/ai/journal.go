// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogName is the AI log in the state directory. Each line is one JSON
// entry. The previous log is kept once as LogName + ".1".
const LogName = "ai-log.jsonl"

const journalMax = 4 << 20

// Entry is one question and what the station did with it. The API key
// is never part of an entry.
type Entry struct {
	Time     time.Time `json:"time"`
	Caller   string    `json:"caller"`
	Via      string    `json:"via"`
	Model    string    `json:"model,omitempty"`
	Question string    `json:"question"`
	Reply    string    `json:"reply,omitempty"`
	Outcome  string    `json:"outcome"`
	Millis   int64     `json:"ms,omitempty"`
}

// Outcomes written to the log.
const (
	OutAnswered  = "answered"
	OutWithheld  = "withheld"
	OutRefused   = "refused"
	OutLimited   = "limited"
	OutBusy      = "busy"
	OutError     = "error"
	OutCancelled = "cancelled"
	OutPending   = "pending"
	OutApproved  = "approved"
	OutDenied    = "denied"
	OutRevoked   = "revoked"
)

// Journal appends entries to the AI log, mode 0600.
type Journal struct {
	mu   sync.Mutex
	path string
}

// NewJournal writes to dir/LogName. An empty dir keeps no log.
func NewJournal(dir string) *Journal {
	if dir == "" {
		return &Journal{}
	}
	return &Journal{path: filepath.Join(dir, LogName)}
}

// Path is the log file, or empty.
func (j *Journal) Path() string {
	if j == nil {
		return ""
	}
	return j.path
}

// Add appends e. A write failure is returned for the caller to note.
func (j *Journal) Add(e Entry) error {
	if j == nil || j.path == "" {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if info, err := os.Stat(j.path); err == nil && info.Size() > journalMax {
		_ = os.Rename(j.path, j.path+".1")
	}
	f, err := os.OpenFile(j.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_ = f.Chmod(0o600)
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(raw, '\n'))
	return err
}

// ReadLog returns the last n entries of the log in dir, oldest first.
func ReadLog(dir string, n int) ([]Entry, error) {
	f, err := os.Open(filepath.Join(dir, LogName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64<<10), 1<<20)
	for scan.Scan() {
		var e Entry
		if json.Unmarshal(scan.Bytes(), &e) != nil {
			continue
		}
		out = append(out, e)
		if n > 0 && len(out) > n {
			out = out[1:]
		}
	}
	return out, scan.Err()
}
