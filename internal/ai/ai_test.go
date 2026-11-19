// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/settings"
)

// fakeServer is an AI server. answer picks the reply for each request.
type fakeServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []fakeRequest
}

type fakeRequest struct {
	Path   string
	Auth   string
	Body   map[string]any
	Msgs   []Message
	Cancel bool
}

func newFake(t *testing.T, backend string, answer func(r fakeRequest) (int, string, time.Duration)) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		req := fakeRequest{Path: r.URL.Path, Auth: r.Header.Get("Authorization")}
		_ = json.Unmarshal(raw, &req.Body)
		var msgs struct {
			Messages []Message `json:"messages"`
		}
		_ = json.Unmarshal(raw, &msgs)
		req.Msgs = msgs.Messages
		code, text, delay := answer(req)
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				req.Cancel = true
				f.add(req)
				return
			}
		}
		f.add(req)
		w.WriteHeader(code)
		if code != http.StatusOK {
			_, _ = io.WriteString(w, `{"error":"boom"}`)
			return
		}
		var out any
		if backend == settings.AIBackendOpenAI {
			out = map[string]any{"choices": []any{map[string]any{"message": Message{Role: "assistant", Content: text}}}}
		} else {
			out = map[string]any{"message": Message{Role: "assistant", Content: text}, "done": true}
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeServer) add(r fakeRequest) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r)
}

func (f *fakeServer) all() []fakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeRequest(nil), f.requests...)
}

func testSettings(t *testing.T, patch map[string]string) func(context.Context) (map[string]string, error) {
	t.Helper()
	values := map[string]string{
		settings.KeyCallsign:  "K5LEB-1",
		settings.KeyAIEnabled: "true",
		settings.KeyAIModel:   "llama3.2:3b",
	}
	for k, v := range patch {
		values[k] = v
	}
	got, err := settings.Validate(values)
	if err != nil {
		t.Fatal(err)
	}
	return func(context.Context) (map[string]string, error) { return got, nil }
}

func newService(t *testing.T, patch map[string]string) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	return New(testSettings(t, patch), dir), dir
}

var radioCaller = Caller{Call: "W5ABC-7", Radio: true, Via: "radio"}

func TestOllamaRequest(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(fakeRequest) (int, string, time.Duration) {
		return 200, "<think>let me see</think>**Packet** radio sends data \u2014 in frames.", 0
	})
	svc, dir := newService(t, map[string]string{settings.KeyAIURL: fake.URL, settings.KeyAIInstructions: "Mention LAARK."})
	if err := WriteKey(dir, "sk-secret-123"); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Message(context.Background(), Caller{Call: "ab12", Mesh: true}, "AI what is packet")
	if !strings.Contains(got, "not offered over MeshCore") {
		t.Fatalf("mesh off: %q", got)
	}
	reply, err := svc.Test(context.Background(), "what is packet")
	if err != nil {
		t.Fatal(err)
	}
	if reply != "Packet radio sends data - in frames." {
		t.Fatalf("reply %q", reply)
	}
	reqs := fake.all()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	r := reqs[0]
	if r.Path != "/api/chat" || r.Body["model"] != "llama3.2:3b" || r.Body["stream"] != false {
		t.Fatalf("request %+v", r)
	}
	opts, _ := r.Body["options"].(map[string]any)
	if opts["num_predict"] == nil {
		t.Fatalf("no num_predict: %+v", r.Body)
	}
	if r.Auth != "Bearer sk-secret-123" {
		t.Fatalf("auth %q", r.Auth)
	}
	if len(r.Msgs) != 2 || r.Msgs[0].Role != "system" || !strings.Contains(r.Msgs[0].Content, "K5LEB-1") ||
		!strings.Contains(r.Msgs[0].Content, "profanity") || !strings.Contains(r.Msgs[0].Content, "Mention LAARK.") {
		t.Fatalf("messages %+v", r.Msgs)
	}
	// The log has the question and the reply but never the key.
	raw, err := os.ReadFile(filepath.Join(dir, LogName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-secret") || !strings.Contains(string(raw), "what is packet") {
		t.Fatalf("log %s", raw)
	}
	info, _ := os.Stat(filepath.Join(dir, LogName))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("log mode %v", info.Mode())
	}
}

func TestOpenAIEndpoints(t *testing.T) {
	cases := map[string]string{
		"":                      "/v1/chat/completions",
		"/v1":                   "/v1/chat/completions",
		"/api":                  "/api/chat/completions",
		"/api/chat/completions": "/api/chat/completions",
		"/custom/path":          "/custom/path",
	}
	for suffix, want := range cases {
		fake := newFake(t, settings.AIBackendOpenAI, func(fakeRequest) (int, string, time.Duration) { return 200, "Hello from Open WebUI.", 0 })
		svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL + suffix, settings.KeyAIBackend: "openai"})
		reply, err := svc.Test(context.Background(), "hi")
		if err != nil || reply != "Hello from Open WebUI." {
			t.Fatalf("%q: %q %v", suffix, reply, err)
		}
		r := fake.all()[0]
		if r.Path != want || r.Body["max_tokens"] == nil || r.Auth != "" {
			t.Fatalf("%q: %+v", suffix, r)
		}
	}
}

func TestServerErrors(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(r fakeRequest) (int, string, time.Duration) {
		switch r.Msgs[len(r.Msgs)-1].Content {
		case "fail":
			return 500, "", 0
		case "slow":
			return 200, "late", 3 * time.Second
		case "empty":
			return 200, "  ", 0
		}
		return 200, "ok", 0
	})
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL, settings.KeyAITimeout: "5"})
	cfg, _ := svc.Config(context.Background())
	if r := svc.ask(context.Background(), cfg, radioCaller, nil, "fail", 600, time.Second); r.ok || r.text != "The AI server returned an error." {
		t.Fatalf("500: %+v", r)
	}
	if r := svc.ask(context.Background(), cfg, radioCaller, nil, "slow", 600, 200*time.Millisecond); r.ok || r.text != "The AI server did not answer in time." {
		t.Fatalf("timeout: %+v", r)
	}
	if r := svc.ask(context.Background(), cfg, radioCaller, nil, "empty", 600, time.Second); r.ok || r.outcome != OutError {
		t.Fatalf("empty: %+v", r)
	}
	down, _ := newService(t, map[string]string{settings.KeyAIURL: "http://127.0.0.1:1"})
	cfg2, _ := down.Config(context.Background())
	if r := down.ask(context.Background(), cfg2, radioCaller, nil, "hi", 600, time.Second); r.ok || r.text != "The AI server could not be reached." {
		t.Fatalf("down: %+v", r)
	}
}

func TestFilterWithholdsReply(t *testing.T) {
	answers := map[string]string{
		"rude":   "Well that is some bullshit right there.",
		"code":   "Here: aGVsbG8gd29ybGQgdGhpcyBpcyBhIHNlY3JldCBtZXNzYWdl",
		"normal": "See https://www.arrl.org/what-is-ham-radio-and-how-to-get-started for more.",
	}
	fake := newFake(t, settings.AIBackendOllama, func(r fakeRequest) (int, string, time.Duration) {
		return 200, answers[r.Msgs[len(r.Msgs)-1].Content], 0
	})
	svc, dir := newService(t, map[string]string{settings.KeyAIURL: fake.URL})
	cfg, _ := svc.Config(context.Background())
	for _, q := range []string{"rude", "code"} {
		r := svc.ask(context.Background(), cfg, radioCaller, nil, q, 600, time.Second)
		if r.ok || r.outcome != OutWithheld || !strings.Contains(r.text, "withheld") {
			t.Fatalf("%s: %+v", q, r)
		}
	}
	if r := svc.ask(context.Background(), cfg, radioCaller, nil, "normal", 600, time.Second); !r.ok {
		t.Fatalf("normal: %+v", r)
	}
	// A profane question never reaches the server.
	before := len(fake.all())
	if r := svc.ask(context.Background(), cfg, radioCaller, nil, "what the fuck", 600, time.Second); r.outcome != OutRefused {
		t.Fatalf("question: %+v", r)
	}
	if len(fake.all()) != before {
		t.Fatal("refused question was sent")
	}
	entries, err := ReadLog(dir, 0)
	if err != nil || len(entries) != 4 || entries[0].Outcome != OutWithheld || entries[0].Reply == "" || entries[3].Outcome != OutRefused {
		t.Fatalf("log %+v %v", entries, err)
	}
}

func TestAllowed(t *testing.T) {
	on := Config{Enabled: true}
	cases := []struct {
		cfg  Config
		who  Caller
		want bool
	}{
		{Config{}, radioCaller, false},
		{on, radioCaller, true},
		{on, Caller{Call: "W5ABC", Via: "link"}, false},
		{on, Caller{Call: "", Via: "telnet"}, false},
		{Config{Enabled: true, Users: []string{"W5ABC"}}, Caller{Call: "W5ABC-3", Via: "link"}, true},
		{Config{Enabled: true, Users: []string{"K5LEB"}}, radioCaller, false},
		{on, Caller{Call: "abcd", Mesh: true}, false},
		{Config{Enabled: true, Mesh: true}, Caller{Call: "abcd", Mesh: true}, true},
		// The approval gate is not part of Allowed. Radio stays open.
		{Config{Enabled: true, RequireApproval: true}, radioCaller, true},
		{Config{Enabled: true, Mesh: true, RequireApproval: true}, Caller{Call: "abcd", Mesh: true}, true},
	}
	for i, c := range cases {
		if got, why := c.cfg.Allowed(c.who); got != c.want || (!got && why == "") {
			t.Errorf("case %d: %v %q", i, got, why)
		}
	}
}

func TestRateLimit(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(fakeRequest) (int, string, time.Duration) { return 200, "ok", 0 })
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL, settings.KeyAIHourly: "2"})
	now := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	cfg, _ := svc.Config(context.Background())
	for i := 0; i < 2; i++ {
		if r := svc.ask(context.Background(), cfg, Caller{Call: "W5ABC-1", Radio: true}, nil, "q", 600, time.Second); !r.ok {
			t.Fatalf("ask %d: %+v", i, r)
		}
	}
	// A new SSID is the same operator.
	r := svc.ask(context.Background(), cfg, Caller{Call: "W5ABC-2", Radio: true}, nil, "q", 600, time.Second)
	if r.outcome != OutLimited || !strings.Contains(r.text, "60 minutes") {
		t.Fatalf("limited: %+v", r)
	}
	if r := svc.ask(context.Background(), cfg, Caller{Call: "K5XYZ", Radio: true}, nil, "q", 600, time.Second); !r.ok {
		t.Fatalf("other caller: %+v", r)
	}
	now = now.Add(61 * time.Minute)
	if r := svc.ask(context.Background(), cfg, Caller{Call: "W5ABC", Radio: true}, nil, "q", 600, time.Second); !r.ok {
		t.Fatalf("after an hour: %+v", r)
	}
}

func TestBusy(t *testing.T) {
	release := make(chan struct{})
	fake := newFake(t, settings.AIBackendOllama, func(fakeRequest) (int, string, time.Duration) {
		<-release
		return 200, "ok", 0
	})
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL})
	cfg, _ := svc.Config(context.Background())
	var wg sync.WaitGroup
	for i := 0; i < slots; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.ask(context.Background(), cfg, Caller{Call: "K5AAA", Radio: true}, nil, "q", 600, 5*time.Second)
		}()
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(svc.slots) < slots && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if r := svc.ask(context.Background(), cfg, Caller{Call: "K5BBB", Radio: true}, nil, "q", 600, time.Second); r.outcome != OutBusy {
		t.Fatalf("busy: %+v", r)
	}
	close(release)
	wg.Wait()
}

func TestBusyDoesNotUseTheHourlyLimit(t *testing.T) {
	release := make(chan struct{})
	fake := newFake(t, settings.AIBackendOllama, func(fakeRequest) (int, string, time.Duration) {
		<-release
		return 200, "ok", 0
	})
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL, settings.KeyAIHourly: "1"})
	cfg, _ := svc.Config(context.Background())
	var wg sync.WaitGroup
	for _, call := range []string{"K5AAA", "K5CCC"} {
		wg.Add(1)
		go func(call string) {
			defer wg.Done()
			svc.ask(context.Background(), cfg, Caller{Call: call, Radio: true}, nil, "q", 600, 5*time.Second)
		}(call)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(svc.slots) < slots && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	who := Caller{Call: "K5BBB", Radio: true}
	if r := svc.ask(context.Background(), cfg, who, nil, "q", 600, time.Second); r.outcome != OutBusy {
		t.Fatalf("busy: %+v", r)
	}
	close(release)
	wg.Wait()
	if r := svc.ask(context.Background(), cfg, who, nil, "q", 600, time.Second); !r.ok {
		t.Fatalf("a busy rejection used the hourly question: %+v", r)
	}
}

func TestRedirectIsNotFollowed(t *testing.T) {
	var hits atomic.Int32
	var forwarded atomic.Value
	secret := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		forwarded.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"leaked"}}`)
	}))
	t.Cleanup(secret.Close)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, secret.URL+"/api/chat", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(fake.Close)
	svc, dir := newService(t, map[string]string{settings.KeyAIURL: fake.URL})
	if err := WriteKey(dir, "sk-redirect-secret"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Test(context.Background(), "where does this go")
	if err == nil {
		t.Fatal("a redirect was treated as an answer")
	}
	if hits.Load() != 0 {
		t.Fatalf("the question was sent to the redirect target %d times, auth %q", hits.Load(), forwarded.Load())
	}
	if strings.Contains(err.Error(), "sk-redirect-secret") || strings.Contains(err.Error(), secret.URL) {
		t.Fatalf("error leaks the redirect target or the key: %v", err)
	}
}

func TestText(t *testing.T) {
	if got := Plain("# Title\n\n\n* one\n* two\t\u201cquoted\u201d \U0001F4E1"); got != "Title\n\n- one\n- two \"quoted\"" {
		t.Fatalf("plain %q", got)
	}
	long := strings.Repeat("word ", 50)
	cut := Cut(long, 60)
	if len(cut) > 60 || !strings.HasSuffix(cut, " [cut]") {
		t.Fatalf("cut %q", cut)
	}
	for _, l := range Wrap(strings.Repeat("abcdefgh ", 30)+strings.Repeat("x", 100), Width) {
		if len(l) > Width {
			t.Fatalf("wrap %q", l)
		}
	}
	if !Profane("You are a BASTARD.") || Profane("Scunthorpe has a classic assembly hall") {
		t.Fatal("profane")
	}
	if Obscene("buy this scam", []string{"scam"}) == false || Obscene("a scamper", []string{"scam"}) || Obscene("classic", []string{"class"}) {
		t.Fatal("extra banned words")
	}
	encoded := []string{
		"5f4dcc3b5aa765d61d8327deb882cf99aa11bb22",
		"mjMetNJURppDnZvZCakbEyeWfHTbFLerOIEuZSjQHeEYjfWmqAqnLH",
		"qzxwkvbnmrtpylkjhgfdsqzxwkvbnmrtpy",
		"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ",
	}
	plain := []string{
		"internationalization-characteristically",
		"pneumonoultramicroscopicsilicovolcanoconiosis",
		"thequickbrownfoxjumpsoverthelazydog",
		"https://example.com/a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6",
		"W5ABC", "K5LEB-1", "EM12", "EM12kp", "FN31pr",
		"repeater", "callsign", "packet",
	}
	for _, s := range encoded {
		if !Encoded(s) {
			t.Fatalf("not encoded: %s", s)
		}
	}
	for _, s := range plain {
		if Encoded(s) {
			t.Fatalf("false encoded: %s", s)
		}
	}
}

func TestExtraBannedWords(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(r fakeRequest) (int, string, time.Duration) {
		if strings.Contains(r.Msgs[len(r.Msgs)-1].Content, "weather") {
			return 200, "Watch out for that scam on the net.", 0
		}
		return 200, "The repeater is on 146.52.", 0
	})
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL, settings.KeyAIBannedWords: "scam"})
	cfg, _ := svc.Config(context.Background())
	if len(cfg.Banned) != 1 || cfg.Banned[0] != "scam" {
		t.Fatalf("banned %+v", cfg.Banned)
	}
	before := len(fake.all())
	if r := svc.ask(context.Background(), cfg, radioCaller, nil, "is this a scam", 600, time.Second); r.outcome != OutRefused {
		t.Fatalf("question: %+v", r)
	}
	if len(fake.all()) != before {
		t.Fatal("banned question was sent")
	}
	if r := svc.ask(context.Background(), cfg, radioCaller, nil, "a scamper on the hill", 600, time.Second); !r.ok {
		t.Fatalf("scamper: %+v", r)
	}
	if r := svc.ask(context.Background(), cfg, radioCaller, nil, "the weather", 600, time.Second); r.outcome != OutWithheld {
		t.Fatalf("reply: %+v", r)
	}
	reqs := fake.all()
	if !strings.Contains(reqs[len(reqs)-1].Msgs[0].Content, "scam") {
		t.Fatal("system prompt did not name the extra word")
	}
}

func TestKeyFile(t *testing.T) {
	dir := t.TempDir()
	if HasKey(dir) {
		t.Fatal("key before save")
	}
	if err := WriteKey(dir, "bad key"); err == nil {
		t.Fatal("space accepted")
	}
	if err := WriteKey(dir, "sk-abc"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, KeyName))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", info, err)
	}
	if key, _ := ReadKey(dir); key != "sk-abc" {
		t.Fatalf("key %q", key)
	}
	if err := WriteKey(dir, ""); err != nil || HasKey(dir) {
		t.Fatalf("clear %v", err)
	}
}

func TestHistoryBound(t *testing.T) {
	var h []Message
	for i := 0; i < 20; i++ {
		h = trimHistory(append(h, Message{Role: "user", Content: strings.Repeat("q", 300)}, Message{Role: "assistant", Content: strings.Repeat("a", 300)}))
	}
	size := 0
	for _, m := range h {
		size += len(m.Content)
	}
	if len(h) > 2*maxTurns || size > maxHistory || h[0].Role != "user" {
		t.Fatalf("history %d messages %d bytes", len(h), size)
	}
}

func TestCommandParse(t *testing.T) {
	if !IsCommand("ai") || !IsCommand("AI what is APRS") || IsCommand("AIR") || IsCommand("LIST") {
		t.Fatal("IsCommand")
	}
	if Question("ai  what is APRS ") != "what is APRS" || Question("AI") != "" {
		t.Fatal("Question")
	}
}

func TestMeshMessage(t *testing.T) {
	fake := newFake(t, settings.AIBackendOllama, func(r fakeRequest) (int, string, time.Duration) {
		if r.Msgs[len(r.Msgs)-1].Content == "long" {
			return 200, strings.Repeat("Mesh answer. ", 80), 0
		}
		return 200, "Answer to " + r.Msgs[len(r.Msgs)-1].Content, 0
	})
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL, settings.KeyAIMesh: "true"})
	if err := svc.Approve("meshcore:a1b2c3"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Approve("meshcore:ffff"); err != nil {
		t.Fatal(err)
	}
	mesh := Caller{Call: "a1b2c3", Mesh: true, Via: "meshcore"}
	if _, ok := svc.Message(context.Background(), mesh, "LIST"); ok {
		t.Fatal("LIST was taken as an AI message")
	}
	got, _ := svc.Message(context.Background(), mesh, "AI long")
	if len(got) > meshReply || !strings.HasSuffix(got, "[cut]") {
		t.Fatalf("mesh reply %d %q", len(got), got)
	}
	if got, _ := svc.Message(context.Background(), mesh, "AI"); got != MessageHelp {
		t.Fatalf("help %q", got)
	}
	svc.Message(context.Background(), mesh, "AI first")
	svc.Message(context.Background(), mesh, "AI second")
	reqs := fake.all()
	last := reqs[len(reqs)-1].Msgs
	if len(last) != 6 || last[3].Content != "first" {
		t.Fatalf("history not kept per contact: %+v", last)
	}
	// Another contact has its own history.
	svc.Message(context.Background(), Caller{Call: "ffff", Mesh: true}, "AI other")
	if n := len(fake.all()[len(fake.all())-1].Msgs); n != 2 {
		t.Fatalf("other contact saw %d messages", n)
	}
	if got, _ := svc.Message(context.Background(), mesh, "ai /new"); !strings.Contains(got, "new conversation") {
		t.Fatalf("/new %q", got)
	}
	svc.Message(context.Background(), mesh, "AI third")
	if n := len(fake.all()[len(fake.all())-1].Msgs); n != 2 {
		t.Fatalf("after /NEW %d messages", n)
	}
	// History expires after 30 idle minutes.
	svc.Message(context.Background(), mesh, "AI fourth")
	later := time.Now().Add(31 * time.Minute)
	svc.now = func() time.Time { return later }
	svc.Message(context.Background(), mesh, "AI fifth")
	if n := len(fake.all()[len(fake.all())-1].Msgs); n != 2 {
		t.Fatalf("after idle %d messages", n)
	}
}

func TestSysopErrors(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":"model \"llama3.2:3b\" not found, try pulling it first"}`)
	}))
	defer fake.Close()
	svc, _ := newService(t, map[string]string{settings.KeyAIURL: fake.URL})
	_, err := svc.Test(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "ollama pull llama3.2:3b") {
		t.Fatalf("model missing: %v", err)
	}
	down, _ := newService(t, map[string]string{settings.KeyAIURL: "http://127.0.0.1:1"})
	_, err = down.Test(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "Ollama at http://127.0.0.1:1 could not be reached") || !strings.Contains(err.Error(), "OLLAMA_HOST=0.0.0.0:11434") {
		t.Fatalf("unreachable: %v", err)
	}
	// A caller on the air sees a short sentence, not the address.
	cfg, _ := svc.Config(context.Background())
	if r := svc.ask(context.Background(), cfg, radioCaller, nil, "hi", 600, time.Second); r.text != "The AI model is not available right now." {
		t.Fatalf("caller text %q", r.text)
	}
}

func TestDefaultsTalkToOllamaDirectly(t *testing.T) {
	cfg := FromSettings(mustValidate(t, map[string]string{}), "")
	if cfg.Backend != "ollama" || cfg.URL != "http://127.0.0.1:11434" || cfg.Model != "llama3.2:3b" || cfg.Enabled || cfg.Mesh || cfg.Reticulum || !cfg.RequireApproval || len(cfg.Users) != 0 || len(cfg.Banned) != 0 {
		t.Fatalf("%+v", cfg)
	}
	if got := ollamaEndpoint(cfg.URL); got != "http://127.0.0.1:11434/api/chat" {
		t.Fatalf("endpoint %s", got)
	}
}

func mustValidate(t *testing.T, m map[string]string) map[string]string {
	t.Helper()
	got, err := settings.Validate(m)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
