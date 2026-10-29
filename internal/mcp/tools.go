// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bbeamer41/crossbeam/internal/page"
	"github.com/bbeamer41/crossbeam/internal/settings"
	"github.com/bbeamer41/crossbeam/internal/store"
	"github.com/bbeamer41/crossbeam/internal/version"
)

type toolDesc struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations *toolNotes      `json:"annotations,omitempty"`
}

type toolNotes struct {
	ReadOnlyHint    bool `json:"readOnlyHint,omitempty"`
	DestructiveHint bool `json:"destructiveHint,omitempty"`
}

type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolResult struct {
	Content           []textPart `json:"content"`
	StructuredContent any        `json:"structuredContent,omitempty"`
	IsError           bool       `json:"isError,omitempty"`
}

type articleHead struct {
	Row     int64  `json:"row"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	Created string `json:"created"`
}

type articleFull struct {
	articleHead
	Body string `json:"body"`
}

func toolDescriptions() []toolDesc {
	empty := json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	row := json.RawMessage(`{"type":"object","properties":{"row":{"type":"integer","minimum":1,"description":"Article row."}},"required":["row"],"additionalProperties":false}`)
	save := json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","description":"Article title."},"body":{"type":"string","description":"Article body."},"author":{"type":"string","description":"Author callsign. Empty uses the station callsign."},"id":{"type":"string","description":"Bulletin id. Empty is allowed."},"created":{"type":"string","description":"RFC3339 time. Empty uses the current time."},"row":{"type":"integer","minimum":1,"description":"Existing row to update. Omit to insert."}},"required":["title","body"],"additionalProperties":false}`)
	readOnly := &toolNotes{ReadOnlyHint: true}
	return []toolDesc{
		{Name: "list_articles", Description: "List stored articles newest first. The result has no bodies.", InputSchema: empty, Annotations: readOnly},
		{Name: "read_article", Description: "Read one stored article, including its body.", InputSchema: row, Annotations: readOnly},
		{Name: "save_article", Description: "Save an article. Omit row to insert. Set row to update that article.", InputSchema: save},
		{Name: "delete_article", Description: "Delete one stored article by row.", InputSchema: row, Annotations: &toolNotes{DestructiveHint: true}},
		{Name: "publish_pages", Description: "Write stored articles into the NomadNet pages directory.", InputSchema: empty},
		{Name: "ingest_pages", Description: "Read NomadNet pages into the article store.", InputSchema: empty},
		{Name: "station", Description: "Report the station callsign, the program version, and whether pages, the link, the radio, telnet, and FBB TCP are configured.", InputSchema: empty, Annotations: readOnly},
	}
}

func (s *server) call(ctx context.Context, name string, args json.RawMessage) (toolResult, *rpcError) {
	switch name {
	case "list_articles":
		return s.listArticles(ctx, args)
	case "read_article":
		return s.readArticle(ctx, args)
	case "save_article":
		return s.saveArticle(ctx, args)
	case "delete_article":
		return s.deleteArticle(ctx, args)
	case "publish_pages":
		return s.publishPages(ctx, args)
	case "ingest_pages":
		return s.ingestPages(ctx, args)
	case "station":
		return s.station(ctx, args)
	default:
		return toolResult{}, &rpcError{Code: codeInvalidParams, Message: "unknown tool"}
	}
}

func (s *server) listArticles(ctx context.Context, args json.RawMessage) (toolResult, *rpcError) {
	if err := decodeStrict(args, &struct{}{}); err != nil {
		return toolResult{}, &rpcError{Code: codeInvalidParams, Message: "invalid arguments"}
	}
	rows, err := s.st.ListArticles(ctx)
	if err != nil {
		return toolError(safeMessage(err, "the articles could not be listed")), nil
	}
	heads := make([]articleHead, 0, len(rows))
	for _, row := range rows {
		heads = append(heads, headOf(row))
	}
	return toolValue(map[string]any{"articles": heads})
}

func (s *server) readArticle(ctx context.Context, args json.RawMessage) (toolResult, *rpcError) {
	row, callErr := requiredRow(args)
	if callErr != nil {
		return toolResult{}, callErr
	}
	saved, err := s.st.Article(ctx, row)
	if err != nil {
		return toolError(safeMessage(err, "the article could not be read")), nil
	}
	full := articleFull{articleHead: headOf(saved), Body: saved.Article.Body}
	return toolValue(full)
}

func (s *server) saveArticle(ctx context.Context, args json.RawMessage) (toolResult, *rpcError) {
	var in struct {
		Title   *string      `json:"title"`
		Body    *string      `json:"body"`
		Author  string       `json:"author"`
		ID      string       `json:"id"`
		Created string       `json:"created"`
		Row     *json.Number `json:"row"`
	}
	if err := decodeStrict(args, &in); err != nil || in.Title == nil || in.Body == nil {
		return toolResult{}, &rpcError{Code: codeInvalidParams, Message: "invalid arguments"}
	}
	rowID := int64(0)
	if in.Row != nil {
		parsed, err := parseRow(*in.Row)
		if err != nil {
			return toolResult{}, &rpcError{Code: codeInvalidParams, Message: "invalid arguments"}
		}
		rowID = parsed
	}
	saved, err := s.st.SaveArticle(ctx, store.ArticleIn{
		RowID:   rowID,
		ID:      in.ID,
		Title:   *in.Title,
		Author:  in.Author,
		Created: in.Created,
		Body:    *in.Body,
		Now:     time.Now(),
	})
	if err != nil {
		return toolError(safeMessage(err, "the article could not be saved")), nil
	}
	return toolValue(headOf(saved))
}

func (s *server) deleteArticle(ctx context.Context, args json.RawMessage) (toolResult, *rpcError) {
	row, callErr := requiredRow(args)
	if callErr != nil {
		return toolResult{}, callErr
	}
	if err := s.st.DeleteArticle(ctx, row); err != nil {
		return toolError(safeMessage(err, "the article could not be deleted")), nil
	}
	return toolValue(map[string]any{"deleted": row})
}

func (s *server) publishPages(ctx context.Context, args json.RawMessage) (toolResult, *rpcError) {
	if err := decodeStrict(args, &struct{}{}); err != nil {
		return toolResult{}, &rpcError{Code: codeInvalidParams, Message: "invalid arguments"}
	}
	dir, err := s.pagesDir(ctx)
	if err != nil {
		return toolError(safePage(err, "the pages could not be published")), nil
	}
	written, failed, err := page.Publish(ctx, s.st, dir)
	if err != nil {
		return toolError(safePage(err, "the pages could not be published")), nil
	}
	body := map[string]any{"written": written, "failed": failed}
	if failed > 0 {
		return toolBoth(fmt.Sprintf("%d pages could not be written", failed), body, true)
	}
	return toolValue(body)
}

func (s *server) ingestPages(ctx context.Context, args json.RawMessage) (toolResult, *rpcError) {
	if err := decodeStrict(args, &struct{}{}); err != nil {
		return toolResult{}, &rpcError{Code: codeInvalidParams, Message: "invalid arguments"}
	}
	dir, err := s.pagesDir(ctx)
	if err != nil {
		return toolError(safePage(err, "the pages could not be imported")), nil
	}
	read, failed, err := page.Ingest(ctx, s.st, dir)
	if err != nil {
		return toolError(safePage(err, "the pages could not be imported")), nil
	}
	body := map[string]any{"imported": read, "failed": failed}
	if failed > 0 {
		return toolBoth(fmt.Sprintf("%d pages could not be imported", failed), body, true)
	}
	return toolValue(body)
}

func (s *server) station(ctx context.Context, args json.RawMessage) (toolResult, *rpcError) {
	if err := decodeStrict(args, &struct{}{}); err != nil {
		return toolResult{}, &rpcError{Code: codeInvalidParams, Message: "invalid arguments"}
	}
	current, err := s.st.Settings(ctx)
	if err != nil {
		return toolError(safeMessage(err, "the station could not be read")), nil
	}
	body := map[string]any{
		"version":  version.String,
		"callsign": current[settings.KeyCallsign],
		"pages":    current[settings.KeyPages] != "",
		"link":     current[settings.KeyLinkListen] != "" || current[settings.KeyLinkPeers] != "",
		"radio":    current[settings.KeyKiss] != "",
		"telnet":   current[settings.KeyTelnetListen] != "" || current[settings.KeyTelnetPeers] != "",
		"fbb":      current[settings.KeyFBBListen] != "" || current[settings.KeyFBBPeers] != "",
	}
	return toolValue(body)
}

func (s *server) pagesDir(ctx context.Context) (string, error) {
	current, err := s.st.Settings(ctx)
	if err != nil {
		return "", err
	}
	return current[settings.KeyPages], nil
}

func requiredRow(args json.RawMessage) (int64, *rpcError) {
	var in struct {
		Row *json.Number `json:"row"`
	}
	if err := decodeStrict(args, &in); err != nil || in.Row == nil {
		return 0, &rpcError{Code: codeInvalidParams, Message: "invalid arguments"}
	}
	row, err := parseRow(*in.Row)
	if err != nil {
		return 0, &rpcError{Code: codeInvalidParams, Message: "invalid arguments"}
	}
	return row, nil
}

func parseRow(n json.Number) (int64, error) {
	text := n.String()
	if text == "" || strings.ContainsAny(text, ".eE+-") {
		return 0, errors.New("row must be a positive integer")
	}
	row, err := strconv.ParseInt(text, 10, 64)
	if err != nil || row <= 0 {
		return 0, errors.New("row must be a positive integer")
	}
	return row, nil
}

func headOf(row store.SavedArticle) articleHead {
	return articleHead{
		Row:     row.RowID,
		ID:      row.Article.ID,
		Title:   row.Article.Title,
		Author:  row.Article.Author,
		Created: row.Article.Created.UTC().Format(time.RFC3339),
	}
}

func toolValue(body any) (toolResult, *rpcError) {
	return toolBoth("", body, false)
}

func toolError(text string) toolResult {
	result, _ := toolBoth(text, nil, true)
	return result
}

func toolBoth(text string, body any, isError bool) (toolResult, *rpcError) {
	if text == "" && body != nil {
		raw, err := marshal(body)
		if err != nil {
			return toolResult{}, &rpcError{Code: codeInternal, Message: "internal error"}
		}
		text = string(raw)
	}
	result := toolResult{
		Content: []textPart{{Type: "text", Text: text}},
		IsError: isError,
	}
	if body != nil {
		result.StructuredContent = body
	}
	return result, nil
}

func safePage(err error, fallback string) string {
	if errors.Is(err, page.ErrPagesOff) || errors.Is(err, page.ErrPagesUnavailable) {
		return err.Error()
	}
	return safeMessage(err, fallback)
}

func safeMessage(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	if errors.Is(err, store.ErrNotFound) {
		return "article not found"
	}
	var dup store.ErrDuplicateID
	if errors.As(err, &dup) {
		return "id is already used"
	}
	if errors.Is(err, context.Canceled) {
		return "context canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "context deadline exceeded"
	}
	msg := err.Error()
	if !publicText(msg) {
		return fallback
	}
	return msg
}

func publicText(msg string) bool {
	if msg == "" || len(msg) > 120 {
		return false
	}
	if strings.ContainsAny(msg, "\r\n/\\") {
		return false
	}
	lower := strings.ToLower(msg)
	if strings.HasPrefix(lower, "sql:") {
		return false
	}
	for _, word := range []string{"sqlite", ".db", "password", "token", "secret"} {
		if strings.Contains(lower, word) {
			return false
		}
	}
	for _, r := range msg {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}
