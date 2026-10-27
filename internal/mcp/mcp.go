// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package mcp serves the article store and NomadNet pages over the
// Model Context Protocol on standard input and standard output.
// It does not open a network port.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/bbeamer41/crossbeam/internal/store"
	"github.com/bbeamer41/crossbeam/internal/version"
)

const (
	maxMessageBytes = 2 * 1024 * 1024

	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603

	protocolLatest = "2025-06-18"
	instructions   = "Crossbeam article store and NomadNet pages. This process uses the state directory of the user who started it. It does not open a network port."
)

var errSession = errors.New("mcp session ended")

var supportedProtocol = map[string]bool{
	"2024-11-05": true,
	"2025-03-26": true,
	"2025-06-18": true,
}

// Serve reads newline-delimited JSON-RPC from in and writes JSON-RPC to out.
// A message longer than 2 MiB ends the session. Clean end of input returns nil.
func Serve(ctx context.Context, st *store.Store, in io.Reader, out io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if st == nil || in == nil || out == nil {
		return errSession
	}
	lines := make(chan scanLine, 1)
	go readLines(ctx, in, lines)
	s := &server{st: st, w: out}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item, ok := <-lines:
			if !ok {
				return nil
			}
			if item.err != nil {
				if errors.Is(item.err, errTooLarge) {
					_ = s.writeError(nil, codeParse, "parse error")
				}
				return errSession
			}
			if err := s.dispatch(ctx, item.line); err != nil {
				return err
			}
		}
	}
}

type scanLine struct {
	line []byte
	err  error
}

var errTooLarge = errors.New("message is too large")

func readLines(ctx context.Context, in io.Reader, lines chan<- scanLine) {
	defer close(lines)
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)
	for sc.Scan() {
		line := append([]byte(nil), sc.Bytes()...)
		select {
		case <-ctx.Done():
			return
		case lines <- scanLine{line: line}:
		}
	}
	err := sc.Err()
	if errors.Is(err, bufio.ErrTooLong) {
		err = errTooLarge
	}
	if err == nil {
		return
	}
	select {
	case <-ctx.Done():
	case lines <- scanLine{err: err}:
	}
}

type server struct {
	st *store.Store
	w  io.Writer
	on bool
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (s *server) dispatch(ctx context.Context, line []byte) error {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	var probe any
	if err := json.Unmarshal(line, &probe); err != nil {
		return s.writeError(nil, codeParse, "parse error")
	}
	obj, ok := probe.(map[string]any)
	if !ok {
		return s.writeError(nil, codeInvalidRequest, "invalid request")
	}
	var req struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      *json.RawMessage `json:"id"`
		Method  string           `json:"method"`
		Params  json.RawMessage  `json:"params"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		return s.writeError(nil, codeInvalidRequest, "invalid request")
	}
	_, hasResult := obj["result"]
	_, hasError := obj["error"]
	_, hasMethod := obj["method"]
	if !hasMethod && (hasResult || hasError) {
		return nil
	}
	if notification(req.ID) {
		if req.JSONRPC == "2.0" && req.Method == "notifications/initialized" {
			return nil
		}
		return nil
	}
	if !validID(req.ID) || req.JSONRPC != "2.0" || req.Method == "" {
		id := json.RawMessage("null")
		if validID(req.ID) {
			id = *req.ID
		}
		return s.writeError(id, codeInvalidRequest, "invalid request")
	}
	id := *req.ID
	if req.Method != "initialize" && !s.on {
		return s.writeError(id, codeInvalidRequest, "invalid request")
	}
	switch req.Method {
	case "initialize":
		return s.initialize(id, req.Params)
	case "ping":
		return s.writeResult(id, map[string]any{})
	case "tools/list":
		return s.toolsList(id, req.Params)
	case "tools/call":
		return s.toolsCall(ctx, id, req.Params)
	default:
		return s.writeError(id, codeMethodNotFound, "method not found")
	}
}

func (s *server) initialize(id json.RawMessage, params json.RawMessage) error {
	if s.on {
		return s.writeError(id, codeInvalidRequest, "invalid request")
	}
	var in struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := decodeLoose(params, &in); err != nil || in.ProtocolVersion == "" {
		return s.writeError(id, codeInvalidParams, "invalid params")
	}
	chosen := protocolLatest
	if supportedProtocol[in.ProtocolVersion] {
		chosen = in.ProtocolVersion
	}
	s.on = true
	return s.writeResult(id, map[string]any{
		"protocolVersion": chosen,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    "crossbeam",
			"version": version.String,
		},
		"instructions": instructions,
	})
}

func (s *server) toolsList(id json.RawMessage, params json.RawMessage) error {
	var in struct {
		Cursor string `json:"cursor"`
	}
	if err := decodeLoose(params, &in); err != nil || in.Cursor != "" {
		return s.writeError(id, codeInvalidParams, "invalid params")
	}
	return s.writeResult(id, map[string]any{"tools": toolDescriptions()})
}

func (s *server) toolsCall(ctx context.Context, id json.RawMessage, params json.RawMessage) error {
	var in struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := decodeLoose(params, &in); err != nil || in.Name == "" {
		return s.writeError(id, codeInvalidParams, "invalid arguments")
	}
	result, callErr := s.call(ctx, in.Name, in.Arguments)
	if callErr != nil {
		return s.writeError(id, callErr.Code, callErr.Message)
	}
	return s.writeResult(id, result)
}

func (s *server) writeResult(id json.RawMessage, result any) error {
	raw, err := marshal(result)
	if err != nil {
		return s.writeError(id, codeInternal, "internal error")
	}
	return s.write(envelope{JSONRPC: "2.0", ID: id, Result: raw})
}

func (s *server) writeError(id json.RawMessage, code int, message string) error {
	if len(bytes.TrimSpace(id)) == 0 {
		id = json.RawMessage("null")
	}
	return s.write(envelope{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	})
}

func (s *server) write(v any) error {
	raw, err := marshal(v)
	if err != nil {
		return errSession
	}
	raw = append(raw, '\n')
	if _, err := writeFull(s.w, raw); err != nil {
		return errSession
	}
	return nil
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func writeFull(w io.Writer, p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		n, err := w.Write(p)
		total += n
		p = p[n:]
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, io.ErrShortWrite
		}
	}
	return total, nil
}

func notification(id *json.RawMessage) bool {
	if id == nil {
		return true
	}
	return bytes.Equal(bytes.TrimSpace(*id), []byte("null"))
}

func validID(id *json.RawMessage) bool {
	if id == nil {
		return false
	}
	var v any
	if err := json.Unmarshal(*id, &v); err != nil {
		return false
	}
	switch v.(type) {
	case string, float64:
		return true
	default:
		return false
	}
}

func decodeLoose(raw json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

func decodeStrict(raw json.RawMessage, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}
