// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package radio

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Model is one hamlib radio the operator can choose.
type Model struct {
	ID    string
	Label string
}

// Models runs `rigctl -l` and returns the radios, without the hamlib
// dummy entries. The list is empty when rigctl is not installed.
func Models(ctx context.Context) ([]Model, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "rigctl", "-l").Output()
	if err != nil {
		return nil, fmt.Errorf("rigctl -l: %w", err)
	}
	return ParseModels(string(out)), nil
}

// ParseModels reads the fixed columns of `rigctl -l`.
func ParseModels(text string) []Model {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 {
		return nil
	}
	mfgAt := strings.Index(lines[0], "Mfg")
	modelAt := strings.Index(lines[0], "Model")
	versionAt := strings.Index(lines[0], "Version")
	if mfgAt < 1 || modelAt <= mfgAt || versionAt <= modelAt {
		return nil
	}
	var models []Model
	for _, line := range lines[1:] {
		if len(line) < versionAt {
			continue
		}
		id := strings.TrimSpace(line[:mfgAt])
		mfg := strings.TrimSpace(line[mfgAt:modelAt])
		name := strings.TrimSpace(line[modelAt:versionAt])
		if id == "" || strings.Contains(strings.ToLower(mfg+" "+name), "dummy") {
			continue
		}
		label := strings.TrimSpace(mfg + " " + name)
		if label == "" {
			continue
		}
		models = append(models, Model{ID: id, Label: label})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Label == models[j].Label {
			return models[i].ID < models[j].ID
		}
		return models[i].Label < models[j].Label
	})
	return models
}
