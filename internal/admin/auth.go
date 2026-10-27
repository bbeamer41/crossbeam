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
			l.evictOne()
		}
		item = &attempt{}
		l.failures[ip] = item
	}
	item.fails++
	if item.fails >= maxFailures {
		item.until = time.Now().Add(lockout)
	}
}

// evictOne drops one client to stay under the cap. An entry that is not
// locked is dropped before a locked one, and the lock that ends soonest
// is dropped before a longer one.
func (l *limiter) evictOne() {
	var oldestIP string
	var oldest time.Time
	for ip, item := range l.failures {
		stamp := item.until
		if oldestIP == "" || stamp.Before(oldest) {
			oldestIP = ip
			oldest = stamp
		}
	}
	if oldestIP != "" {
		delete(l.failures, oldestIP)
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

// hostSet is the Host values this process accepts. Unspecified is set
// when the socket is 0.0.0.0 or ::; the request is then matched against
// the connection's own local address.
type hostSet struct {
	names       map[string]struct{}
	unspecified bool
}

func accessHosts(name string, bound []string) hostSet {
	set := hostSet{names: map[string]struct{}{}}
	var ports []string
	loopback := strings.EqualFold(name, "localhost")
	for _, addr := range bound {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			continue
		}
		ports = append(ports, port)
		if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
			set.unspecified = true
			continue
		}
		addHostPort(&set, host, port)
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			loopback = true
		}
	}
	if name != "" {
		for _, port := range ports {
			addHostPort(&set, name, port)
		}
	}
	if loopback {
		for _, port := range ports {
			addHostPort(&set, "localhost", port)
			addHostPort(&set, "127.0.0.1", port)
			addHostPort(&set, "::1", port)
		}
	}
	return set
}

func addHostPort(set *hostSet, host, port string) {
	set.names[strings.ToLower(net.JoinHostPort(host, port))] = struct{}{}
	if port == "443" {
		set.names[strings.ToLower(browserHost(host))] = struct{}{}
	}
}

func browserHost(host string) string {
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return "[" + ip.String() + "]"
	}
	return host
}

func hostAllowed(r *http.Request, hosts hostSet) bool {
	matched, ok := requestHostMatch(r, hosts)
	if !ok {
		return false
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	origin := r.Header.Get("Origin")
	// A browser that has not trusted the self-signed certificate sends
	// Origin "null" when the sign-in form is posted. The Host check above
	// still has to match. Other requests have to name this panel.
	// An empty Origin is a client that is not a browser.
	if origin == "null" {
		return r.URL.Path == "/login" || r.URL.Path == "/api/login"
	}
	if origin == "" {
		return true
	}
	return originMatches(origin, matched)
}

func requestHostMatch(r *http.Request, hosts hostSet) (string, bool) {
	key := strings.ToLower(r.Host)
	if _, ok := hosts.names[key]; ok {
		return key, true
	}
	if !hosts.unspecified {
		return "", false
	}
	addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || !endpointsEqual(r.Host, addr.String()) {
		return "", false
	}
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "", false
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = strings.Trim(r.Host, "[]")
	}
	return strings.ToLower(net.JoinHostPort(host, port)), true
}

func originMatches(origin, hostport string) bool {
	if strings.EqualFold(origin, "https://"+hostport) {
		return true
	}
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return strings.EqualFold(origin, "https://"+hostport+":443")
	}
	return port == "443" && strings.EqualFold(origin, "https://"+browserHost(host))
}

func endpointsEqual(header, local string) bool {
	localHost, localPort, err := net.SplitHostPort(local)
	if err != nil {
		return false
	}
	headerHost, headerPort, err := net.SplitHostPort(header)
	if err != nil {
		if localPort != "443" {
			return false
		}
		headerHost = strings.Trim(header, "[]")
		headerPort = "443"
	}
	if headerPort != localPort {
		return false
	}
	a := net.ParseIP(headerHost)
	b := net.ParseIP(localHost)
	if a == nil || b == nil {
		return strings.EqualFold(headerHost, localHost)
	}
	if v4 := a.To4(); v4 != nil {
		a = v4
	}
	if v4 := b.To4(); v4 != nil {
		b = v4
	}
	return a.Equal(b)
}
