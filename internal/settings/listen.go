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
)

// LookupIP resolves a hostname to addresses. Resolve uses the system
// resolver when lookup is nil. Names are not resolved when a setting is saved.
type LookupIP func(ctx context.Context, host string) ([]net.IP, error)

// ValidateListen checks a saved admin.listen value. An empty value means
// the next start chooses a private address. Port 0 is rejected. A hostname
// is not resolved here, so a save works with no network.
func ValidateListen(listen string, public bool) error {
	if listen == "" {
		return nil
	}
	return checkListen(listen, public, false)
}

// BindAllowed checks an address Start is about to bind. Port 0 is allowed
// so tests can ask the kernel for a free port. A hostname is not resolved
// here; call Resolve first.
func BindAllowed(listen string, public bool) error {
	if listen == "" {
		return fmt.Errorf("admin.listen is empty")
	}
	return checkListen(listen, public, true)
}

// Plan is the concrete bind for one start. Addrs are passed to
// net.Listen. A hostname is resolved once and every returned address is
// listed, so a later lookup cannot move the socket. Name is that
// hostname; it is empty when the operator set an IP or left the
// address to be chosen.
type Plan struct {
	Addrs []string
	Name  string
}

// Resolve turns a saved listen value into the addresses Start should
// bind. An empty value becomes a private LAN address on port 8443, or
// 127.0.0.1:8443 when the host has none. A hostname is resolved now.
// Every result must be a local address unless public is set. A DNS
// failure stops the start. Port 0 is rejected.
func Resolve(listen string, public bool, lookup LookupIP) (Plan, error) {
	return resolve(listen, public, lookup, false)
}

// ResolveBind is Resolve for an address this process is about to bind.
// Port 0 is allowed so a test can ask the kernel for a free port.
func ResolveBind(listen string, public bool, lookup LookupIP) (Plan, error) {
	return resolve(listen, public, lookup, true)
}

func resolve(listen string, public bool, lookup LookupIP, allowZero bool) (Plan, error) {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		listen = autoListen()
	}
	host, port, err := splitListen(listen)
	if err != nil {
		return Plan{}, err
	}
	if err := checkPort(port, allowZero); err != nil {
		return Plan{}, err
	}
	if ip := net.ParseIP(host); ip != nil {
		if err := classify(ip, public); err != nil {
			return Plan{}, err
		}
		return Plan{Addrs: []string{net.JoinHostPort(canonicalIP(ip).String(), port)}}, nil
	}
	if lookup == nil {
		lookup = defaultLookup
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := lookup(ctx, host)
	if err != nil {
		return Plan{}, fmt.Errorf("cannot resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return Plan{}, fmt.Errorf("cannot resolve %s", host)
	}
	seen := map[string]struct{}{}
	addrs := make([]string, 0, len(ips))
	for _, ip := range ips {
		if err := classify(ip, public); err != nil {
			return Plan{}, fmt.Errorf("cannot use %s: it resolved to a public address", host)
		}
		dial := net.JoinHostPort(canonicalIP(ip).String(), port)
		if _, ok := seen[dial]; ok {
			continue
		}
		seen[dial] = struct{}{}
		addrs = append(addrs, dial)
	}
	return Plan{Addrs: addrs, Name: host}, nil
}

func defaultLookup(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

func checkListen(listen string, public bool, allowZero bool) error {
	host, port, err := splitListen(listen)
	if err != nil {
		return err
	}
	if err := checkPort(port, allowZero); err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil {
		return classify(ip, public)
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("admin.listen must be host:port")
	}
	return nil
}

func splitListen(listen string) (string, string, error) {
	if len(listen) > 256 {
		return "", "", fmt.Errorf("admin.listen is longer than 256 bytes")
	}
	if strings.Contains(listen, "%") {
		return "", "", fmt.Errorf("admin.listen must not include a zone id")
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil || host == "" || port == "" {
		return "", "", fmt.Errorf("admin.listen must be host:port")
	}
	return host, port, nil
}

func checkPort(port string, allowZero bool) error {
	n, err := strconv.Atoi(port)
	if err != nil || strconv.Itoa(n) != port {
		return fmt.Errorf("admin.listen port is not a number")
	}
	if n == 0 && allowZero {
		return nil
	}
	if n < 1 || n > 65535 {
		return fmt.Errorf("admin.listen port must be between 1 and 65535")
	}
	return nil
}

func classify(ip net.IP, public bool) error {
	ip = canonicalIP(ip)
	if ip == nil {
		return fmt.Errorf("admin.listen address is not valid")
	}
	if ip.IsUnspecified() {
		if public {
			return nil
		}
		return fmt.Errorf("admin.listen refuses an unspecified address")
	}
	if public || isLocal(ip) {
		return nil
	}
	return fmt.Errorf("admin.listen refuses public address %s", ip)
}

func canonicalIP(ip net.IP) net.IP {
	if ip == nil {
		return nil
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return ip
}

func isLocal(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}

// autoListen picks the first global-unicast private IPv4 on a
// non-loopback interface. It never picks a public address or 0.0.0.0.
func autoListen() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "127.0.0.1:8443"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || !ip.IsPrivate() || !ip.IsGlobalUnicast() {
				continue
			}
			return net.JoinHostPort(ip.String(), "8443")
		}
	}
	return "127.0.0.1:8443"
}
