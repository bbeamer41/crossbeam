// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

// Package settings is the schema for the admin panel. The browser, the
// command, and the settings document all use these keys and help lines.
package settings

import (
	"fmt"
	"os"
	"path/filepath"
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
	KeyCallsign       = "station.callsign"
	KeyName           = "station.name"
	KeyListen         = "admin.listen"
	KeyPublic         = "admin.public"
	KeyLinkListen     = "link.listen"
	KeyLinkPeers      = "link.peers"
	KeyKiss           = "kiss.tnc"
	KeyRadioModel     = "radio.model"
	KeyRadioDevice    = "radio.device"
	KeyRadioFrequency = "radio.frequency"
	KeyRadioMode      = "radio.mode"
	KeyTelnetListen   = "telnet.listen"
	KeyTelnetPeers    = "telnet.peers"
	KeyFBBListen      = "fbb.listen"
	KeyFBBPeers       = "fbb.peers"
	KeyBulletinTo     = "bulletin.to"
	KeyBulletinAt     = "bulletin.at"
	KeyPages          = "nomad.pages"
)

// Field is one setting. Label is the plain name on the panel. Example
// is shown in an empty box. Help is the longer explanation.
type Field struct {
	Key      string
	Label    string
	Example  string
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
		Label:    "Callsign",
		Example:  "K5LEB",
		Help:     "Your amateur callsign. Compose uses it when the author box is empty. The packet link, the radio modem, telnet, and bulletin forwarding use it as the address of this station. When one of those is turned on, it has to be an AX.25 callsign, with an optional SSID such as K5LEB-1.",
		Kind:     KindLine,
		MaxRunes: article.MaxAuthorRunes,
	},
	{
		Key:      KeyName,
		Label:    "Station name",
		Example:  "Home",
		Help:     "A short name for this station. It is shown in the header of this panel so you can tell which node you are using.",
		Kind:     KindLine,
		MaxRunes: 80,
	},
	{
		Key:     KeyListen,
		Label:   "Panel address",
		Example: "192.168.1.20:8443",
		Help:    "The address of this web page, written as host:port, such as 192.168.1.20:8443. Leave it empty and Crossbeam chooses a private network address on port 8443. A saved address is used the next time Crossbeam starts. This page keeps the address it already opened.",
		Kind:    KindListen,
	},
	{
		Key:     KeyPublic,
		Label:   "Public address",
		Help:    "Allow this page to listen on a public internet address. Leave this off to keep the panel on your own network. The sysop password is still required, and the browser warns until the certificate is trusted.",
		Kind:    KindBool,
		Default: "false",
	},
	{
		Key:      KeyLinkListen,
		Label:    "Listen address",
		Example:  "192.168.1.20:10093",
		Help:     "The address other packet stations use to reach this one, written as host:port. Port 10093 is the usual choice. Leave it empty to keep the link off. Each UDP packet carries one AX.25 frame. A saved address is used the next time Crossbeam starts.",
		Kind:     KindLine,
		MaxRunes: 256,
	},
	{
		Key:      KeyLinkPeers,
		Label:    "Stations to call",
		Example:  "W1AW-1@192.168.1.20:10093",
		Help:     "Stations this node will call. Write each one as CALL@host:port, separated by spaces or commas, such as W1AW-1@192.168.1.20:10093. Bulletin forwarding on the packet link is accepted only from a station in this list. A station that is not listed can still reach the keyboard prompt. The list is read the next time Crossbeam starts.",
		Kind:     KindLine,
		MaxRunes: 2048,
	},
	{
		Key:      KeyKiss,
		Label:    "Modem address",
		Example:  "127.0.0.1:6700",
		Help:     "The address of the KISS modem, such as 127.0.0.1:6700 for a modem on this computer. Leave it empty to keep the radio off. Crossbeam sends 1200 baud packet to that modem. The modem turns the text into tones and keys the transmitter. The address is used the next time Crossbeam starts.",
		Kind:     KindLine,
		MaxRunes: 256,
	},
	{
		Key:      KeyRadioModel,
		Label:    "Radio",
		Help:     "The radio plugged into this computer. Choose it from the list, or leave it off. Saving a choice sets the frequency and mode on that radio. The modem still sends the audio and keys the transmitter.",
		Kind:     KindLine,
		MaxRunes: 6,
	},
	{
		Key:      KeyRadioDevice,
		Label:    "USB port",
		Help:     "The USB port of the radio. Choose the stable name from the list. That name lives under /dev/serial/by-id and still matches after a reboot. Required when a radio is selected.",
		Kind:     KindLine,
		MaxRunes: 256,
	},
	{
		Key:      KeyRadioFrequency,
		Label:    "Frequency",
		Example:  "145.030",
		Help:     "The frequency to send to the radio, in MHz. For example, 145.030 is 145.030 MHz. Required when a radio is selected. Crossbeam does not pick a packet frequency for you.",
		Kind:     KindLine,
		MaxRunes: 16,
	},
	{
		Key:      KeyRadioMode,
		Label:    "Mode",
		Help:     "The mode to send to the radio. FM is the usual choice for packet. USB, LSB, and AM are also available. An empty value uses FM. Choosing a mode does not key the transmitter.",
		Kind:     KindLine,
		Default:  "FM",
		MaxRunes: 8,
	},
	{
		Key:      KeyTelnetListen,
		Label:    "Telnet address",
		Example:  "192.168.1.20:8770",
		Help:     "The address for a keyboard session over telnet, written as host:port, such as 192.168.1.20:8770. Leave it empty to keep telnet off. The session is readable text, with no encryption. The address is used the next time Crossbeam starts.",
		Kind:     KindLine,
		MaxRunes: 256,
	},
	{
		Key:      KeyTelnetPeers,
		Label:    "Telnet stations",
		Example:  "W1AW-1@192.168.1.20:8770",
		Help:     "Stations this node will call over telnet. Write each one as CALL@host:port, such as W1AW-1@192.168.1.20:8770. Anyone who can open the port can reach the keyboard prompt. Bulletin forwarding on telnet is accepted only from a station in this list. The list is read the next time Crossbeam starts.",
		Kind:     KindLine,
		MaxRunes: 2048,
	},
	{
		Key:      KeyFBBListen,
		Label:    "Forwarding address",
		Example:  "192.168.1.20:6300",
		Help:     "The address for bulletin forwarding over TCP, written as host:port, such as 192.168.1.20:6300. Leave it empty to keep this forwarding off. The session is the FBB bulletin exchange as readable text, without telnet commands. The address is used the next time Crossbeam starts.",
		Kind:     KindLine,
		MaxRunes: 256,
	},
	{
		Key:      KeyFBBPeers,
		Label:    "Stations to forward to",
		Example:  "W1AW-1@192.168.1.20:6300",
		Help:     "Stations this node will forward bulletins to over TCP. Write each one as CALL@host:port, such as W1AW-1@192.168.1.20:6300. Crossbeam uses this list when the station is not already in the packet-link list. An incoming forward is accepted only from a station in this list. The list is read the next time Crossbeam starts.",
		Kind:     KindLine,
		MaxRunes: 2048,
	},
	{
		Key:      KeyBulletinTo,
		Label:    "Category",
		Example:  "ALL",
		Help:     "The category other stations see on a bulletin this station forwards. ALL is the usual category. Use letters and digits, at most 12. An empty value is stored as ALL. The next forward uses a saved change.",
		Kind:     KindLine,
		Default:  "ALL",
		MaxRunes: 12,
	},
	{
		Key:      KeyBulletinAt,
		Label:    "Distribution",
		Example:  "WW",
		Help:     "The distribution other stations see on a bulletin this station forwards. WW is the widest distribution. A hierarchical address such as K5LEB.#DFW.TX is also valid. Use letters, digits, dots, hashes, and hyphens, at most 40. An empty value is stored as WW. The next forward uses a saved change.",
		Kind:     KindLine,
		Default:  "WW",
		MaxRunes: 40,
	},
	{
		Key:      KeyPages,
		Label:    "Pages folder",
		Example:  "/var/lib/nomadnetwork/storage/pages",
		Help:     "A folder where each bulletin is also written as a NomadNet page, such as /var/lib/nomadnetwork/storage/pages. Leave it empty to keep bulletins in Crossbeam only. The folder must already exist. A page is an ordinary .mu file. Publishing and importing use the saved folder immediately.",
		Kind:     KindLine,
		MaxRunes: 256,
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
	kiss, err := canonicalKiss(out[KeyKiss])
	if err != nil {
		return nil, err
	}
	out[KeyKiss] = kiss
	if err := validateLink(out); err != nil {
		return nil, err
	}
	if err := validateBulletin(out); err != nil {
		return nil, err
	}
	pages, err := canonicalPages(out[KeyPages])
	if err != nil {
		return nil, err
	}
	out[KeyPages] = pages
	if err := validateRadio(out); err != nil {
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

func canonicalPages(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	for _, part := range strings.Split(value, string(os.PathSeparator)) {
		if part == ".." {
			return "", fmt.Errorf("nomad.pages must be a clean absolute path")
		}
	}
	cleaned := filepath.Clean(value)
	if !filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("nomad.pages must be an absolute path")
	}
	return cleaned, nil
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
	telnetPeers, err := ParsePeerList(KeyTelnetPeers, out[KeyTelnetPeers])
	if err != nil {
		return err
	}
	out[KeyTelnetPeers] = FormatPeers(telnetPeers)
	fbbPeers, err := ParsePeerList(KeyFBBPeers, out[KeyFBBPeers])
	if err != nil {
		return err
	}
	out[KeyFBBPeers] = FormatPeers(fbbPeers)
	telnetListen, err := CanonicalListen(KeyTelnetListen, out[KeyTelnetListen])
	if err != nil {
		return err
	}
	out[KeyTelnetListen] = telnetListen
	fbbListen, err := CanonicalListen(KeyFBBListen, out[KeyFBBListen])
	if err != nil {
		return err
	}
	out[KeyFBBListen] = fbbListen
	call, callErr := ax25.Parse(out[KeyCallsign])
	if out[KeyLinkListen] != "" && callErr != nil {
		return fmt.Errorf("station.callsign must be an AX.25 callsign when link.listen is set")
	}
	if out[KeyKiss] != "" && callErr != nil {
		return fmt.Errorf("station.callsign must be an AX.25 callsign when kiss.tnc is set")
	}
	if out[KeyTelnetListen] != "" && callErr != nil {
		return fmt.Errorf("station.callsign must be an AX.25 callsign when telnet.listen is set")
	}
	if out[KeyFBBListen] != "" && callErr != nil {
		return fmt.Errorf("station.callsign must be an AX.25 callsign when fbb.listen is set")
	}
	if callErr == nil {
		if err := rejectSelf(call.String(), "link.peers", peers); err != nil {
			return err
		}
		if err := rejectSelf(call.String(), KeyTelnetPeers, telnetPeers); err != nil {
			return err
		}
		if err := rejectSelf(call.String(), KeyFBBPeers, fbbPeers); err != nil {
			return err
		}
	}
	return nil
}

func rejectSelf(call, key string, peers []Peer) error {
	for _, peer := range peers {
		if peer.Call == call {
			return fmt.Errorf("%s cannot list this station", key)
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
