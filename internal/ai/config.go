// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package ai is the station AI assistant. A connected packet caller
// types AI at the station prompt and chats with a model on a computer on
// the sysop's own network, through Ollama or an OpenAI-compatible server
// such as Open WebUI. Every question and reply is written to a log for
// the control operator. The assistant is off by default.
package ai

import (
	"strconv"
	"strings"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ax25"
	"github.com/bbeamer41/crossbeam/internal/settings"
)

// Config is the assistant settings read for one question or one chat.
type Config struct {
	Enabled      bool
	Backend      string
	URL          string
	Model        string
	Key          string
	Users        []string
	Hourly       int
	Reply        int
	Timeout      time.Duration
	Instructions string
	Mesh         bool
	Station      string
}

// FromSettings reads a validated settings map. key is the API key from
// the key file, or empty.
func FromSettings(current map[string]string, key string) Config {
	users, _ := settings.ParseAIUsers(current[settings.KeyAIUsers])
	station := strings.TrimSpace(current[settings.KeyCallsign])
	if station == "" {
		station = "this station"
	}
	return Config{
		Enabled:      current[settings.KeyAIEnabled] == "true",
		Backend:      current[settings.KeyAIBackend],
		URL:          current[settings.KeyAIURL],
		Model:        current[settings.KeyAIModel],
		Key:          key,
		Users:        users,
		Hourly:       number(current[settings.KeyAIHourly], 20),
		Reply:        number(current[settings.KeyAIReply], 600),
		Timeout:      time.Duration(number(current[settings.KeyAITimeout], 90)) * time.Second,
		Instructions: strings.TrimSpace(current[settings.KeyAIInstructions]),
		Mesh:         current[settings.KeyAIMesh] == "true",
		Station:      station,
	}
}

func number(text string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

// Caller is who is asking and how they reached the station.
type Caller struct {
	// Call is the AX.25 callsign, or a MeshCore contact id when Mesh is set.
	Call string
	// Radio is true for a session heard over the KISS radio. Crossbeam
	// checked that callsign against the FCC before the session opened.
	Radio bool
	// Mesh is true for a MeshCore direct message.
	Mesh bool
	// Via is a short label for the log, such as "radio" or "netrom".
	Via string
}

// Allowed reports whether this caller may ask. The reason is a sentence
// for the caller when the answer is no.
func (c Config) Allowed(who Caller) (bool, string) {
	if !c.Enabled {
		return false, "The AI assistant is not turned on at this station."
	}
	if who.Mesh {
		if !c.Mesh {
			return false, "The AI assistant is not offered over MeshCore here."
		}
		return true, ""
	}
	addr, err := ax25.Parse(who.Call)
	if err != nil || who.Call == "" {
		return false, "The AI assistant needs a callsign. Connect over packet radio to use it."
	}
	if len(c.Users) == 0 {
		if who.Radio {
			return true, ""
		}
		return false, "The AI assistant is open to radio callers only."
	}
	for _, u := range c.Users {
		if u == addr.Call {
			return true, ""
		}
	}
	return false, "Your callsign is not on the AI assistant list here."
}

// limitKey is the rate-limit identity. The SSID is dropped so one
// operator cannot get more questions by changing SSID.
func limitKey(who Caller) string {
	if who.Mesh {
		return "mesh:" + who.Call
	}
	if addr, err := ax25.Parse(who.Call); err == nil {
		return addr.Call
	}
	return who.Call
}
