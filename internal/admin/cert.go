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

// EnsureCert returns a certificate for host, creating or replacing the
// files under dir/tls. An existing certificate is kept when it is more
// than seven days from expiry and its names include host. The port is
// not part of that check. The key file is mode 0600.
func EnsureCert(dir, host string) (tls.Certificate, string, error) {
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
	if pair, leaf, err := loadCert(certPath, keyPath); err == nil && certReusable(leaf, host, time.Now()) {
		return pair, fingerprint(leaf.Raw), nil
	}
	return writeCert(certPath, keyPath, host)
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
	if leaf == nil {
		return false
	}
	if !leaf.NotAfter.After(now.Add(7 * 24 * time.Hour)) {
		return false
	}
	return sanMatches(leaf, host)
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

func writeCert(certPath, keyPath, host string) (tls.Certificate, string, error) {
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
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: host},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(certLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		template.IPAddresses = []net.IP{ip}
		if ip.IsLoopback() {
			template.DNSNames = []string{"localhost"}
		}
	} else {
		template.DNSNames = []string{host}
		if strings.EqualFold(host, "localhost") {
			template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
		}
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
