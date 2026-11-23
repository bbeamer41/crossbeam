// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package settings

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/bbeamer41/crossbeam/internal/ax25"
)

// AI assistant keys. Every one is read when a session asks for the
// assistant, so a saved change applies to the next question.
const (
	KeyAIEnabled         = "ai.enabled"
	KeyAIBackend         = "ai.backend"
	KeyAIURL             = "ai.url"
	KeyAIModel           = "ai.model"
	KeyAIUsers           = "ai.users"
	KeyAIHourly          = "ai.hourly"
	KeyAIReply           = "ai.reply"
	KeyAITimeout         = "ai.timeout"
	KeyAIInstructions    = "ai.instructions"
	KeyAIMesh            = "ai.mesh"
	KeyAIReticulum       = "ai.reticulum"
	KeyAIRequireApproval = "ai.require_approval"
	KeyAIBannedWords     = "ai.banned_words"
)

// Defaults for a model on this computer.
const (
	AIDefaultURL   = "http://127.0.0.1:11434"
	AIDefaultModel = "llama3.2:3b"
)

// AI backend names stored in ai.backend.
const (
	AIBackendOllama = "ollama"
	AIBackendOpenAI = "openai"
)

// aiFields is appended to Schema in this order.
var aiFields = []Field{
	{
		Key:     KeyAIEnabled,
		Label:   "AI assistant",
		Help:    "Let callers chat with an AI model that runs on a computer on your own network. A caller types AI at the station prompt to start, /EXIT to come back to the prompt, and BYE to disconnect. It is off until you turn it on. You are still the control operator for every reply this station sends, and each question and reply is written to the AI log so you can review it.",
		Kind:    KindBool,
		Default: "false",
	},
	{
		Key:      KeyAIBackend,
		Label:    "AI server type",
		Help:     "The kind of AI server. Ollama is the usual choice: Crossbeam talks to Ollama directly, with nothing else in between. Choose OpenAI-compatible only for Open WebUI, LM Studio, llama.cpp, or another server that speaks the OpenAI chat format.",
		Kind:     KindLine,
		Default:  AIBackendOllama,
		MaxRunes: 16,
	},
	{
		Key:      KeyAIURL,
		Label:    "AI server address",
		Example:  "http://192.168.1.50:11434",
		Help:     "The web address of the AI server. The default, http://127.0.0.1:11434, is Ollama on this computer. For Ollama on another computer, use its address, such as http://192.168.1.50:11434. For Open WebUI, use an address such as http://192.168.1.50:3000/api/chat/completions. Use an address on your own network.",
		Kind:     KindLine,
		Default:  AIDefaultURL,
		MaxRunes: 256,
	},
	{
		Key:      KeyAIModel,
		Label:    "AI model",
		Example:  "llama3.2:3b",
		Help:     "The name of the model to ask, exactly as the AI server lists it. The default is llama3.2:3b. On the Ollama computer, download it once with: ollama pull llama3.2:3b. A smaller model answers faster.",
		Kind:     KindLine,
		Default:  AIDefaultModel,
		MaxRunes: 128,
	},
	{
		Key:      KeyAIUsers,
		Label:    "Who may use it",
		Example:  "N0CALL W1AW",
		Help:     "Optional. Leave it empty, the usual choice, and anyone who connects over the radio may use the assistant, because Crossbeam has already checked their license with the FCC. Callsigns listed here, separated by spaces or commas, may also use it over the packet link or NET/ROM. Telnet callers have no callsign, so they cannot use it.",
		Kind:     KindLine,
		MaxRunes: 1024,
	},
	{
		Key:      KeyAIHourly,
		Label:    "Questions per hour",
		Example:  "20",
		Help:     "How many questions one callsign may ask in an hour. This keeps the channel and the AI computer from being tied up by one station. Use a number from 1 to 600. An empty value is stored as 20.",
		Kind:     KindLine,
		Default:  "20",
		MaxRunes: 4,
	},
	{
		Key:      KeyAIReply,
		Label:    "Longest reply",
		Example:  "600",
		Help:     "The most characters one reply may use. At 1200 baud, 600 characters take about five seconds of air time. A longer reply is cut short. Use a number from 80 to 4000. An empty value is stored as 600.",
		Kind:     KindLine,
		Default:  "600",
		MaxRunes: 4,
	},
	{
		Key:      KeyAITimeout,
		Label:    "Answer time limit",
		Example:  "90",
		Help:     "How many seconds to wait for the AI server before giving up on a question. Use a number from 5 to 600. An empty value is stored as 90.",
		Kind:     KindLine,
		Default:  "90",
		MaxRunes: 3,
	},
	{
		Key:      KeyAIInstructions,
		Label:    "Extra instructions",
		Example:  "You help members of the Lake Area Amateur Radio Klub.",
		Help:     "Optional instructions added to what the assistant is always told. Crossbeam already tells it to answer briefly in plain English, with no profanity, no codes, and no business. Use this for anything about your station or club.",
		Kind:     KindLine,
		MaxRunes: 500,
	},
	{
		Key:      KeyAIBannedWords,
		Label:    "Extra banned words",
		Example:  "scam crypto",
		Help:     "Optional words the assistant will not send or accept, added to the built-in list. Separate words with spaces or commas. A word matches on its own, so listing one does not block a longer word that merely contains it.",
		Kind:     KindLine,
		MaxRunes: 1024,
	},
	{
		Key:     KeyAIMesh,
		Label:   "AI over MeshCore",
		Help:    "Also answer a MeshCore direct message that starts with AI, such as AI what is a repeater. The assistant remembers the last few questions from that contact for 30 minutes. Send AI /NEW to start over. Leave this off to keep the assistant off MeshCore.",
		Kind:    KindBool,
		Default: "false",
	},
	{
		Key:     KeyAIReticulum,
		Label:   "AI over Reticulum",
		Help:    "Also answer an LXMF message sent to this station over Reticulum, such as from Sideband or NomadNet, when it starts with AI. It needs the separate Crossbeam LXMF bridge running on this computer. The assistant remembers the last few questions from that sender for 30 minutes. Reticulum messages are encrypted, so do not carry them on an amateur radio interface. Leave this off to keep the assistant off Reticulum.",
		Kind:    KindBool,
		Default: "false",
	},
	{
		Key:     KeyAIRequireApproval,
		Label:   "Approve MeshCore and Reticulum senders",
		Help:    "When this is on, a MeshCore or Reticulum sender must be approved before a question reaches the model. The first message is held for you, and the sender is told once that it is waiting. A blocked sender gets no reply. Radio callers are not on this list: the license check already covers them. Leave this on.",
		Kind:    KindBool,
		Default: "true",
	},
}

func validateAI(out map[string]string) error {
	backend := strings.ToLower(strings.TrimSpace(out[KeyAIBackend]))
	if backend == "" {
		backend = AIBackendOllama
	}
	if backend != AIBackendOllama && backend != AIBackendOpenAI {
		return fmt.Errorf("ai.backend must be ollama or openai")
	}
	out[KeyAIBackend] = backend
	addr, err := canonicalAIURL(out[KeyAIURL])
	if err != nil {
		return err
	}
	out[KeyAIURL] = addr
	if strings.ContainsAny(out[KeyAIModel], " \t") {
		return fmt.Errorf("ai.model must not contain spaces")
	}
	users, err := ParseAIUsers(out[KeyAIUsers])
	if err != nil {
		return err
	}
	out[KeyAIUsers] = strings.Join(users, " ")
	words, err := ParseAIBanned(out[KeyAIBannedWords])
	if err != nil {
		return err
	}
	out[KeyAIBannedWords] = strings.Join(words, " ")
	for _, n := range []struct {
		key      string
		def      string
		min, max int
	}{
		{KeyAIHourly, "20", 1, 600},
		{KeyAIReply, "600", 80, 4000},
		{KeyAITimeout, "90", 5, 600},
	} {
		value := strings.TrimSpace(out[n.key])
		if value == "" {
			value = n.def
		}
		v, err := strconv.Atoi(value)
		if err != nil || v < n.min || v > n.max {
			return fmt.Errorf("%s must be a number from %d to %d", n.key, n.min, n.max)
		}
		out[n.key] = strconv.Itoa(v)
	}
	if out[KeyAIEnabled] == "true" {
		if out[KeyAIURL] == "" {
			return fmt.Errorf("ai.url is required when ai.enabled is true")
		}
		if out[KeyAIModel] == "" {
			return fmt.Errorf("ai.model is required when ai.enabled is true")
		}
	}
	return nil
}

func canonicalAIURL(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" {
		return "", fmt.Errorf("ai.url must be an http or https address such as http://192.168.1.50:11434")
	}
	if u.User != nil {
		return "", fmt.Errorf("ai.url must not hold a user name or password; use crossbeam ai key")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("ai.url must not have a query or a fragment")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// ParseAIBanned reads ai.banned_words. Words are letters, stored in
// lower case, and duplicates are dropped.
func ParseAIBanned(value string) ([]string, error) {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t' || r == ';'
	})
	seen := map[string]bool{}
	var out []string
	for _, field := range fields {
		word := strings.ToLower(strings.TrimSpace(field))
		if word == "" {
			continue
		}
		if utf8Len(word) > 32 || !wordLetters(word) {
			return nil, fmt.Errorf("ai.banned_words must be words of letters, separated by spaces or commas")
		}
		if !seen[word] {
			seen[word] = true
			out = append(out, word)
		}
	}
	if len(out) > 64 {
		return nil, fmt.Errorf("ai.banned_words lists more than 64 words")
	}
	return out, nil
}

func wordLetters(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func utf8Len(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// ParseAIUsers reads ai.users. Each entry is an AX.25 callsign. The SSID
// is dropped because the license belongs to the base callsign.
func ParseAIUsers(value string) ([]string, error) {
	fields := strings.FieldsFunc(value, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' })
	seen := map[string]bool{}
	var out []string
	for _, field := range fields {
		addr, err := ax25.Parse(field)
		if err != nil {
			return nil, fmt.Errorf("ai.users must be callsigns separated by spaces or commas")
		}
		if !seen[addr.Call] {
			seen[addr.Call] = true
			out = append(out, addr.Call)
		}
	}
	if len(out) > 256 {
		return nil, fmt.Errorf("ai.users lists more than 256 callsigns")
	}
	return out, nil
}
