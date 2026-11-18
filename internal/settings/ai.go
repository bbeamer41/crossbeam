// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package settings

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/bbeamer41/crossbeam/internal/ax25"
)

// AI assistant keys. Every one is read when a session asks for the
// assistant, so a saved change applies to the next question.
const (
	KeyAIEnabled      = "ai.enabled"
	KeyAIBackend      = "ai.backend"
	KeyAIURL          = "ai.url"
	KeyAIModel        = "ai.model"
	KeyAIUsers        = "ai.users"
	KeyAIHourly       = "ai.hourly"
	KeyAIReply        = "ai.reply"
	KeyAITimeout      = "ai.timeout"
	KeyAIInstructions = "ai.instructions"
	KeyAIMesh         = "ai.mesh"
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
		Help:    "Let callers chat with an AI model that runs on a computer on your own network. A caller types AI at the station prompt to start, and /EXIT to come back. It is off until you turn it on. You are still the control operator for every reply this station sends, and each question and reply is written to the AI log so you can review it.",
		Kind:    KindBool,
		Default: "false",
	},
	{
		Key:      KeyAIBackend,
		Label:    "AI server type",
		Help:     "The kind of AI server. Choose Ollama for an Ollama computer. Choose OpenAI-compatible for Open WebUI, LM Studio, llama.cpp, or another server that speaks the OpenAI chat format.",
		Kind:     KindLine,
		Default:  AIBackendOllama,
		MaxRunes: 16,
	},
	{
		Key:      KeyAIURL,
		Label:    "AI server address",
		Example:  "http://192.168.1.50:11434",
		Help:     "The web address of the AI server, such as http://192.168.1.50:11434 for Ollama or http://192.168.1.50:3000/api/chat/completions for Open WebUI. Use an address on your own network. Required when the assistant is on.",
		Kind:     KindLine,
		MaxRunes: 256,
	},
	{
		Key:      KeyAIModel,
		Label:    "AI model",
		Example:  "llama3.2:3b",
		Help:     "The name of the model to ask, exactly as the AI server lists it, such as llama3.2:3b. A small model answers faster. Required when the assistant is on.",
		Kind:     KindLine,
		MaxRunes: 128,
	},
	{
		Key:      KeyAIUsers,
		Label:    "Who may use it",
		Example:  "K5LEB W5ABC",
		Help:     "Callsigns allowed to use the assistant, separated by spaces or commas. Leave it empty to allow anyone who connects over the radio, where Crossbeam has already checked their license with the FCC. A listed callsign may also use it over the packet link or NET/ROM. Telnet callers have no callsign, so they cannot use it.",
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
		Key:     KeyAIMesh,
		Label:   "AI over MeshCore",
		Help:    "Also answer a MeshCore direct message that starts with AI, such as AI what is a repeater. Each message is one question with no memory of the last one. Leave this off to keep the assistant on packet only.",
		Kind:    KindBool,
		Default: "false",
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
