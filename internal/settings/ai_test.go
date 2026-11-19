// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package settings

import (
	"strings"
	"testing"
)

func TestAIDefaultsOff(t *testing.T) {
	got, err := Validate(map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got[KeyAIEnabled] != "false" || got[KeyAIBackend] != "ollama" || got[KeyAIHourly] != "20" || got[KeyAIReply] != "600" || got[KeyAITimeout] != "90" || got[KeyAIMesh] != "false" || got[KeyAIReticulum] != "false" || got[KeyAIRequireApproval] != "true" || got[KeyAIBannedWords] != "" || got[KeyAIURL] != AIDefaultURL || got[KeyAIModel] != AIDefaultModel || got[KeyAIUsers] != "" {
		t.Fatalf("defaults %v", got)
	}
}

func TestAISettings(t *testing.T) {
	got, err := Validate(map[string]string{
		KeyAIEnabled: "true",
		KeyAIBackend: " OpenAI ",
		KeyAIURL:     "http://192.168.1.50:3000/api/chat/completions/",
		KeyAIModel:   "llama3.2:3b",
		KeyAIUsers:   "k5leb-1, w5abc w5abc-2",
		KeyAIHourly:  " 30 ",
		KeyAIReply:   "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[KeyAIBackend] != "openai" || got[KeyAIURL] != "http://192.168.1.50:3000/api/chat/completions" || got[KeyAIUsers] != "K5LEB W5ABC" || got[KeyAIHourly] != "30" || got[KeyAIReply] != "600" {
		t.Fatalf("canonical %v", got)
	}
	bad := []struct {
		patch map[string]string
		want  string
	}{
		{map[string]string{KeyAIEnabled: "true", KeyAIURL: ""}, "ai.url is required"},
		{map[string]string{KeyAIEnabled: "true", KeyAIModel: " "}, "ai.model is required"},
		{map[string]string{KeyAIBackend: "gemini"}, "ai.backend"},
		{map[string]string{KeyAIURL: "ftp://h"}, "ai.url"},
		{map[string]string{KeyAIURL: "http://user:pw@h:1"}, "crossbeam ai key"},
		{map[string]string{KeyAIURL: "http://h:1/?key=x"}, "query"},
		{map[string]string{KeyAIUsers: "not a call!"}, "ai.users"},
		{map[string]string{KeyAIHourly: "0"}, "ai.hourly"},
		{map[string]string{KeyAIReply: "50000"}, "ai.reply"},
		{map[string]string{KeyAITimeout: "two"}, "ai.timeout"},
		{map[string]string{KeyAIModel: "two words"}, "ai.model"},
		{map[string]string{KeyAIBannedWords: "scam2"}, "ai.banned_words"},
		{map[string]string{KeyAIBannedWords: strings.Repeat("a", 33)}, "ai.banned_words"},
	}
	for _, c := range bad {
		if _, err := Validate(c.patch); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v", c.patch, err)
		}
	}
}

// Turning the assistant on needs nothing else: Ollama on this computer,
// llama3.2:3b, and radio callers with no list.
func TestAIEnableAlone(t *testing.T) {
	got, err := Validate(map[string]string{KeyAIEnabled: "true"})
	if err != nil {
		t.Fatal(err)
	}
	if got[KeyAIURL] != "http://127.0.0.1:11434" || got[KeyAIModel] != "llama3.2:3b" || got[KeyAIBackend] != "ollama" || got[KeyAIUsers] != "" || got[KeyAIRequireApproval] != "true" {
		t.Fatalf("%v", got)
	}
}

func TestAIBannedWords(t *testing.T) {
	got, err := Validate(map[string]string{KeyAIBannedWords: "Scam, crypto; Crypto scam"})
	if err != nil {
		t.Fatal(err)
	}
	if got[KeyAIBannedWords] != "scam crypto" {
		t.Fatalf("%q", got[KeyAIBannedWords])
	}
	var many []string
	for i := 0; i < 65; i++ {
		many = append(many, string(rune('a'+i/26))+string(rune('a'+i%26))+"x")
	}
	if _, err := Validate(map[string]string{KeyAIBannedWords: strings.Join(many, " ")}); err == nil || !strings.Contains(err.Error(), "64") {
		t.Fatal(err)
	}
	off, err := Validate(map[string]string{KeyAIRequireApproval: "false"})
	if err != nil || off[KeyAIRequireApproval] != "false" {
		t.Fatalf("%v %v", off[KeyAIRequireApproval], err)
	}
}
