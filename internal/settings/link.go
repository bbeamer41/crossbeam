// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package settings

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
)

// Peer is one station this node can call over AX/UDP.
type Peer struct {
	Call string
	Host string
	Port int
}

// Target is host:port, with brackets when the host is IPv6.
func (p Peer) Target() string {
	return net.JoinHostPort(p.Host, strconv.Itoa(p.Port))
}

// ParsePeers reads a link.peers list. Entries are separated by spaces or
// commas. An empty value is an empty list. Callsigns are stored in
// uppercase, and SSID 0 is omitted.
func ParsePeers(text string) ([]Peer, error) {
	return ParsePeerList("link.peers", text)
}

// ParsePeerList reads a peer list for the named setting. The entry rules
// match link.peers.
func ParsePeerList(key, text string) ([]Peer, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil
	}
	parts := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(parts) > 32 {
		return nil, fmt.Errorf("%s lists more than 32 stations", key)
	}
	seen := make(map[string]struct{}, len(parts))
	out := make([]Peer, 0, len(parts))
	for _, part := range parts {
		at := strings.Index(part, "@")
		if at <= 0 || at == len(part)-1 {
			return nil, fmt.Errorf("%s entry must be CALL@host:port", key)
		}
		call, err := ax25.Parse(part[:at])
		if err != nil {
			return nil, fmt.Errorf("%s entry must be CALL@host:port", key)
		}
		host, port, err := splitPeerNamed(key, part[at+1:])
		if err != nil {
			return nil, err
		}
		if _, ok := seen[call.String()]; ok {
			return nil, fmt.Errorf("%s lists %s more than once", key, call)
		}
		seen[call.String()] = struct{}{}
		out = append(out, Peer{Call: call.String(), Host: host, Port: port})
	}
	return out, nil
}

// FormatPeers renders peers in the stored form.
func FormatPeers(peers []Peer) string {
	parts := make([]string, len(peers))
	for i, peer := range peers {
		parts[i] = peer.Call + "@" + peer.Target()
	}
	return strings.Join(parts, " ")
}

// canonicalKiss checks kiss.tnc. Empty keeps the radio off. A hostname
// is saved without looking it up. Port 0 is rejected.
func canonicalKiss(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	host, port, err := splitKiss(text)
	if err != nil {
		return "", err
	}
	if err := checkKissPort(port); err != nil {
		return "", err
	}
	if ip := net.ParseIP(host); ip != nil {
		ip = canonicalIP(ip)
		if err := checkKissIP(ip); err != nil {
			return "", err
		}
		return net.JoinHostPort(ip.String(), port), nil
	}
	if !hostnameOK(host) {
		return "", fmt.Errorf("kiss.tnc must be host:port")
	}
	return net.JoinHostPort(host, port), nil
}

// ResolveKiss returns the one address to dial. A hostname must resolve
// to one address. DNS failure stops the start.
func ResolveKiss(text string, lookup LookupIP) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("kiss.tnc is empty")
	}
	host, port, err := splitKiss(text)
	if err != nil {
		return "", err
	}
	if err := checkKissPort(port); err != nil {
		return "", err
	}
	if ip := net.ParseIP(host); ip != nil {
		ip = canonicalIP(ip)
		if err := checkKissIP(ip); err != nil {
			return "", err
		}
		return net.JoinHostPort(ip.String(), port), nil
	}
	if lookup == nil {
		lookup = defaultLookup
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := lookup(ctx, host)
	if err != nil {
		return "", fmt.Errorf("cannot resolve %s: %w", host, err)
	}
	seen := map[string]struct{}{}
	var chosen net.IP
	for _, ip := range ips {
		ip = canonicalIP(ip)
		if ip == nil {
			continue
		}
		if _, ok := seen[ip.String()]; ok {
			continue
		}
		seen[ip.String()] = struct{}{}
		if err := checkKissIP(ip); err != nil {
			return "", err
		}
		chosen = ip
	}
	if len(seen) == 0 {
		return "", fmt.Errorf("cannot resolve %s", host)
	}
	if len(seen) > 1 {
		return "", fmt.Errorf("%s resolves to more than one address; set one IP in kiss.tnc", host)
	}
	return net.JoinHostPort(chosen.String(), port), nil
}

func splitKiss(text string) (string, string, error) {
	if len(text) > 256 {
		return "", "", fmt.Errorf("kiss.tnc is longer than 256 bytes")
	}
	if strings.Contains(text, "%") {
		return "", "", fmt.Errorf("kiss.tnc must not include a zone id")
	}
	host, port, err := net.SplitHostPort(text)
	if err != nil || host == "" || port == "" {
		return "", "", fmt.Errorf("kiss.tnc must be host:port")
	}
	return host, port, nil
}

func checkKissPort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || strconv.Itoa(n) != port || n < 1 || n > 65535 {
		return fmt.Errorf("kiss.tnc port must be between 1 and 65535")
	}
	return nil
}

func checkKissIP(ip net.IP) error {
	if ip == nil || ip.IsUnspecified() {
		return fmt.Errorf("kiss.tnc address is not valid")
	}
	if ip.IsMulticast() {
		return fmt.Errorf("kiss.tnc refuses a multicast address")
	}
	return nil
}

// ValidateLinkListen checks a saved link.listen value. Empty keeps the
// link off. Port 0 is rejected. A public address is allowed because the
// link is the packet port, not the admin panel. A hostname is not resolved.
func ValidateLinkListen(listen string) error {
	if strings.TrimSpace(listen) == "" {
		return nil
	}
	return checkLink(listen, false)
}

// ResolveLink returns the one address the link should bind. Port 0 is
// allowed when allowZero is set. A hostname is resolved now and must
// return one address. DNS failure stops the start.
func ResolveLink(listen string, lookup LookupIP, allowZero bool) (string, error) {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return "", fmt.Errorf("link.listen is empty")
	}
	host, port, err := splitLink(listen)
	if err != nil {
		return "", err
	}
	if err := checkLinkPort(port, allowZero); err != nil {
		return "", err
	}
	if ip := net.ParseIP(host); ip != nil {
		ip = canonicalIP(ip)
		if err := checkLinkIP(ip); err != nil {
			return "", err
		}
		return net.JoinHostPort(ip.String(), port), nil
	}
	if lookup == nil {
		lookup = defaultLookup
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := lookup(ctx, host)
	if err != nil {
		return "", fmt.Errorf("cannot resolve %s: %w", host, err)
	}
	seen := map[string]struct{}{}
	var chosen net.IP
	for _, ip := range ips {
		ip = canonicalIP(ip)
		if ip == nil {
			continue
		}
		if _, ok := seen[ip.String()]; ok {
			continue
		}
		seen[ip.String()] = struct{}{}
		if err := checkLinkIP(ip); err != nil {
			return "", err
		}
		chosen = ip
	}
	if len(seen) == 0 {
		return "", fmt.Errorf("cannot resolve %s", host)
	}
	if len(seen) > 1 {
		return "", fmt.Errorf("%s resolves to more than one address; set one IP in link.listen", host)
	}
	return net.JoinHostPort(chosen.String(), port), nil
}

func checkLink(listen string, allowZero bool) error {
	host, port, err := splitLink(listen)
	if err != nil {
		return err
	}
	if err := checkLinkPort(port, allowZero); err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil {
		return checkLinkIP(canonicalIP(ip))
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("link.listen must be host:port")
	}
	return nil
}

func splitLink(listen string) (string, string, error) {
	if len(listen) > 256 {
		return "", "", fmt.Errorf("link.listen is longer than 256 bytes")
	}
	if strings.Contains(listen, "%") {
		return "", "", fmt.Errorf("link.listen must not include a zone id")
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil || host == "" || port == "" {
		return "", "", fmt.Errorf("link.listen must be host:port")
	}
	return host, port, nil
}

func checkLinkPort(port string, allowZero bool) error {
	n, err := strconv.Atoi(port)
	if err != nil || strconv.Itoa(n) != port {
		return fmt.Errorf("link.listen port is not a number")
	}
	if n == 0 && allowZero {
		return nil
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf("link.listen port must be between 1 and 65535")
	}
	return nil
}

func checkLinkIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("link.listen address is not valid")
	}
	if ip.IsMulticast() {
		return fmt.Errorf("link.listen refuses a multicast address")
	}
	return nil
}

func splitPeer(target string) (string, int, error) {
	return splitPeerNamed("link.peers", target)
}

func splitPeerNamed(key, target string) (string, int, error) {
	bad := fmt.Errorf("%s entry must be CALL@host:port", key)
	if len(target) > 256 || strings.Contains(target, "%") {
		return "", 0, bad
	}
	host, portText, err := net.SplitHostPort(target)
	if err != nil || host == "" || portText == "" {
		return "", 0, bad
	}
	n, err := strconv.Atoi(portText)
	if err != nil || strconv.Itoa(n) != portText || n < 1 || n > 65535 {
		return "", 0, bad
	}
	if ip := net.ParseIP(host); ip != nil {
		ip = canonicalIP(ip)
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			return "", 0, bad
		}
		return ip.String(), n, nil
	}
	if !hostnameOK(host) {
		return "", 0, bad
	}
	return host, n, nil
}

// CanonicalListen checks a TCP listen address. Empty keeps that port off.
// A public address is allowed. Port 0 is rejected. A hostname is not resolved.
func CanonicalListen(key, text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	host, port, n, err := splitPacket(key, text, false)
	if err != nil {
		return "", err
	}
	return joinListen(key, host, port, n)
}

// ResolveListen returns the one address to bind. Port 0 is allowed when
// allowZero is set. A hostname must resolve to one address.
func ResolveListen(key, text string, lookup LookupIP, allowZero bool) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("%s is empty", key)
	}
	host, port, n, err := splitPacket(key, text, allowZero)
	if err != nil {
		return "", err
	}
	if ip := net.ParseIP(host); ip != nil {
		return joinListen(key, host, port, n)
	}
	if lookup == nil {
		lookup = defaultLookup
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := lookup(ctx, host)
	if err != nil {
		return "", fmt.Errorf("cannot resolve %s: %w", host, err)
	}
	seen := map[string]net.IP{}
	var chosen net.IP
	for _, ip := range ips {
		ip = canonicalIP(ip)
		if ip == nil || ip.IsMulticast() {
			continue
		}
		if _, ok := seen[ip.String()]; ok {
			continue
		}
		seen[ip.String()] = ip
		chosen = ip
	}
	if len(seen) == 0 {
		return "", fmt.Errorf("cannot resolve %s", host)
	}
	if len(seen) > 1 {
		return "", fmt.Errorf("%s resolves to more than one address; set one IP in %s", host, key)
	}
	return net.JoinHostPort(chosen.String(), port), nil
}

func splitPacket(key, text string, allowZero bool) (string, string, int, error) {
	if len(text) > 256 {
		return "", "", 0, fmt.Errorf("%s is longer than 256 bytes", key)
	}
	if strings.Contains(text, "%") {
		return "", "", 0, fmt.Errorf("%s must not include a zone id", key)
	}
	host, port, err := net.SplitHostPort(text)
	if err != nil || host == "" || port == "" {
		return "", "", 0, fmt.Errorf("%s must be host:port", key)
	}
	n, err := strconv.Atoi(port)
	if err != nil || strconv.Itoa(n) != port {
		return "", "", 0, fmt.Errorf("%s port is not a number", key)
	}
	if n == 0 && allowZero {
		return host, port, n, nil
	}
	if n < 1 || n > 65535 {
		return "", "", 0, fmt.Errorf("%s port must be between 1 and 65535", key)
	}
	return host, port, n, nil
}

func joinListen(key, host, port string, n int) (string, error) {
	if n == 0 {
		return net.JoinHostPort(host, port), nil
	}
	if ip := net.ParseIP(host); ip != nil {
		ip = canonicalIP(ip)
		if ip == nil {
			return "", fmt.Errorf("%s address is not valid", key)
		}
		if ip.IsMulticast() {
			return "", fmt.Errorf("%s refuses a multicast address", key)
		}
		return net.JoinHostPort(ip.String(), port), nil
	}
	if strings.TrimSpace(host) == "" || !hostnameOK(host) {
		return "", fmt.Errorf("%s must be host:port", key)
	}
	return net.JoinHostPort(host, port), nil
}

func hostnameOK(host string) bool {
	if host == "" || len(host) > 253 || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, c := range label {
			if c != '-' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
				return false
			}
		}
	}
	return true
}
