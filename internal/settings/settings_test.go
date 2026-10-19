// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package settings

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateTable(t *testing.T) {
	longName := strings.Repeat("n", 81)
	longCall := strings.Repeat("c", 65)
	cases := []struct {
		name    string
		values  map[string]string
		wantErr string
		want    map[string]string
	}{
		{
			name:   "defaults",
			values: map[string]string{},
			want: map[string]string{
				KeyCallsign: "",
				KeyName:     "",
				KeyListen:   "",
				KeyPublic:   "false",
			},
		},
		{
			name: "private listen",
			values: map[string]string{
				KeyCallsign: " K5LEB ",
				KeyName:     "Home",
				KeyListen:   "192.168.1.20:8443",
				KeyPublic:   "false",
			},
			want: map[string]string{
				KeyCallsign: "K5LEB",
				KeyName:     "Home",
				KeyListen:   "192.168.1.20:8443",
				KeyPublic:   "false",
			},
		},
		{
			name:    "bool yes",
			values:  map[string]string{KeyPublic: "yes"},
			wantErr: "must be true or false",
		},
		{
			name:    "bool one",
			values:  map[string]string{KeyPublic: "1"},
			wantErr: "must be true or false",
		},
		{
			name:    "bool upper",
			values:  map[string]string{KeyPublic: "TRUE"},
			wantErr: "must be true or false",
		},
		{
			name:    "callsign length",
			values:  map[string]string{KeyCallsign: longCall},
			wantErr: "above the limit",
		},
		{
			name:    "name length",
			values:  map[string]string{KeyName: longName},
			wantErr: "above the limit",
		},
		{
			name:    "callsign newline",
			values:  map[string]string{KeyCallsign: "K5\nLEB"},
			wantErr: "single line",
		},
		{
			name:    "name control",
			values:  map[string]string{KeyName: "bad\x00"},
			wantErr: "control character",
		},
		{
			name:    "unknown",
			values:  map[string]string{"station.other": "x"},
			wantErr: "unknown setting",
		},
		{
			name:    "public ip",
			values:  map[string]string{KeyListen: "1.2.3.4:8443", KeyPublic: "false"},
			wantErr: "refuses public address",
		},
		{
			name: "public opt in",
			values: map[string]string{
				KeyListen: "1.2.3.4:8443",
				KeyPublic: "true",
			},
			want: map[string]string{
				KeyCallsign: "",
				KeyName:     "",
				KeyListen:   "1.2.3.4:8443",
				KeyPublic:   "true",
			},
		},
		{
			name:    "hostname saved without dns",
			values:  map[string]string{KeyListen: "panel.example:8443"},
			wantErr: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Validate(tc.values)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.want != nil {
				for key, value := range tc.want {
					if got[key] != value {
						t.Fatalf("%s = %q, want %q", key, got[key], value)
					}
				}
			}
		})
	}
}

func TestListenClassification(t *testing.T) {
	ok := []string{
		"",
		"10.1.2.3:8443",
		"172.16.0.1:8443",
		"192.168.1.20:8443",
		"127.0.0.1:8443",
		"[::1]:8443",
		"169.254.1.1:8443",
		"[fd00::1]:8443",
		"[fe80::1]:8443",
		"localhost:8443",
	}
	for _, listen := range ok {
		if err := ValidateListen(listen, false); err != nil {
			t.Fatalf("%s: %v", listen, err)
		}
	}
	bad := []string{
		"0.0.0.0:8443",
		"[::]:8443",
		"1.2.3.4:8443",
		"[::ffff:1.2.3.4]:8443",
		"8.8.8.8:53",
		"127.0.0.1:0",
		"192.168.1.20:8443%eth0",
		"[fe80::1%eth0]:8443",
		"not a listen",
		"192.168.1.20",
	}
	for _, listen := range bad {
		if err := ValidateListen(listen, false); err == nil {
			t.Fatalf("%s was accepted", listen)
		}
	}
	for _, listen := range []string{"0.0.0.0:8443", "[::]:8443", "8.8.8.8:53", "1.2.3.4:9"} {
		if err := ValidateListen(listen, true); err != nil {
			t.Fatalf("public %s: %v", listen, err)
		}
	}
	if err := BindAllowed("127.0.0.1:0", false); err != nil {
		t.Fatal(err)
	}
	if err := ValidateListen("127.0.0.1:0", false); err == nil {
		t.Fatal("saved port 0 was accepted")
	}
	if err := ValidateListen(strings.Repeat("a", 252)+":8443", false); err == nil {
		t.Fatal("long listen was accepted")
	}
}

func TestResolve(t *testing.T) {
	plan, err := Resolve("", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Name != "" || len(plan.Addrs) != 1 {
		t.Fatalf("auto plan %+v", plan)
	}
	host, port, err := net.SplitHostPort(plan.Addrs[0])
	if err != nil || port != "8443" {
		t.Fatalf("auto %q", plan.Addrs[0])
	}
	ip := net.ParseIP(host)
	if ip == nil || (!ip.IsLoopback() && !ip.IsPrivate()) {
		t.Fatalf("auto address %q is not private", plan.Addrs[0])
	}

	_, err = Resolve("panel.example:8443", false, func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "public address") {
		t.Fatalf("public name: %v", err)
	}
	_, err = Resolve("panel.example:8443", false, func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("192.168.1.20"), net.ParseIP("8.8.8.8")}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "public address") {
		t.Fatalf("mixed name: %v", err)
	}
	plan, err = Resolve("panel.example:8443", false, func(context.Context, string) ([]net.IP, error) {
		return []net.IP{
			net.ParseIP("192.168.1.20"),
			net.ParseIP("10.0.0.5"),
			net.ParseIP("::ffff:192.168.1.20"),
		}, nil
	})
	if err != nil || plan.Name != "panel.example" || len(plan.Addrs) != 2 || plan.Addrs[0] != "192.168.1.20:8443" || plan.Addrs[1] != "10.0.0.5:8443" {
		t.Fatalf("private name %+v %v", plan, err)
	}
	plan, err = ResolveBind("[::ffff:192.168.1.20]:0", false, nil)
	if err != nil || plan.Name != "" || len(plan.Addrs) != 1 || plan.Addrs[0] != "192.168.1.20:0" {
		t.Fatalf("mapped %+v %v", plan, err)
	}
	_, err = Resolve("[::ffff:1.2.3.4]:8443", false, nil)
	if err == nil || !strings.Contains(err.Error(), "public address") {
		t.Fatalf("mapped public: %v", err)
	}
	_, err = Resolve("missing.example:8443", false, func(context.Context, string) ([]net.IP, error) {
		return nil, &net.DNSError{Err: "no such host", Name: "missing.example", IsNotFound: true}
	})
	if err == nil || !strings.Contains(err.Error(), "cannot resolve") {
		t.Fatalf("dns failure: %v", err)
	}
}

func TestSettingsDocument(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "docs", "settings.md")
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(text)
	if len(Schema) != 4 {
		t.Fatalf("schema has %d keys", len(Schema))
	}
	for _, field := range Schema {
		if !strings.Contains(doc, field.Key) {
			t.Errorf("docs/settings.md is missing %s", field.Key)
		}
		if !strings.Contains(doc, field.Help) {
			t.Errorf("docs/settings.md is missing the help for %s", field.Key)
		}
	}
}
