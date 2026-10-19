// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package settings is the schema for the admin panel. The browser, the
// command, and the settings document all use these keys and help lines.
package settings

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bbeamer41/crossbeam/internal/article"
	"github.com/bbeamer41/crossbeam/internal/ax25"
)

// Kind is how a setting is edited and checked.
type Kind int

const (
	// KindLine is one line of text.
	KindLine Kind = iota
	// KindBool is the canonical string true or false.
	KindBool
	// KindListen is a host:port, or empty to choose an address at start.
	KindListen
)

// Setting keys. These names are stored and shown as-is.
const (
	KeyCallsign   = "station.callsign"
	KeyName       = "station.name"
	KeyListen     = "admin.listen"
	KeyPublic     = "admin.public"
	KeyLinkListen = "link.listen"
	KeyLinkPeers  = "link.peers"
	KeyBulletinTo = "bulletin.to"
	KeyBulletinAt = "bulletin.at"
)

// Field is one setting.
type Field struct {
	Key      string
	Help     string
	Kind     Kind
	Default  string
	MaxRunes int
}

// Schema is every setting this build stores. The admin form is built
// from this list, so a new key is not left off the page.
var Schema = []Field{
	{
		Key:      KeyCallsign,
		Help:     "The station callsign. Compose uses it as the article author when the author box is empty. The AX/UDP link uses it as the station address.",
		Kind:     KindLine,
		MaxRunes: article.MaxAuthorRunes,
	},
	{
		Key:      KeyName,
		Help:     "A short name for this station, shown in the admin panel.",
		Kind:     KindLine,
		MaxRunes: 80,
	},
	{
		Key:  KeyListen,
		Help: "The host and port for the admin panel, such as 192.168.1.20:8443. Leave this empty to choose a private LAN address on port 8443. A change takes effect on the next start.",
		Kind: KindListen,
	},
	{
		Key:     KeyPublic,
		Help:    "Allow the admin panel to listen on a public address. Leave this false to keep the panel on the LAN.",
		Kind:    KindBool,
		Default: "false",
	},
	{
		Key:      KeyLinkListen,
		Help:     "The host and port for the AX/UDP link, such as 192.168.1.20:10093. Leave this empty to keep the link off. A change takes effect on the next start.",
		Kind:     KindLine,
		MaxRunes: 256,
	},
	{
		Key:      KeyLinkPeers,
		Help:     "Stations this node can call. Separate entries with spaces or commas. Each entry is CALL@host:port, such as W1AW-1@192.168.1.20:10093. A change takes effect on the next start.",
		Kind:     KindLine,
		MaxRunes: 2048,
	},
	{
		Key:      KeyBulletinTo,
		Help:     "The category used when this station forwards a bulletin, such as ALL. Letters and digits, at most 12. An empty value uses ALL.",
		Kind:     KindLine,
		Default:  "ALL",
		MaxRunes: 12,
	},
	{
		Key:      KeyBulletinAt,
		Help:     "The distribution used when this station forwards a bulletin, such as WW. Letters, digits, dots, hashes, and hyphens, at most 40. An empty value uses WW.",
		Kind:     KindLine,
		Default:  "WW",
		MaxRunes: 40,
	},
}

// Defaults returns a copy of the default value for every key.
func Defaults() map[string]string {
	out := make(map[string]string, len(Schema))
	for _, f := range Schema {
		out[f.Key] = f.Default
	}
	return out
}

// ByKey returns the schema field for a key.
func ByKey(key string) (Field, bool) {
	for _, f := range Schema {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// Validate checks a complete settings map and returns the canonical
// values. Unknown keys are rejected. A missing key keeps its default.
// The form token csrf is not a setting; remove it before calling Validate.
func Validate(values map[string]string) (map[string]string, error) {
	for key := range values {
		if _, ok := ByKey(key); !ok {
			return nil, unknownKey(key)
		}
	}
	out := Defaults()
	for _, field := range Schema {
		raw, ok := values[field.Key]
		if !ok {
			continue
		}
		value, err := validateField(field, raw)
		if err != nil {
			return nil, err
		}
		out[field.Key] = value
	}
	public := out[KeyPublic] == "true"
	if err := ValidateListen(out[KeyListen], public); err != nil {
		return nil, err
	}
	if err := validateLink(out); err != nil {
		return nil, err
	}
	if err := validateBulletin(out); err != nil {
		return nil, err
	}
	return out, nil
}

func validateBulletin(out map[string]string) error {
	to, err := canonicalBulletinTo(out[KeyBulletinTo])
	if err != nil {
		return err
	}
	at, err := canonicalBulletinAt(out[KeyBulletinAt])
	if err != nil {
		return err
	}
	out[KeyBulletinTo] = to
	out[KeyBulletinAt] = at
	return nil
}

func canonicalBulletinTo(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "ALL", nil
	}
	if len(value) > 12 || !lettersDigits(value) {
		return "", fmt.Errorf("bulletin.to must be letters and digits, at most 12")
	}
	return value, nil
}

func canonicalBulletinAt(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "WW", nil
	}
	if len(value) > 40 || !distribution(value) {
		return "", fmt.Errorf("bulletin.at must be letters, digits, dots, hashes, and hyphens, at most 40")
	}
	return value, nil
}

func lettersDigits(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func distribution(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '.' && c != '#' && c != '-' {
			return false
		}
	}
	return true
}

func validateLink(out map[string]string) error {
	peers, err := ParsePeers(out[KeyLinkPeers])
	if err != nil {
		return err
	}
	out[KeyLinkPeers] = FormatPeers(peers)
	if err := ValidateLinkListen(out[KeyLinkListen]); err != nil {
		return err
	}
	call, callErr := ax25.Parse(out[KeyCallsign])
	if out[KeyLinkListen] != "" && callErr != nil {
		return fmt.Errorf("station.callsign must be an AX.25 callsign when link.listen is set")
	}
	if callErr == nil {
		for _, peer := range peers {
			if peer.Call == call.String() {
				return fmt.Errorf("link.peers cannot list this station")
			}
		}
	}
	return nil
}

func validateField(field Field, raw string) (string, error) {
	switch field.Kind {
	case KindBool:
		value := strings.TrimSpace(raw)
		if value != "true" && value != "false" {
			return "", fmt.Errorf("%s must be true or false", field.Key)
		}
		return value, nil
	case KindListen:
		value := strings.TrimSpace(raw)
		if len(value) > 256 {
			return "", fmt.Errorf("%s is longer than 256 bytes", field.Key)
		}
		return value, nil
	case KindLine:
		return singleLine(field.Key, raw, field.MaxRunes)
	default:
		return "", fmt.Errorf("%s has no validator", field.Key)
	}
}

func singleLine(name, value string, max int) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("%s is not valid UTF-8", name)
	}
	value = strings.TrimSpace(value)
	for _, r := range value {
		switch {
		case r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029':
			return "", fmt.Errorf("%s must be a single line", name)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			return "", fmt.Errorf("%s contains a control character", name)
		case unicode.Is(unicode.Cf, r):
			return "", fmt.Errorf("%s contains a format character", name)
		}
	}
	if n := utf8.RuneCountInString(value); max > 0 && n > max {
		return "", fmt.Errorf("%s is %d runes, above the limit of %d", name, n, max)
	}
	return value, nil
}

func unknownKey(key string) error {
	if len(key) > 64 || !utf8.ValidString(key) {
		return fmt.Errorf("unknown setting")
	}
	return fmt.Errorf("unknown setting %q", key)
}
