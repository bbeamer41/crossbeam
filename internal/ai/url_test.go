// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"net"
	"testing"
)

func TestLocalURL(t *testing.T) {
	private := []net.IP{net.ParseIP("192.168.1.20")}
	public := []net.IP{net.ParseIP("203.0.113.5")}
	mixed := []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("203.0.113.5")}
	lookup := func(host string) ([]net.IP, error) {
		switch host {
		case "ollama.local", "node.ts.net":
			return private, nil
		case "public.local":
			return public, nil
		case "mixed.ts.net":
			return mixed, nil
		case "missing.local":
			return nil, nil
		default:
			t.Errorf("unexpected lookup %s", host)
			return nil, nil
		}
	}
	local := []string{
		"http://127.0.0.1:11434",
		"http://127.1.2.3:11434",
		"http://[::1]:11434",
		"http://10.1.2.3:11434",
		"http://192.168.1.50:11434",
		"http://172.16.0.1:11434",
		"http://100.64.0.1:11434",
		"http://100.127.255.1:11434",
		"http://169.254.1.1:11434",
		"http://[fd00::1]:11434",
		"http://[fe80::1]:11434",
		"http://localhost:11434",
		"http://localhost.:11434",
		"http://ollama.local:11434",
		"http://node.ts.net:11434",
		"http://192.168.1.50:3000/api/chat/completions",
	}
	publicURLs := []string{
		"http://203.0.113.5:11434",
		"http://8.8.8.8:11434",
		"http://100.128.0.1:11434",
		"http://172.32.0.1:11434",
		"http://[2001:db8::1]:11434",
		"http://example.com:11434",
		"http://ollama.example.com:11434",
		"http://notlocal:11434",
		"http://public.local:11434",
		"http://mixed.ts.net:11434",
		"http://missing.local:11434",
		"http://evil.local.example.com:11434",
	}
	for _, raw := range local {
		if got := URLWarning(raw, lookup); got != "" {
			t.Errorf("%s warned: %s", raw, got)
		}
	}
	for _, raw := range publicURLs {
		if got := URLWarning(raw, lookup); got != URLWarningText {
			t.Errorf("%s warning %q", raw, got)
		}
	}
	if URLWarning("", lookup) != "" {
		t.Fatal("empty url warned")
	}
	if URLWarning("http://example.com:11434", func(string) ([]net.IP, error) {
		t.Fatal("a public name was resolved")
		return nil, nil
	}) == "" {
		t.Fatal("example.com was treated as private")
	}
}
