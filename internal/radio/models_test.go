// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package radio

import (
	"reflect"
	"testing"
)

func TestParseModels(t *testing.T) {
	const list = "" +
		" Rig #  Mfg                    Model                   Version         Status      Macro\n" +
		"     1  Hamlib                 Dummy                   20240709.0      Stable      RIG_MODEL_DUMMY\n" +
		"     2  Hamlib                 NET rigctl              20250211.0      Stable      RIG_MODEL_NETRIGCTL\n" +
		"     4  FLRig                                          20250107.0      Stable      RIG_MODEL_FLRIG\n" +
		"     6  Hamlib                 Dummy No VFO            20240409.0      Stable      RIG_MODEL_DUMMY_NOVFO\n" +
		"  1035  Yaesu                  FT-991                  20241118.18     Stable      RIG_MODEL_FT991\n" +
		"  3073  Icom                   IC-7300                 20250517.14     Stable      RIG_MODEL_IC7300\n"
	got := ParseModels(list)
	want := []Model{
		{ID: "4", Label: "FLRig"},
		{ID: "2", Label: "Hamlib NET rigctl"},
		{ID: "3073", Label: "Icom IC-7300"},
		{ID: "1035", Label: "Yaesu FT-991"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%#v", got)
	}
	if ParseModels("not a rigctl list\n1 2 3\n") != nil {
		t.Fatal("bad header was parsed")
	}
}
