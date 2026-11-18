// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/bbeamer41/crossbeam/internal/settings"
)

// Message is one chat turn. Role is system, user, or assistant.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Errors a caller sees as a short sentence. The raw error, which can
// hold the server address, stays out of the reply and the key is never
// part of any error.
var (
	ErrTimeout     = errors.New("the AI server did not answer in time")
	ErrUnreachable = errors.New("the AI server could not be reached")
	ErrServer      = errors.New("the AI server returned an error")
	ErrEmpty       = errors.New("the AI server sent an empty answer")
	ErrModel       = errors.New("the AI server does not have that model")
	ErrAuth        = errors.New("the AI server refused the API key")
)

const maxResponse = 1 << 20

// NewHTTPClient never uses a proxy from the environment, so a question
// goes only to the address the sysop saved.
func NewHTTPClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 0,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
	}}
}

// complete asks the configured server once and returns the raw answer.
func complete(ctx context.Context, client *http.Client, cfg Config, msgs []Message) (string, error) {
	if client == nil {
		client = NewHTTPClient()
	}
	tokens := cfg.Reply/3 + 64
	var endpoint string
	var body any
	switch cfg.Backend {
	case settings.AIBackendOpenAI:
		endpoint = openAIEndpoint(cfg.URL)
		body = map[string]any{
			"model":      cfg.Model,
			"messages":   msgs,
			"stream":     false,
			"max_tokens": tokens,
		}
	default:
		endpoint = ollamaEndpoint(cfg.URL)
		body = map[string]any{
			"model":    cfg.Model,
			"messages": msgs,
			"stream":   false,
			"options":  map[string]any{"num_predict": tokens},
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", ErrUnreachable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Crossbeam AI assistant")
	if cfg.Key != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Key)
	}
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", ErrTimeout
		}
		if errors.Is(err, context.Canceled) {
			return "", context.Canceled
		}
		return "", ErrUnreachable
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponse))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", ErrTimeout
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return "", context.Canceled
		}
		return "", ErrUnreachable
	}
	if res.StatusCode != http.StatusOK {
		lower := strings.ToLower(string(data))
		switch {
		case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
			return "", ErrAuth
		case strings.Contains(lower, "model") && (strings.Contains(lower, "not found") || strings.Contains(lower, "pull") || strings.Contains(lower, "does not exist")):
			// Ollama: 404 {"error":"model \"x\" not found, try pulling it first"}.
			return "", ErrModel
		}
		return "", fmt.Errorf("%w (HTTP %d)", ErrServer, res.StatusCode)
	}
	var text string
	if cfg.Backend == settings.AIBackendOpenAI {
		var out struct {
			Choices []struct {
				Message Message `json:"message"`
			} `json:"choices"`
		}
		if json.Unmarshal(data, &out) != nil {
			return "", ErrServer
		}
		if len(out.Choices) > 0 {
			text = out.Choices[0].Message.Content
		}
	} else {
		var out struct {
			Message Message `json:"message"`
		}
		if json.Unmarshal(data, &out) != nil {
			return "", ErrServer
		}
		text = out.Message.Content
	}
	text = strings.TrimSpace(stripThinking(text))
	if text == "" {
		return "", ErrEmpty
	}
	return text, nil
}

// ollamaEndpoint accepts http://host:11434, or a full /api/chat address.
func ollamaEndpoint(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	if strings.Trim(u.Path, "/") == "" {
		u.Path = "/api/chat"
	}
	return u.String()
}

// openAIEndpoint accepts a full chat completions address, such as Open
// WebUI's /api/chat/completions, a base ending in /v1, or a bare host.
func openAIEndpoint(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	path := strings.TrimRight(u.Path, "/")
	switch {
	case path == "":
		u.Path = "/v1/chat/completions"
	case strings.HasSuffix(path, "/v1") || strings.HasSuffix(path, "/api"):
		u.Path = path + "/chat/completions"
	}
	return u.String()
}

var thinkBlock = regexp.MustCompile(`(?s)<think>.*?</think>`)

// stripThinking drops the reasoning block some local models write first.
func stripThinking(text string) string {
	text = thinkBlock.ReplaceAllString(text, "")
	if i := strings.Index(text, "<think>"); i >= 0 {
		text = text[:i]
	}
	return text
}

// publicError is the sentence a caller sees for err.
func publicError(err error) string {
	switch {
	case errors.Is(err, ErrTimeout):
		return "The AI server did not answer in time."
	case errors.Is(err, ErrEmpty):
		return "The AI server sent an empty answer."
	case errors.Is(err, ErrModel):
		return "The AI model is not available right now."
	case errors.Is(err, ErrAuth):
		return "The AI server refused this station."
	case errors.Is(err, ErrServer):
		return "The AI server returned an error."
	default:
		return "The AI server could not be reached."
	}
}

// sysopError explains err to the sysop running crossbeam ai test, with
// the address and the next step. It is never sent on the air.
func sysopError(cfg Config, err error) error {
	name := "the AI server"
	if cfg.Backend != settings.AIBackendOpenAI {
		name = "Ollama"
	}
	switch {
	case errors.Is(err, ErrUnreachable):
		hint := "Check that Ollama is running there (ollama serve, or systemctl status ollama)."
		if cfg.Backend == settings.AIBackendOpenAI {
			hint = "Check that the server is running and the address is right."
		}
		return fmt.Errorf("%s at %s could not be reached. %s If it is on another computer, it must listen on the network (OLLAMA_HOST=0.0.0.0:11434 for Ollama) and its firewall must let this computer in", name, cfg.URL, hint)
	case errors.Is(err, ErrModel):
		if cfg.Backend == settings.AIBackendOpenAI {
			return fmt.Errorf("the AI server at %s does not have the model %s. Check the model name the server lists", cfg.URL, cfg.Model)
		}
		return fmt.Errorf("Ollama at %s does not have the model %s. On that computer run: ollama pull %s", cfg.URL, cfg.Model, cfg.Model)
	case errors.Is(err, ErrTimeout):
		return fmt.Errorf("%s at %s did not answer within %s. The first answer can be slow while the model loads; try again, or raise ai.timeout", name, cfg.URL, cfg.Timeout)
	case errors.Is(err, ErrAuth):
		return fmt.Errorf("%s at %s refused the API key. Save the right key with: crossbeam ai key", name, cfg.URL)
	case errors.Is(err, ErrEmpty):
		return fmt.Errorf("%s at %s sent an empty answer", name, cfg.URL)
	default:
		return fmt.Errorf("%s at %s returned an error: %v", name, cfg.URL, err)
	}
}
