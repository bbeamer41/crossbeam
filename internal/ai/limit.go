// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"sync"
	"time"
)

const limiterMax = 1024

// limiter counts questions per caller over the last hour.
type limiter struct {
	mu   sync.Mutex
	seen map[string][]time.Time
}

// take records one question when the caller is under max for the hour.
// When the caller is over, wait is how long until the oldest question
// leaves the window.
func (l *limiter) take(key string, max int, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen == nil {
		l.seen = map[string][]time.Time{}
	}
	cut := now.Add(-time.Hour)
	keep := l.seen[key][:0]
	for _, t := range l.seen[key] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	if len(keep) >= max {
		l.seen[key] = keep
		return false, keep[0].Add(time.Hour).Sub(now)
	}
	if len(l.seen) >= limiterMax {
		for k, times := range l.seen {
			if len(times) == 0 || !times[len(times)-1].After(cut) {
				delete(l.seen, k)
			}
		}
	}
	l.seen[key] = append(keep, now)
	return true, 0
}
