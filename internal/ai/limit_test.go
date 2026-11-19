// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"fmt"
	"testing"
	"time"
)

func TestLimiterHardCap(t *testing.T) {
	var l limiter
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	for i := 0; i < limiterMax; i++ {
		ok, _ := l.take(fmt.Sprintf("c%d", i), 20, now)
		if !ok {
			t.Fatalf("caller %d was refused under the cap", i)
		}
	}
	if len(l.seen) != limiterMax {
		t.Fatalf("map has %d keys", len(l.seen))
	}
	ok, wait := l.take("newcomer", 20, now)
	if ok || wait <= 0 {
		t.Fatalf("new caller at the cap: ok=%v wait=%s", ok, wait)
	}
	if _, exists := l.seen["newcomer"]; exists || len(l.seen) != limiterMax {
		t.Fatalf("map grew to %d", len(l.seen))
	}
	ok, _ = l.take("c0", 20, now)
	if !ok {
		t.Fatal("an existing caller was refused")
	}
	if len(l.seen) != limiterMax || len(l.seen["c0"]) != 2 {
		t.Fatalf("existing caller: len=%d times=%d", len(l.seen), len(l.seen["c0"]))
	}
	// Stale keys are dropped, so a later caller can be recorded.
	later := now.Add(time.Hour + time.Second)
	ok, _ = l.take("after", 20, later)
	if !ok {
		t.Fatal("a new caller was refused after the old keys went stale")
	}
	if _, exists := l.seen["after"]; !exists {
		t.Fatal("the later caller was not recorded")
	}
	if len(l.seen) > limiterMax {
		t.Fatalf("map grew past the cap to %d", len(l.seen))
	}
}
