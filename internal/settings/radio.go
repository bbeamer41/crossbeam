// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package settings

import (
	"fmt"
	"strconv"
	"strings"
)

const radioDevicePrefix = "/dev/serial/by-id/"

func validateRadio(out map[string]string) error {
	model, err := canonicalModel(out[KeyRadioModel])
	if err != nil {
		return err
	}
	device, err := canonicalDevice(out[KeyRadioDevice])
	if err != nil {
		return err
	}
	freq, err := canonicalFrequency(out[KeyRadioFrequency])
	if err != nil {
		return err
	}
	mode, err := canonicalMode(out[KeyRadioMode])
	if err != nil {
		return err
	}
	out[KeyRadioModel] = model
	out[KeyRadioDevice] = device
	out[KeyRadioFrequency] = freq
	out[KeyRadioMode] = mode
	if model == "" {
		return nil
	}
	if device == "" {
		return fmt.Errorf("radio.device is required when radio.model is set")
	}
	if freq == "" {
		return fmt.Errorf("radio.frequency is required when radio.model is set")
	}
	return nil
}

func canonicalModel(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > 999999 || strconv.Itoa(n) != value {
		return "", fmt.Errorf("radio.model must be a hamlib model number")
	}
	return value, nil
}

func canonicalDevice(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	name := value
	if strings.HasPrefix(value, radioDevicePrefix) {
		name = strings.TrimPrefix(value, radioDevicePrefix)
	} else if strings.Contains(value, "/") {
		return "", fmt.Errorf("radio.device must be a /dev/serial/by-id name")
	}
	if name == "" || name == "." || name == ".." || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return "", fmt.Errorf("radio.device must be a /dev/serial/by-id name")
	}
	return radioDevicePrefix + name, nil
}

func canonicalFrequency(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	hz, err := FrequencyHz(value)
	if err != nil {
		return "", err
	}
	return formatMHz(hz), nil
}

func canonicalMode(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "FM", nil
	}
	switch value {
	case "FM", "USB", "LSB", "AM":
		return value, nil
	default:
		return "", fmt.Errorf("radio.mode must be FM, USB, LSB, or AM")
	}
}

// FrequencyHz converts a canonical MHz value to hertz.
func FrequencyHz(mhz string) (int64, error) {
	mhz = strings.TrimSpace(mhz)
	whole, frac, _ := strings.Cut(mhz, ".")
	if whole == "" || strings.Contains(frac, ".") || len(frac) > 6 {
		return 0, fmt.Errorf("radio.frequency must be a frequency in MHz")
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w < 0 || strings.TrimLeft(whole, "0123456789") != "" {
		return 0, fmt.Errorf("radio.frequency must be a frequency in MHz")
	}
	for len(frac) < 6 {
		frac += "0"
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("radio.frequency must be a frequency in MHz")
	}
	hz := w*1_000_000 + f
	if hz < 100_000 || hz > 6_000_000_000 {
		return 0, fmt.Errorf("radio.frequency must be between 0.1 and 6000 MHz")
	}
	return hz, nil
}

func formatMHz(hz int64) string {
	whole := hz / 1_000_000
	frac := hz % 1_000_000
	if frac == 0 {
		return strconv.FormatInt(whole, 10)
	}
	text := fmt.Sprintf("%d.%06d", whole, frac)
	return strings.TrimRight(text, "0")
}
