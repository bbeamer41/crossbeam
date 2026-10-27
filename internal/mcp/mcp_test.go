// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbeamer41/crossbeam/internal/settings"
	"github.com/bbeamer41/crossbeam/internal/store"
	"github.com/bbeamer41/crossbeam/internal/version"
)

func TestInitializeAndTools(t *testing.T) {
	st := openState(t)
	lines := exchange(t, st,
		req(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "clientInfo": map[string]any{"name": "test", "version": "0"}}),
		note("notifications/initialized"),
		req("list", "tools/list", map[string]any{}),
		req(4, "ping", nil),
		req(5, "nope", map[string]any{}),
	)
	if len(lines) != 4 {
		t.Fatalf("responses %d", len(lines))
	}
	init := decode(t, lines[0])
	if init.err != nil || init.result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize %#v", init)
	}
	info, _ := init.result["serverInfo"].(map[string]any)
	if info["name"] != "crossbeam" || info["version"] != version.String {
		t.Fatalf("serverInfo %#v", info)
	}
	caps, _ := init.result["capabilities"].(map[string]any)
	toolsCap, _ := caps["tools"].(map[string]any)
	if toolsCap["listChanged"] != false {
		t.Fatalf("listChanged %#v", toolsCap)
	}
	if !bytes.Contains(lines[0], []byte(`"id":1`)) || bytes.Contains(lines[0], []byte(`"id":"1"`)) {
		t.Fatalf("id encoding %s", lines[0])
	}
	listed := decode(t, lines[1])
	if listed.id != `"list"` {
		t.Fatalf("string id %s", lines[1])
	}
	rawTools, _ := json.Marshal(listed.result["tools"])
	var tools []toolDesc
	if err := json.Unmarshal(rawTools, &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 7 {
		t.Fatalf("tools %d", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
		if tool.Name == "list_articles" && (tool.Annotations == nil || !tool.Annotations.ReadOnlyHint) {
			t.Fatalf("list annotations %#v", tool.Annotations)
		}
		if tool.Name == "delete_article" && (tool.Annotations == nil || !tool.Annotations.DestructiveHint) {
			t.Fatalf("delete annotations %#v", tool.Annotations)
		}
	}
	for _, name := range []string{"list_articles", "read_article", "save_article", "delete_article", "publish_pages", "ingest_pages", "station"} {
		if !names[name] {
			t.Fatalf("missing %s", name)
		}
	}
	ping := decode(t, lines[2])
	if ping.err != nil || len(ping.result) != 0 {
		t.Fatalf("ping %#v", ping)
	}
	missing := decode(t, lines[3])
	if missing.err == nil || missing.err.Code != codeMethodNotFound {
		t.Fatalf("missing %#v", missing)
	}
}

func TestProtocolVersionsAndOrder(t *testing.T) {
	st := openState(t)
	early := exchange(t, st, req(1, "tools/list", nil))
	if len(early) != 1 {
		t.Fatalf("early %d", len(early))
	}
	got := decode(t, early[0])
	if got.err == nil || got.err.Code != codeInvalidRequest || got.result != nil {
		t.Fatalf("before initialize %#v", got)
	}

	old := exchange(t, openState(t), req(1, "initialize", map[string]any{"protocolVersion": "2024-11-05"}))
	if decode(t, old[0]).result["protocolVersion"] != "2024-11-05" {
		t.Fatalf("old %s", old[0])
	}
	other := exchange(t, openState(t),
		req(1, "initialize", map[string]any{"protocolVersion": "1999-01-01"}),
		req(2, "ping", nil),
	)
	if decode(t, other[0]).result["protocolVersion"] != protocolLatest {
		t.Fatalf("fallback %s", other[0])
	}
	if decode(t, other[1]).err != nil {
		t.Fatalf("ping after fallback %s", other[1])
	}
	again := exchange(t, openState(t),
		req(1, "initialize", map[string]any{"protocolVersion": "2025-03-26"}),
		req(2, "initialize", map[string]any{"protocolVersion": "2025-03-26"}),
	)
	if decode(t, again[0]).result["protocolVersion"] != "2025-03-26" {
		t.Fatalf("march %s", again[0])
	}
	second := decode(t, again[1])
	if second.err == nil || second.err.Code != codeInvalidRequest {
		t.Fatalf("second initialize %#v", second)
	}

	bad := exchange(t, openState(t),
		"{",
		"[]",
		`{"jsonrpc":"2.0","id":true,"method":"ping"}`,
		req(9, "initialize", map[string]any{}),
	)
	if len(bad) != 4 {
		t.Fatalf("bad count %d %q", len(bad), bad)
	}
	for i, code := range []int{codeParse, codeInvalidRequest, codeInvalidRequest, codeInvalidParams} {
		env := decode(t, bad[i])
		if env.err == nil || env.err.Code != code {
			t.Fatalf("bad %d %#v %s", i, env, bad[i])
		}
	}
	if !bytes.Contains(bad[2], []byte(`"id":null`)) {
		t.Fatalf("bad id %s", bad[2])
	}
}

func TestArticleTools(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state-secret")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if _, err := st.Apply(context.Background(), map[string]string{settings.KeyCallsign: "K5LEB"}); err != nil {
		t.Fatal(err)
	}
	body := "The net meets.\n\nBring the list. <ready>"
	lines := exchange(t, st,
		req(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}),
		note("notifications/initialized"),
		req(2, "tools/call", map[string]any{"name": "save_article", "arguments": map[string]any{
			"title": "Hello", "body": body, "created": "2026-10-18T15:04:05Z",
		}}),
		req(3, "tools/call", map[string]any{"name": "save_article", "arguments": map[string]any{
			"title": "Later", "author": "W1AW", "id": "NIGHT", "body": "After", "created": "2026-10-19T15:04:05Z",
		}}),
		req(4, "tools/call", map[string]any{"name": "list_articles"}),
		req(5, "tools/call", map[string]any{"name": "read_article", "arguments": map[string]any{"row": 1}}),
		req(6, "tools/call", map[string]any{"name": "save_article", "arguments": map[string]any{
			"title": "Hello again", "body": body, "row": 1, "created": "2026-10-18T15:04:05Z",
		}}),
		req(7, "tools/call", map[string]any{"name": "save_article", "arguments": map[string]any{
			"title": "Dup", "author": "K5LEB", "id": "NIGHT", "body": "x", "created": "2026-10-20T15:04:05Z",
		}}),
		req(8, "tools/call", map[string]any{"name": "delete_article", "arguments": map[string]any{"row": 2}}),
		req(9, "tools/call", map[string]any{"name": "delete_article", "arguments": map[string]any{"row": 2}}),
		req(10, "tools/call", map[string]any{"name": "save_article", "arguments": map[string]any{
			"title": "", "body": "x", "author": "K5LEB",
		}}),
		req(11, "tools/call", map[string]any{"name": "tools"}),
		req(12, "tools/call", map[string]any{"name": "delete_article", "arguments": map[string]any{"row": 0}}),
		req(13, "tools/call", map[string]any{"name": "read_article", "arguments": map[string]any{"row": 1.5}}),
		req(14, "tools/call", map[string]any{"name": "list_articles", "arguments": map[string]any{"body": "nope"}}),
	)
	if len(lines) != 14 {
		t.Fatalf("responses %d", len(lines))
	}
	saved := toolData(t, lines[1])
	if saved.isError || saved.data["row"] != float64(1) || saved.data["author"] != "K5LEB" {
		t.Fatalf("save %#v", saved.data)
	}
	if _, ok := saved.data["body"]; ok {
		t.Fatal("save echoed the body")
	}
	list := toolData(t, lines[3])
	rawList := string(lines[3])
	if strings.Contains(rawList, "Bring the list") || strings.Contains(rawList, dir) {
		t.Fatalf("list leaked %s", rawList)
	}
	articles, _ := list.data["articles"].([]any)
	if len(articles) != 2 {
		t.Fatalf("list %#v", list.data)
	}
	first, _ := articles[0].(map[string]any)
	if first["title"] != "Later" || first["row"] != float64(2) {
		t.Fatalf("order %#v", first)
	}
	read := toolData(t, lines[4])
	if read.isError || read.data["body"] != body || read.data["title"] != "Hello" {
		t.Fatalf("read %#v", read.data)
	}
	if bytes.Count(lines[4], []byte("\n")) != 0 {
		t.Fatal("read response was not one line")
	}
	updated := toolData(t, lines[5])
	if updated.data["title"] != "Hello again" || updated.data["row"] != float64(1) {
		t.Fatalf("update %#v", updated.data)
	}
	dup := toolData(t, lines[6])
	if !dup.isError || dup.text != "id is already used" {
		t.Fatalf("dup %#v", dup)
	}
	deleted := toolData(t, lines[7])
	if deleted.isError || deleted.data["deleted"] != float64(2) {
		t.Fatalf("delete %#v", deleted.data)
	}
	missing := toolData(t, lines[8])
	if !missing.isError || missing.text != "article not found" || missing.proto != nil {
		t.Fatalf("missing %#v %s", missing, lines[8])
	}
	emptyTitle := toolData(t, lines[9])
	if !emptyTitle.isError || emptyTitle.text != "title is empty" {
		t.Fatalf("empty title %#v", emptyTitle)
	}
	unknown := decode(t, lines[10])
	if unknown.err == nil || unknown.err.Message != "unknown tool" {
		t.Fatalf("unknown %#v", unknown)
	}
	for i, line := range lines[11:] {
		env := decode(t, line)
		if env.err == nil || env.err.Message != "invalid arguments" {
			t.Fatalf("bad args %d %#v %s", i, env, line)
		}
	}
	if strings.Contains(string(lines[8]), dir) || strings.Contains(string(lines[6]), "sqlite") {
		t.Fatal("error leaked the store")
	}
}

func TestPagesAndStation(t *testing.T) {
	dir := t.TempDir()
	pages := filepath.Join(dir, "pages")
	if err := os.Mkdir(pages, 0o755); err != nil {
		t.Fatal(err)
	}
	st := openState(t)
	secretPeer := "W1AW-1@192.168.1.21:10093"
	if _, err := st.Apply(context.Background(), map[string]string{
		settings.KeyCallsign:  "K5LEB",
		settings.KeyPages:     pages,
		settings.KeyLinkPeers: secretPeer,
	}); err != nil {
		t.Fatal(err)
	}
	off := exchange(t, openState(t),
		req(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}),
		req(2, "tools/call", map[string]any{"name": "publish_pages"}),
	)
	publishOff := toolData(t, off[1])
	if !publishOff.isError || publishOff.text != "nomad.pages is empty" || strings.Contains(string(off[1]), pages) {
		t.Fatalf("pages off %#v %s", publishOff, off[1])
	}

	lines := exchange(t, st,
		req(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}),
		req(2, "tools/call", map[string]any{"name": "station"}),
		req(3, "tools/call", map[string]any{"name": "save_article", "arguments": map[string]any{
			"title": "Hello", "author": "K5LEB", "body": "The net meets.", "created": "2026-10-18T15:04:05Z",
		}}),
		req(4, "tools/call", map[string]any{"name": "publish_pages"}),
		req(5, "tools/call", map[string]any{"name": "delete_article", "arguments": map[string]any{"row": 1}}),
		req(6, "tools/call", map[string]any{"name": "ingest_pages"}),
		req(7, "tools/call", map[string]any{"name": "list_articles"}),
	)
	station := toolData(t, lines[1])
	rawStation := string(lines[1])
	if station.isError || station.data["callsign"] != "K5LEB" || station.data["version"] != version.String {
		t.Fatalf("station %#v", station.data)
	}
	if station.data["pages"] != true || station.data["link"] != true || station.data["radio"] != false || station.data["telnet"] != false || station.data["fbb"] != false {
		t.Fatalf("flags %#v", station.data)
	}
	if strings.Contains(rawStation, pages) || strings.Contains(rawStation, "192.168.1.21") || strings.Contains(rawStation, "W1AW") {
		t.Fatalf("station leaked %s", rawStation)
	}
	wrote := toolData(t, lines[3])
	if wrote.isError || wrote.data["written"] != float64(1) || wrote.data["failed"] != float64(0) {
		t.Fatalf("publish %#v", wrote.data)
	}
	info, err := os.Stat(filepath.Join(pages, "cb-1.mu"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	imported := toolData(t, lines[5])
	if imported.isError || imported.data["imported"] != float64(1) {
		t.Fatalf("ingest %#v", imported.data)
	}
	restored := toolData(t, lines[6])
	articles, _ := restored.data["articles"].([]any)
	if len(articles) != 1 {
		t.Fatalf("restored %#v", restored.data)
	}

	st.Close()
	closed := exchange(t, st,
		req(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}),
		req(2, "tools/call", map[string]any{"name": "list_articles"}),
	)
	failed := toolData(t, closed[1])
	if !failed.isError || strings.Contains(failed.text, dir) || strings.Contains(string(closed[1]), dir) {
		t.Fatalf("closed %#v %s", failed, closed[1])
	}
}

func TestMessageTooLarge(t *testing.T) {
	st := openState(t)
	var in bytes.Buffer
	in.WriteString(req(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"}))
	in.WriteByte('\n')
	in.Write(bytes.Repeat([]byte("A"), maxMessageBytes+1))
	in.WriteByte('\n')
	var out bytes.Buffer
	err := Serve(context.Background(), st, &in, &out)
	if !errors.Is(err, errSession) {
		t.Fatalf("err %v", err)
	}
	text := out.String()
	if strings.Contains(text, "AAAA") || !strings.Contains(text, "parse error") {
		t.Fatalf("output %q", text)
	}
}

func openState(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func req(id any, method string, params any) string {
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func note(method string) string {
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func exchange(t *testing.T, st *store.Store, lines ...string) []json.RawMessage {
	t.Helper()
	var in bytes.Buffer
	for _, line := range lines {
		in.WriteString(line)
		in.WriteByte('\n')
	}
	var out bytes.Buffer
	if err := Serve(context.Background(), st, &in, &out); err != nil {
		t.Fatalf("serve: %v\n%s", err, out.String())
	}
	sc := bufio.NewScanner(&out)
	sc.Buffer(make([]byte, 0, 64*1024), maxMessageBytes)
	var msgs []json.RawMessage
	for sc.Scan() {
		if !json.Valid(sc.Bytes()) {
			t.Fatalf("not json %q", sc.Bytes())
		}
		msgs = append(msgs, append([]byte(nil), sc.Bytes()...))
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return msgs
}

type decoded struct {
	id     string
	result map[string]any
	err    *rpcError
}

func decode(t *testing.T, line json.RawMessage) decoded {
	t.Helper()
	var env struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  map[string]any  `json:"result"`
		Error   *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(line, &env); err != nil {
		t.Fatal(err)
	}
	if env.JSONRPC != "2.0" {
		t.Fatalf("jsonrpc %s", line)
	}
	if env.Error != nil && env.Result != nil {
		t.Fatalf("both result and error %s", line)
	}
	return decoded{id: string(env.ID), result: env.Result, err: env.Error}
}

type toolDecoded struct {
	isError bool
	text    string
	data    map[string]any
	proto   *rpcError
}

func toolData(t *testing.T, line json.RawMessage) toolDecoded {
	t.Helper()
	env := decode(t, line)
	if env.err != nil {
		return toolDecoded{proto: env.err}
	}
	content, _ := env.result["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content %#v", env.result)
	}
	part, _ := content[0].(map[string]any)
	if part["type"] != "text" {
		t.Fatalf("part %#v", part)
	}
	text, _ := part["text"].(string)
	out := toolDecoded{text: text, proto: nil}
	if env.result["isError"] == true {
		out.isError = true
	}
	if raw, ok := env.result["structuredContent"]; ok && raw != nil {
		body, _ := raw.(map[string]any)
		out.data = body
	}
	return out
}
