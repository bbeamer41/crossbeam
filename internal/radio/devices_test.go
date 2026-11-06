// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package radio

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDeviceNames(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"usb-radio", "usb-other"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := deviceNames(dir)
	want := []string{dir + "/usb-other", dir + "/usb-radio"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
	if deviceNames(filepath.Join(dir, "missing")) != nil {
		t.Fatal("missing directory returned names")
	}
}
