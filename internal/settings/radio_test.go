// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package settings

import "testing"

func TestRadioSettings(t *testing.T) {
	got, err := Validate(nil)
	if err != nil || got[KeyRadioModel] != "" || got[KeyRadioDevice] != "" || got[KeyRadioFrequency] != "" || got[KeyRadioMode] != "FM" {
		t.Fatalf("defaults %v %v", got, err)
	}

	_, err = Validate(map[string]string{KeyRadioModel: "03073"})
	if err == nil || err.Error() != "radio.model must be a hamlib model number" {
		t.Fatalf("leading zero: %v", err)
	}
	_, err = Validate(map[string]string{
		KeyRadioModel:     "3073",
		KeyRadioDevice:    "usb-Icom_IC-7300_if00",
		KeyRadioFrequency: "145.030",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = Validate(map[string]string{
		KeyRadioModel:     "3073",
		KeyRadioDevice:    "usb-Icom_IC-7300_if00",
		KeyRadioFrequency: "145.030",
		KeyRadioMode:      "fm",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[KeyRadioModel] != "3073" || got[KeyRadioDevice] != "/dev/serial/by-id/usb-Icom_IC-7300_if00" || got[KeyRadioFrequency] != "145.03" || got[KeyRadioMode] != "FM" {
		t.Fatalf("canonical %v", got)
	}
	hz, err := FrequencyHz("145.030")
	if err != nil || hz != 145030000 {
		t.Fatalf("hertz %d %v", hz, err)
	}
	hz, err = FrequencyHz(got[KeyRadioFrequency])
	if err != nil || hz != 145030000 {
		t.Fatalf("stored hertz %d %v", hz, err)
	}
	if hz, err = FrequencyHz("0.1"); err != nil || hz != 100000 {
		t.Fatalf("low edge %d %v", hz, err)
	}
	if hz, err = FrequencyHz("6000"); err != nil || hz != 6000000000 {
		t.Fatalf("high edge %d %v", hz, err)
	}

	_, err = Validate(map[string]string{KeyRadioModel: "3073"})
	if err == nil || err.Error() != "radio.device is required when radio.model is set" {
		t.Fatalf("missing device: %v", err)
	}
	_, err = Validate(map[string]string{KeyRadioModel: "3073", KeyRadioDevice: "usb-Icom_IC-7300_if00"})
	if err == nil || err.Error() != "radio.frequency is required when radio.model is set" {
		t.Fatalf("missing frequency: %v", err)
	}
	for _, device := range []string{"/dev/ttyUSB0", "/dev/serial/by-id/../ttyUSB0", "/dev/serial/by-id/usb/foo", "usb/foo", "..", "."} {
		_, err = Validate(map[string]string{
			KeyRadioModel:     "3073",
			KeyRadioDevice:    device,
			KeyRadioFrequency: "145.03",
		})
		if err == nil || err.Error() != "radio.device must be a /dev/serial/by-id name" {
			t.Fatalf("%s: %v", device, err)
		}
	}
	_, err = Validate(map[string]string{KeyRadioModel: "0", KeyRadioDevice: "usb-a", KeyRadioFrequency: "145"})
	if err == nil {
		t.Fatal("model 0 was accepted")
	}
	_, err = Validate(map[string]string{KeyRadioFrequency: "0.05"})
	if err == nil || err.Error() != "radio.frequency must be between 0.1 and 6000 MHz" {
		t.Fatalf("low: %v", err)
	}
	_, err = Validate(map[string]string{KeyRadioFrequency: "6001"})
	if err == nil || err.Error() != "radio.frequency must be between 0.1 and 6000 MHz" {
		t.Fatalf("high: %v", err)
	}
	_, err = Validate(map[string]string{KeyRadioFrequency: "145.030.1"})
	if err == nil || err.Error() != "radio.frequency must be a frequency in MHz" {
		t.Fatalf("extra dot: %v", err)
	}
	_, err = Validate(map[string]string{KeyRadioMode: "CW"})
	if err == nil || err.Error() != "radio.mode must be FM, USB, LSB, or AM" {
		t.Fatalf("mode: %v", err)
	}

	got, err = Validate(map[string]string{
		KeyRadioModel:     "",
		KeyRadioDevice:    "/dev/serial/by-id/usb-Icom_IC-7300_if00",
		KeyRadioFrequency: "145.030",
		KeyRadioMode:      "",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[KeyRadioModel] != "" || got[KeyRadioDevice] != "/dev/serial/by-id/usb-Icom_IC-7300_if00" || got[KeyRadioFrequency] != "145.03" || got[KeyRadioMode] != "FM" {
		t.Fatalf("radio off %v", got)
	}
}
