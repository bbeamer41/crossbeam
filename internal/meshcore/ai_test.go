// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package meshcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ai"
	"github.com/bbeamer41/crossbeam/internal/settings"
)

func meshAssistant(t *testing.T, mesh string) *ai.Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Messages []ai.Message `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		q := in.Messages[len(in.Messages)-1].Content
		_ = json.NewEncoder(w).Encode(map[string]any{"message": ai.Message{Role: "assistant", Content: fmt.Sprintf("Mesh AI %d: %s", len(in.Messages), q)}})
	}))
	t.Cleanup(srv.Close)
	values, err := settings.Validate(map[string]string{
		settings.KeyCallsign:  "K5LEB",
		settings.KeyAIEnabled: "true",
		settings.KeyAIURL:     srv.URL,
		settings.KeyAIMesh:    mesh,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ai.New(func(context.Context) (map[string]string, error) { return values, nil }, t.TempDir())
}

func runMeshAI(t *testing.T, svc *ai.Service, script func(io.ReadWriter) error) {
	t.Helper()
	host, radio := net.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	scriptErr := make(chan error, 1)
	go func() {
		defer radio.Close()
		scriptErr <- script(radio)
	}()
	err := sessionAI(ctx, host, nil, "K5LEB", nil, svc)
	_ = host.Close()
	if serr := <-scriptErr; serr != nil {
		t.Fatal(serr)
	}
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}

func meshExchange(rw io.ReadWriter, prefix []byte, text string, seq uint32) (string, error) {
	if err := writeFrame(rw, markFromRadio, contactFrame(prefix, text, 0, seq)); err != nil {
		return "", err
	}
	sent, err := expectCmd(rw, cmdSendText)
	if err != nil {
		return "", err
	}
	if err := writeFrame(rw, markFromRadio, []byte{respMsgSent, 0}); err != nil {
		return "", err
	}
	return string(sent[13:]), nil
}

func TestMeshAIKeepsShortHistory(t *testing.T) {
	svc := meshAssistant(t, "true")
	if err := svc.Approve("meshcore:010203040506"); err != nil {
		t.Fatal(err)
	}
	runMeshAI(t, svc, func(rw io.ReadWriter) error {
		if err := setupRadio(rw, "K5LEB"); err != nil {
			return err
		}
		if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
			return err
		}
		if err := writeFrame(rw, markFromRadio, []byte{respNoMore}); err != nil {
			return err
		}
		prefix := []byte{1, 2, 3, 4, 5, 6}
		got, err := meshExchange(rw, prefix, "hello", 50)
		if err != nil {
			return err
		}
		if !strings.HasSuffix(got, "Or AI and a question for the AI.") || len(got) > maxTextBytes {
			return fmt.Errorf("greeting does not mention AI: %q", got)
		}
		if got, err = meshExchange(rw, prefix, "AI what is a repeater", 51); err != nil {
			return err
		}
		if got != "Mesh AI 2: what is a repeater" {
			return fmt.Errorf("first answer %q", got)
		}
		if got, err = meshExchange(rw, prefix, "AI and on 2m?", 52); err != nil {
			return err
		}
		if got != "Mesh AI 4: and on 2m?" {
			return fmt.Errorf("follow-up did not carry history: %q", got)
		}
		if got, err = meshExchange(rw, prefix, "LIST", 53); err != nil {
			return err
		}
		if !strings.Contains(got, "not available") {
			return fmt.Errorf("LIST after AI %q", got)
		}
		return nil
	})
}

func TestMeshAIOffKeepsGreeting(t *testing.T) {
	runMeshAI(t, meshAssistant(t, "false"), func(rw io.ReadWriter) error {
		if err := setupRadio(rw, "K5LEB"); err != nil {
			return err
		}
		if _, err := expectCmd(rw, cmdSyncMessage); err != nil {
			return err
		}
		if err := writeFrame(rw, markFromRadio, []byte{respNoMore}); err != nil {
			return err
		}
		got, err := meshExchange(rw, []byte{9, 9, 9, 9, 9, 9}, "AI hello", 60)
		if err != nil {
			return err
		}
		if strings.Contains(got, "AI") || !strings.Contains(got, "LIST") {
			return fmt.Errorf("with ai.mesh off: %q", got)
		}
		return nil
	})
}
