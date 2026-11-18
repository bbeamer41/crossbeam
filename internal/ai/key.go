// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package ai

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// KeyName is the API key file in the state directory, mode 0600. The key
// is not a setting, so it is never shown on the panel, in crossbeam
// settings, in the MCP status, or in a log.
const KeyName = "ai-key"

const maxKey = 512

// ReadKey returns the saved API key, or empty when there is none.
func ReadKey(dir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(dir, KeyName))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if len(raw) > maxKey {
		return "", errors.New("the AI key file is too long")
	}
	return strings.TrimSpace(string(raw)), nil
}

// HasKey reports whether a key file is present.
func HasKey(dir string) bool {
	key, err := ReadKey(dir)
	return err == nil && key != ""
}

// ValidateKey checks a key before it is saved.
func ValidateKey(key string) error {
	if len(key) > maxKey {
		return errors.New("the API key is too long")
	}
	for i := 0; i < len(key); i++ {
		if key[i] <= 0x20 || key[i] >= 0x7f {
			return errors.New("the API key must be printable text with no spaces")
		}
	}
	return nil
}

// WriteKey saves key with mode 0600. An empty key removes the file. When
// root writes it, the file is given to the owner of the state directory
// so the crossbeam service can still read it.
func WriteKey(dir, key string) error {
	path := filepath.Join(dir, KeyName)
	key = strings.TrimSpace(key)
	if key == "" {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := ValidateKey(key); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".ai-key-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(key + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if info, err := os.Stat(dir); err == nil {
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				_ = os.Chown(name, int(st.Uid), int(st.Gid))
			}
		}
	}
	return os.Rename(name, path)
}
