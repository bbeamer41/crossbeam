// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/article"
	"github.com/bbeamer41/crossbeam/internal/settings"
	"github.com/bbeamer41/crossbeam/internal/store"
)

func TestPanelFlow(t *testing.T) {
	running, st, client := startPanel(t)
	base := running.URL

	anon := trustClient(t, filepath.Join(st.Dir(), "tls", "cert.pem"))
	anon.Jar = nil
	res, err := anon.Get(base + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, res)
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "Sign in is required.") {
		t.Fatalf("unauth %d %s", res.StatusCode, body)
	}
	res, err = anon.Get(base + "/app.css")
	if err != nil {
		t.Fatal(err)
	}
	body = readAll(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, "#f3efe6") || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("css %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	res, err = anon.Get(base + "/app.js")
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("script %d", res.StatusCode)
	}

	res, err = client.PostForm(base+"/login", url.Values{"password": {"not-the-password"}})
	if err != nil {
		t.Fatal(err)
	}
	body = readAll(t, res)
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "The password is not right.") {
		t.Fatalf("bad login %d %s", res.StatusCode, body)
	}

	res, err = client.PostForm(base+"/login", url.Values{"password": {"test-password-1"}})
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("login %d", res.StatusCode)
	}
	raw := res.Header.Get("Set-Cookie")
	for _, part := range []string{"Secure", "HttpOnly", "SameSite=Strict", "Path=/"} {
		if !strings.Contains(raw, part) {
			t.Fatalf("cookie %q missing %s", raw, part)
		}
	}

	res, err = client.Get(base + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	page := readAll(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("settings %d %s", res.StatusCode, page)
	}
	for _, field := range settings.Schema {
		if !strings.Contains(page, field.Key) || !strings.Contains(page, field.Help) {
			t.Fatalf("settings page missing %s", field.Key)
		}
	}
	token := csrfFrom(t, page)

	res, err = client.PostForm(base+"/settings", url.Values{"station.name": {"ignored"}})
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("csrf %d", res.StatusCode)
	}
	res, err = client.PostForm(base+"/settings", url.Values{
		"csrf":         {token},
		"admin.public": {"false", "true"},
	})
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("checkbox %d", res.StatusCode)
	}
	got, err := st.Settings(context.Background())
	if err != nil || got[settings.KeyPublic] != "true" {
		t.Fatalf("checkbox value %v %v", got, err)
	}
	if _, err := st.Apply(context.Background(), map[string]string{settings.KeyPublic: "false"}); err != nil {
		t.Fatal(err)
	}

	_, storeErr := st.Apply(context.Background(), map[string]string{settings.KeyPublic: "yes"})
	if storeErr == nil {
		t.Fatal("store accepted yes")
	}
	req, err := http.NewRequest(http.MethodPut, base+"/api/settings", strings.NewReader(`{"admin.public":"yes"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Crossbeam-Request", "1")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rejected := readAll(t, res)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(rejected, storeErr.Error()) {
		t.Fatalf("put reject %d %s, store %v", res.StatusCode, rejected, storeErr)
	}
	req, err = http.NewRequest(http.MethodPut, base+"/api/settings", strings.NewReader(`{"admin.public":"yes"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("missing header %d", res.StatusCode)
	}

	req, err = http.NewRequest(http.MethodPut, base+"/api/settings", strings.NewReader(`{"station.name":"Home"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Crossbeam-Request", "1")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("put %d", res.StatusCode)
	}
	got, err = st.Settings(context.Background())
	if err != nil || got[settings.KeyName] != "Home" {
		t.Fatalf("store %v %v", got, err)
	}

	created := time.Date(2026, 10, 18, 15, 4, 5, 0, time.UTC)
	payload := `{"title":"Hello","author":"K5LEB","created":"2026-10-18T15:04:05Z","body":"The net meets."}`
	want, err := (article.Article{Title: "Hello", Author: "K5LEB", Created: created, Body: "The net meets."}).RenderPacket()
	if err != nil {
		t.Fatal(err)
	}
	wantMicron, err := (article.Article{Title: "Hello", Author: "K5LEB", Created: created, Body: "The net meets."}).RenderMicron()
	if err != nil {
		t.Fatal(err)
	}
	req, err = http.NewRequest(http.MethodPost, base+"/api/preview", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Crossbeam-Request", "1")
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var preview struct {
		Packet string `json:"packet"`
		Micron string `json:"micron"`
	}
	if err := json.NewDecoder(res.Body).Decode(&preview); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if preview.Packet != want || preview.Micron != wantMicron {
		t.Fatalf("preview\n%q\n%q", preview.Packet, preview.Micron)
	}

	res, err = client.PostForm(base+"/compose", url.Values{
		"csrf":    {token},
		"title":   {"Hello"},
		"author":  {"K5LEB"},
		"created": {"2026-10-18T15:04:05Z"},
		"body":    {"The net meets."},
		"action":  {"preview"},
	})
	if err != nil {
		t.Fatal(err)
	}
	page = readAll(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(page, `id="packet"`) || !strings.Contains(page, `id="micron"`) || !strings.Contains(page, "Title: Hello") || !strings.Contains(page, "----") {
		t.Fatalf("compose %d %s", res.StatusCode, page)
	}

	res, err = client.PostForm(base+"/compose", url.Values{
		"csrf":    {csrfFrom(t, page)},
		"title":   {"Hello"},
		"author":  {"K5LEB"},
		"created": {"2026-10-18T15:04:05Z"},
		"body":    {"The net meets."},
		"action":  {"save"},
	})
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("save %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	res, err = client.Get(base + loc)
	if err != nil {
		t.Fatal(err)
	}
	page = readAll(t, res)
	if res.StatusCode != http.StatusOK || !strings.Contains(page, "Title: Hello") || !strings.Contains(page, "----") {
		t.Fatalf("reopen %d %s", res.StatusCode, page)
	}

	info, err := os.Stat(running.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %o", info.Mode().Perm())
	}
	unixClient := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", running.SocketPath)
		},
	}}
	unixPut := func(header bool) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, "http://crossbeam/api/settings", strings.NewReader(`{"station.callsign":"K5LEB"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Host = "evil.example"
		if header {
			req.Header.Set("X-Crossbeam-Request", "1")
		}
		res, err := unixClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res = unixPut(false)
	readAll(t, res)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("unix without header %d", res.StatusCode)
	}
	res = unixPut(true)
	readAll(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("unix put %d", res.StatusCode)
	}
	got, err = st.Settings(context.Background())
	if err != nil || got[settings.KeyCallsign] != "K5LEB" {
		t.Fatalf("callsign %v %v", got, err)
	}

	req, err = http.NewRequest(http.MethodGet, base+"/login", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example"
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("evil host %d", res.StatusCode)
	}
}

func TestPublicBindRejected(t *testing.T) {
	for _, listen := range []string{"0.0.0.0:8443", "[::]:8443", "1.2.3.4:8443", "8.8.8.8:53"} {
		st := openState(t)
		_, err := Start(context.Background(), st, Config{Listen: listen})
		if err == nil {
			t.Fatalf("%s was bound", listen)
		}
	}
	st := openState(t)
	_, err := Start(context.Background(), st, Config{
		Listen: "panel.test:9",
		Lookup: func(context.Context, string) ([]net.IP, error) {
			return []net.IP{net.ParseIP("8.8.8.8")}, nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "public address") {
		t.Fatalf("public name: %v", err)
	}
}

func TestHostnameBind(t *testing.T) {
	st := openState(t)
	if err := st.SetPassword(context.Background(), "test-password-1"); err != nil {
		t.Fatal(err)
	}
	var calls int
	running, err := Start(context.Background(), st, Config{
		Listen: "panel.test:0",
		Lookup: func(context.Context, string) ([]net.IP, error) {
			calls++
			return []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.2")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		running.Close()
		running.Wait()
	})
	if calls != 1 {
		t.Fatalf("lookups %d", calls)
	}
	if !strings.HasPrefix(running.URL, "https://panel.test:") {
		t.Fatalf("url %s", running.URL)
	}
	bounds := strings.Split(running.Listen, ", ")
	if len(bounds) != 2 {
		t.Fatalf("listen %s", running.Listen)
	}
	pemBytes, err := os.ReadFile(filepath.Join(st.Dir(), "tls", "cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatal("certificate pem")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"panel.test", "127.0.0.1", "127.0.0.2"} {
		if !sanMatches(leaf, name) {
			t.Fatalf("certificate missing %s", name)
		}
	}
	client := trustClient(t, filepath.Join(st.Dir(), "tls", "cert.pem"))
	baseTransport := client.Transport.(*http.Transport)
	baseTransport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		var d net.Dialer
		return d.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", port))
	}
	res, err := client.Get(running.URL + "/login")
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("hostname %d", res.StatusCode)
	}
	req, err := http.NewRequest(http.MethodGet, running.URL+"/login", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example"
	res, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("evil host %d", res.StatusCode)
	}
	_, port, err := net.SplitHostPort(bounds[1])
	if err != nil {
		t.Fatal(err)
	}
	second := trustClient(t, filepath.Join(st.Dir(), "tls", "cert.pem"))
	second.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, bounds[1])
	}
	res, err = second.Get("https://panel.test:" + port + "/login")
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("second address %d", res.StatusCode)
	}
}

func TestHostRules(t *testing.T) {
	hosts := accessHosts("", []string{"192.0.2.10:443"})
	req := httptest.NewRequest(http.MethodPost, "https://192.0.2.10/login", strings.NewReader("password=x"))
	req.Host = "192.0.2.10"
	req.Header.Set("Origin", "https://192.0.2.10")
	if !hostAllowed(req, hosts) {
		t.Fatal("bare 443 host was rejected")
	}
	req.Host = "192.0.2.10:443"
	req.Header.Set("Origin", "https://192.0.2.10:443")
	if !hostAllowed(req, hosts) {
		t.Fatal("explicit 443 host was rejected")
	}
	req.Header.Set("Origin", "https://evil.example")
	if hostAllowed(req, hosts) {
		t.Fatal("evil origin was accepted")
	}

	v6 := accessHosts("", []string{"[::1]:443"})
	req.Host = "[::1]"
	req.Header.Set("Origin", "https://[::1]")
	if !hostAllowed(req, v6) {
		t.Fatal("ipv6 port 443 host was rejected")
	}

	wild := accessHosts("", []string{"0.0.0.0:8443"})
	req = httptest.NewRequest(http.MethodGet, "https://192.0.2.10:8443/login", nil)
	req.Host = "192.0.2.10:8443"
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, fakeAddr("192.0.2.10:8443")))
	if !hostAllowed(req, wild) {
		t.Fatal("unspecified bind rejected its own address")
	}
	mapped := req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, fakeAddr("[::ffff:192.0.2.10]:8443")))
	if !hostAllowed(mapped, wild) {
		t.Fatal("mapped local address was rejected")
	}
	other := req.Clone(req.Context())
	other.Host = "198.51.100.2:8443"
	if hostAllowed(other, wild) {
		t.Fatal("unspecified bind accepted a different host")
	}

	named := accessHosts("panel.test", []string{"192.0.2.10:8443"})
	req = httptest.NewRequest(http.MethodPost, "https://panel.test:8443/login", nil)
	req.Host = "panel.test:8443"
	req.Header.Set("Origin", "https://panel.test:8443")
	if !hostAllowed(req, named) {
		t.Fatal("hostname was rejected")
	}
	req.Host = "192.0.2.10:8443"
	req.Header.Set("Origin", "https://192.0.2.10:8443")
	if !hostAllowed(req, named) {
		t.Fatal("bound address was rejected")
	}
	req.Host = "panel.test:8443"
	req.Header.Set("Origin", "null")
	if !hostAllowed(req, named) {
		t.Fatal("null origin was rejected")
	}
	api := httptest.NewRequest(http.MethodPost, "https://panel.test:8443/api/login", nil)
	api.Host = "panel.test:8443"
	api.Header.Set("Origin", "null")
	if !hostAllowed(api, named) {
		t.Fatal("null origin was rejected on api login")
	}
	off := httptest.NewRequest(http.MethodPost, "https://panel.test:8443/settings", nil)
	off.Host = "panel.test:8443"
	off.Header.Set("Origin", "null")
	if hostAllowed(off, named) {
		t.Fatal("null origin was accepted off the sign-in form")
	}
	off.Header.Del("Origin")
	if !hostAllowed(off, named) {
		t.Fatal("empty origin was rejected")
	}
	req.Host = "panel.test"
	req.Header.Del("Origin")
	if hostAllowed(req, named) {
		t.Fatal("hostname without its port was accepted")
	}
}

type fakeAddr string

func (fakeAddr) Network() string  { return "tcp" }
func (a fakeAddr) String() string { return string(a) }

func TestRateLimit(t *testing.T) {
	running, _, client := startPanel(t)
	for i := 0; i < 8; i++ {
		res, err := client.PostForm(running.URL+"/api/login", url.Values{"password": {"not-the-password"}})
		if err != nil {
			t.Fatal(err)
		}
		body := readAll(t, res)
		if res.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "The password is not right.") {
			t.Fatalf("attempt %d: %d %s", i, res.StatusCode, body)
		}
	}
	res, err := client.PostForm(running.URL+"/api/login", url.Values{"password": {"test-password-1"}})
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, res)
	if res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("locked %d %s", res.StatusCode, body)
	}
}

func TestSocketLifecycle(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	st := openAt(t, dir)
	if err := st.SetPassword(context.Background(), "test-password-1"); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "crossbeam.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	running, err := Start(context.Background(), st, Config{Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		running.Close()
		running.Wait()
	})
	st2 := openAt(t, filepath.Join(t.TempDir(), "other"))
	if err := st2.SetPassword(context.Background(), "test-password-1"); err != nil {
		t.Fatal(err)
	}
	_, err = Start(context.Background(), st2, Config{Listen: "127.0.0.1:0", SocketPath: running.SocketPath})
	if err == nil || !strings.Contains(err.Error(), "socket is already in use") {
		t.Fatalf("second start: %v", err)
	}
	conn, err := net.Dial("unix", running.SocketPath)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestCertificate(t *testing.T) {
	dir := t.TempDir()
	pair, sha, err := EnsureCert(dir, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) != 32*2+31 || !strings.Contains(sha, ":") {
		t.Fatalf("fingerprint %s", sha)
	}
	leaf := pair.Leaf
	if leaf.NotBefore.After(time.Now()) || leaf.NotBefore.Before(time.Now().Add(-2*time.Minute)) {
		t.Fatalf("not before %s", leaf.NotBefore)
	}
	life := time.Until(leaf.NotAfter)
	if life < 396*24*time.Hour || life > 398*24*time.Hour {
		t.Fatalf("lifetime %s", life)
	}
	if leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 || leaf.KeyUsage&x509.KeyUsageKeyEncipherment == 0 {
		t.Fatalf("usage %v", leaf.KeyUsage)
	}
	if len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth {
		t.Fatalf("eku %v", leaf.ExtKeyUsage)
	}
	if !sanMatches(leaf, "127.0.0.1") || !sanMatches(leaf, "localhost") {
		t.Fatal("san")
	}
	info, err := os.Stat(filepath.Join(dir, "tls", "key.pem"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %v %v", info, err)
	}
	again, sha2, err := EnsureCert(dir, "127.0.0.1")
	if err != nil || sha2 != sha || again.Leaf.SerialNumber.Cmp(leaf.SerialNumber) != 0 {
		t.Fatal("cert was regenerated")
	}
	if err := os.Chmod(filepath.Join(dir, "tls", "key.pem"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, _, err := EnsureCert(dir, "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(filepath.Join(dir, "tls", "key.pem"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key stayed group-readable: %v", info.Mode().Perm())
	}
	_, sha3, err := EnsureCert(dir, "127.0.0.2")
	if err != nil || sha3 == sha {
		t.Fatal("host change did not replace the cert")
	}
	short := &x509.Certificate{
		NotAfter:    time.Now().Add(3 * 24 * time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	if certReusable(short, "127.0.0.1", time.Now()) {
		t.Fatal("expiring cert reused")
	}
	long := strings.Repeat("a", 50) + "." + strings.Repeat("b", 20)
	longPair, _, err := EnsureCert(t.TempDir(), long)
	if err != nil {
		t.Fatal(err)
	}
	if !sanMatches(longPair.Leaf, long) || len(longPair.Leaf.Subject.CommonName) > 64 {
		t.Fatalf("long name cn %q", longPair.Leaf.Subject.CommonName)
	}
}

func TestWebAssets(t *testing.T) {
	js, err := webFS.ReadFile("web/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), "form.title") || !strings.Contains(string(js), "namedItem") || !strings.Contains(string(js), "previewGen") {
		t.Fatal("compose script should read fields with namedItem and ignore a stale preview")
	}
	css, err := webFS.ReadFile("web/app.css")
	if err != nil {
		t.Fatal(err)
	}
	text := string(css)
	for _, needle := range []string{"#f3efe6", "#1a1916", "#1e4d3a", "focus-visible", "max-width: 800px"} {
		if !strings.Contains(text, needle) {
			t.Errorf("css missing %s", needle)
		}
	}
	if _, err := ParseSocketMode("0666"); err == nil {
		t.Fatal("0666 accepted")
	}
	if _, err := ParseSocketMode("0600"); err != nil {
		t.Fatal(err)
	}
}

func TestLimiterKeepsLock(t *testing.T) {
	l := newLimiter()
	for i := 0; i < maxFailures; i++ {
		l.fail("victim")
	}
	if !l.locked("victim") {
		t.Fatal("victim was not locked")
	}
	id := make([]byte, 4)
	for i := 0; i < maxClients; i++ {
		id[0] = byte(i)
		id[1] = byte(i >> 8)
		l.fail(string(id))
	}
	if !l.locked("victim") {
		t.Fatal("a full table cleared the lock")
	}
}

func TestLimiterCountsInFlight(t *testing.T) {
	l := newLimiter()
	for i := 0; i < maxFailures; i++ {
		if !l.start("burst") {
			t.Fatalf("start %d", i)
		}
	}
	if l.start("burst") {
		t.Fatal("a burst passed the cap")
	}
	l.cancel("burst")
	if !l.start("burst") {
		t.Fatal("cancel did not free a sign-in slot")
	}
}

func TestRemoteIPIgnoresForwardedHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	req.RemoteAddr = "192.0.2.10:40000"
	req.Header.Set("X-Forwarded-For", "198.51.100.2")
	req.Header.Set("X-Real-IP", "203.0.113.5")
	if remoteIP(req) != "192.0.2.10" {
		t.Fatalf("remote %s", remoteIP(req))
	}
}

func TestCleanErrHidesInternals(t *testing.T) {
	if cleanErr(errors.New("title is empty")) != "title is empty" {
		t.Fatal("validation error was hidden")
	}
	for _, text := range []string{"sqlite: no such table", "open /var/lib/crossbeam/crossbeam.db", "bad\nline"} {
		if cleanErr(errors.New(text)) != "The change was rejected." {
			t.Fatalf("echoed %q", text)
		}
	}
}

func TestUntrustedRequestEdges(t *testing.T) {
	running, st, client := startPanel(t)
	anon := trustClient(t, filepath.Join(st.Dir(), "tls", "cert.pem"))
	anon.Jar = nil
	for _, path := range []string{"/api/articles/1", "/api/articles/nope", "/articles/1", "/articles/no"} {
		res, err := anon.Get(running.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		readAll(t, res)
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %d", path, res.StatusCode)
		}
	}
	req, err := http.NewRequest(http.MethodPut, running.URL+"/api/articles/nope", strings.NewReader(`{"title":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "198.51.100.9")
	res, err := anon.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("put %d", res.StatusCode)
	}
	req, err = http.NewRequest(http.MethodPost, running.URL+"/login?password=test-password-1", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err = anon.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body := readAll(t, res)
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "The password is not right.") {
		t.Fatalf("query password %d %s", res.StatusCode, body)
	}

	res, err = client.PostForm(running.URL+"/login", url.Values{"password": {"test-password-1"}})
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	res, err = client.Get(running.URL + "/settings")
	if err != nil {
		t.Fatal(err)
	}
	token := csrfFrom(t, readAll(t, res))
	res, err = client.PostForm(running.URL+"/settings?csrf="+url.QueryEscape(token)+"&station.name=Evil", url.Values{
		"station.name": {"Kept"},
	})
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("query csrf %d", res.StatusCode)
	}
	got, err := st.Settings(context.Background())
	if err != nil || got[settings.KeyName] == "Evil" || got[settings.KeyName] == "Kept" {
		t.Fatalf("query settings applied: %v %v", got, err)
	}
	res, err = client.PostForm(running.URL+"/settings?station.name=Evil", url.Values{
		"csrf":         {token},
		"station.name": {"Kept"},
	})
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, res)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("body save %d", res.StatusCode)
	}
	got, err = st.Settings(context.Background())
	if err != nil || got[settings.KeyName] != "Kept" {
		t.Fatalf("stored %v %v", got, err)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("cache %s", res.Header.Get("Cache-Control"))
	}
}

func startPanel(t *testing.T) (*Running, *store.Store, *http.Client) {
	t.Helper()
	st := openState(t)
	if err := st.SetPassword(context.Background(), "test-password-1"); err != nil {
		t.Fatal(err)
	}
	running, err := Start(context.Background(), st, Config{Listen: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		running.Close()
		running.Wait()
	})
	return running, st, trustClient(t, filepath.Join(st.Dir(), "tls", "cert.pem"))
}

func openState(t *testing.T) *store.Store {
	t.Helper()
	return openAt(t, filepath.Join(t.TempDir(), "state"))
}

func openAt(t *testing.T, dir string) *store.Store {
	t.Helper()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func trustClient(t *testing.T, certPath string) *http.Client {
	t.Helper()
	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		t.Fatal("certificate")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		},
	}
}

func readAll(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func csrfFrom(t *testing.T, body string) string {
	t.Helper()
	const needle = `name="csrf" value="`
	i := strings.Index(body, needle)
	if i < 0 {
		t.Fatalf("csrf not in %s", body)
	}
	rest := body[i+len(needle):]
	j := strings.Index(rest, `"`)
	if j != 64 {
		t.Fatalf("csrf length %d", j)
	}
	return rest[:j]
}
