// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package admin

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const certLifetime = 397 * 24 * time.Hour

// EnsureCert returns a certificate for hosts, creating or replacing the
// files under dir/tls. An existing certificate is kept when it is more
// than seven days from expiry and its names include every host. The
// port is not part of that check. The key file is mode 0600.
func EnsureCert(dir string, hosts ...string) (tls.Certificate, string, error) {
	if len(hosts) == 0 {
		return tls.Certificate{}, "", fmt.Errorf("certificate needs a name")
	}
	tlsDir := filepath.Join(dir, "tls")
	if err := os.MkdirAll(tlsDir, 0o700); err != nil {
		return tls.Certificate{}, "", err
	}
	if err := os.Chmod(tlsDir, 0o700); err != nil {
		return tls.Certificate{}, "", err
	}
	certPath := filepath.Join(tlsDir, "cert.pem")
	keyPath := filepath.Join(tlsDir, "key.pem")
	if _, err := os.Stat(keyPath); err == nil {
		if err := os.Chmod(keyPath, 0o600); err != nil {
			return tls.Certificate{}, "", err
		}
	}
	if pair, leaf, err := loadCert(certPath, keyPath); err == nil && certCovers(leaf, time.Now(), hosts...) {
		return pair, fingerprint(leaf.Raw), nil
	}
	return writeCert(certPath, keyPath, hosts)
}

// Fingerprint reads the saved certificate and returns the SHA-256 of
// its DER encoding as colon-separated hex.
func Fingerprint(dir string) (string, error) {
	pemBytes, err := os.ReadFile(filepath.Join(dir, "tls", "cert.pem"))
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return "", fmt.Errorf("certificate file is empty")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", err
	}
	return fingerprint(leaf.Raw), nil
}

func certReusable(leaf *x509.Certificate, host string, now time.Time) bool {
	return certCovers(leaf, now, host)
}

func certCovers(leaf *x509.Certificate, now time.Time, hosts ...string) bool {
	if leaf == nil || len(hosts) == 0 {
		return false
	}
	if !leaf.NotAfter.After(now.Add(7 * 24 * time.Hour)) {
		return false
	}
	for _, host := range hosts {
		if !sanMatches(leaf, host) {
			return false
		}
	}
	return true
}

func sanMatches(leaf *x509.Certificate, host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		for _, candidate := range leaf.IPAddresses {
			if candidate.Equal(ip) {
				return true
			}
		}
		return false
	}
	for _, name := range leaf.DNSNames {
		if strings.EqualFold(name, host) {
			return true
		}
	}
	return false
}

func certSANs(hosts []string) ([]string, []net.IP) {
	var dns []string
	var ips []net.IP
	seenDNS := map[string]struct{}{}
	seenIP := map[string]struct{}{}
	addDNS := func(name string) {
		key := strings.ToLower(name)
		if _, ok := seenDNS[key]; ok {
			return
		}
		seenDNS[key] = struct{}{}
		dns = append(dns, name)
	}
	addIP := func(ip net.IP) {
		if ip == nil {
			return
		}
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		if _, ok := seenIP[ip.String()]; ok {
			return
		}
		seenIP[ip.String()] = struct{}{}
		ips = append(ips, ip)
	}
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			addIP(ip)
			if ip.IsLoopback() {
				addDNS("localhost")
			}
			continue
		}
		if host == "" {
			continue
		}
		addDNS(host)
		if strings.EqualFold(host, "localhost") {
			addIP(net.ParseIP("127.0.0.1"))
			addIP(net.ParseIP("::1"))
		}
	}
	return dns, ips
}

// certCN picks a common name that fits the 64-octet limit. A longer
// hostname stays in the SAN only. Browsers match the SAN.
func certCN(dns []string, ips []net.IP) string {
	for _, name := range dns {
		if len(name) <= 64 {
			return name
		}
	}
	for _, ip := range ips {
		if text := ip.String(); len(text) <= 64 {
			return text
		}
	}
	return ""
}

func loadCert(certPath, keyPath string) (tls.Certificate, *x509.Certificate, error) {
	pair, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	if len(pair.Certificate) == 0 {
		return tls.Certificate{}, nil, fmt.Errorf("certificate file is empty")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	pair.Leaf = leaf
	return pair, leaf, nil
}

func writeCert(certPath, keyPath string, hosts []string) (tls.Certificate, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, "", err
	}
	if serial.Sign() == 0 {
		serial = big.NewInt(1)
	}
	dnsNames, ips := certSANs(hosts)
	if len(dnsNames) == 0 && len(ips) == 0 {
		return tls.Certificate{}, "", fmt.Errorf("certificate needs a name")
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: certCN(dnsNames, ips)},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(certLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
		IPAddresses:           ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, "", err
	}
	if err := writeKey(keyPath, keyPEM); err != nil {
		return tls.Certificate{}, "", err
	}
	pair, leaf, err := loadCert(certPath, keyPath)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	return pair, fingerprint(leaf.Raw), nil
}

func writeKey(path string, pemBytes []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(pemBytes); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func fingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	var b strings.Builder
	for i, value := range sum {
		if i > 0 {
			b.WriteByte(':')
		}
		fmt.Fprintf(&b, "%02X", value)
	}
	return b.String()
}
