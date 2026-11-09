// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bbeamer41/crossbeam/internal/admin"
	"github.com/bbeamer41/crossbeam/internal/settings"
	"github.com/bbeamer41/crossbeam/internal/version"
)

const runtimeSocket = "/run/crossbeam/crossbeam.sock"

func (s *server) status(ctx context.Context, args json.RawMessage) (toolResult, *rpcError) {
	if err := decodeStrict(args, &struct{}{}); err != nil {
		return toolResult{}, &rpcError{Code: codeInvalidParams, Message: "invalid arguments"}
	}
	if s.socket != "" {
		body, err := liveStatus(ctx, s.socket)
		if err != nil {
			return toolError(statusMessage(err)), nil
		}
		return toolValue(body)
	}
	for _, path := range socketCandidates(s.st.Dir()) {
		if !socketUp(path) {
			continue
		}
		body, err := liveStatus(ctx, path)
		if err != nil {
			return toolError(statusMessage(err)), nil
		}
		return toolValue(body)
	}
	return s.savedStatus(ctx)
}

func socketCandidates(stateDir string) []string {
	paths := []string{filepath.Join(stateDir, "crossbeam.sock")}
	if stateDir == "/var/lib/crossbeam" {
		paths = append(paths, runtimeSocket)
	}
	return paths
}

func (s *server) savedStatus(ctx context.Context) (toolResult, *rpcError) {
	current, err := s.st.Settings(ctx)
	if err != nil {
		return toolError(safeMessage(err, "the status could not be read")), nil
	}
	listen := current[settings.KeyListen]
	if listen == "" {
		listen = "(chosen at start)"
	}
	sha := "(none)"
	if fp, err := admin.Fingerprint(s.st.Dir()); err == nil {
		sha = fp
	}
	return toolValue(map[string]any{
		"version":     version.String,
		"listen":      listen,
		"public":      current[settings.KeyPublic] == "true",
		"url":         "(running process)",
		"cert_sha256": sha,
		"running":     false,
	})
}

func liveStatus(ctx context.Context, path string) (map[string]any, error) {
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", path)
			},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/api/status", nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New("status was not read")
	}
	var body struct {
		Version string `json:"version"`
		Listen  string `json:"listen"`
		Public  bool   `json:"public"`
		URL     string `json:"url"`
		Cert    string `json:"cert_sha256"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}
	return map[string]any{
		"version":     body.Version,
		"listen":      body.Listen,
		"public":      body.Public,
		"url":         body.URL,
		"cert_sha256": body.Cert,
		"running":     true,
	}, nil
}

func socketUp(path string) bool {
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func statusMessage(err error) string {
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
		return "socket is not running"
	}
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return "permission denied"
	}
	return "the status could not be read"
}
