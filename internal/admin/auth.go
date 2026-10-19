// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "crossbeam_session"
	badPassword   = "The password is not right."
	requestHeader = "X-Crossbeam-Request"
	maxFailures   = 8
	lockout       = time.Minute
	maxClients    = 1024
)

type limiter struct {
	mu       sync.Mutex
	failures map[string]*attempt
}

type attempt struct {
	fails int
	until time.Time
}

func newLimiter() *limiter {
	return &limiter{failures: map[string]*attempt{}}
}

func (l *limiter) locked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	item := l.failures[ip]
	if item == nil || item.until.IsZero() {
		return false
	}
	if time.Now().Before(item.until) {
		return true
	}
	item.fails = 0
	item.until = time.Time{}
	return false
}

func (l *limiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	item := l.failures[ip]
	if item == nil {
		if len(l.failures) >= maxClients {
			l.failures = map[string]*attempt{}
		}
		item = &attempt{}
		l.failures[ip] = item
	}
	item.fails++
	if item.fails >= maxFailures {
		item.until = time.Now().Add(lockout)
	}
}

func (l *limiter) success(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func csrfOK(got, want string) bool {
	if len(got) != 64 || len(want) != 64 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func allowedHosts(listen string) map[string]struct{} {
	set := map[string]struct{}{strings.ToLower(listen): {}}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return set
	}
	add := func(name string) {
		set[strings.ToLower(net.JoinHostPort(name, port))] = struct{}{}
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		add("localhost")
		add("127.0.0.1")
		add("::1")
	}
	if strings.EqualFold(host, "localhost") {
		add("localhost")
		add("127.0.0.1")
		add("::1")
	}
	return set
}

func hostAllowed(r *http.Request, hosts map[string]struct{}) bool {
	if _, ok := hosts[strings.ToLower(r.Host)]; !ok {
		return false
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	for host := range hosts {
		if strings.EqualFold(origin, "https://"+host) {
			return true
		}
	}
	return false
}
