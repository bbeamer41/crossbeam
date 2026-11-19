// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"context"
	"net"
	"net/url"
	"strings"
	"time"
)

// URLWarningText is the sentence shown when ai.url is not a private address.
// It is a warning. The address is still used.
const URLWarningText = "This AI server address is not private. Questions will be sent off your network."

// URLWarning is URLWarningText when raw is not a local or private address,
// and empty when it is, or when raw is empty. A nil lookup uses the system
// resolver with a short timeout. Lookup is used only for names that end in
// .local or .ts.net.
func URLWarning(raw string, lookup func(string) ([]net.IP, error)) string {
	if strings.TrimSpace(raw) == "" || localURL(raw, lookup) {
		return ""
	}
	return URLWarningText
}

func localURL(raw string, lookup func(string) ([]net.IP, error)) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return privateIP(ip)
	}
	if host == "localhost" {
		return true
	}
	if !strings.HasSuffix(host, ".local") && !strings.HasSuffix(host, ".ts.net") {
		return false
	}
	if lookup == nil {
		lookup = lookupPrivate
	}
	ips, err := lookup(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !privateIP(ip) {
			return false
		}
	}
	return true
}

func lookupPrivate(host string) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// privateIP is true for loopback, link-local, RFC1918, IPv6 unique-local,
// and the carrier-grade range 100.64.0.0/10 used by Tailscale.
func privateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate() {
		return true
	}
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	// 100.64.0.0/10
	return v4[0] == 100 && v4[1]&0xc0 == 64
}
