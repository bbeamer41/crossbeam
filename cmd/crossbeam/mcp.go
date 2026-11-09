// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"path/filepath"

	"github.com/bbeamer41/crossbeam/internal/mcp"
	"github.com/bbeamer41/crossbeam/internal/store"
)

func cmdMCP(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultState, "state directory")
	socket := fs.String("socket", "", "admin socket path")
	if err := parseArgs(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "crossbeam: usage: crossbeam mcp [--state DIR] [--socket PATH]")
		return 2
	}
	chosen := ""
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "socket" {
			chosen = *socket
			if chosen == "" {
				chosen = filepath.Join(*state, "crossbeam.sock")
			}
		}
	})
	st, err := store.OpenExisting(*state)
	if err != nil {
		if errors.Is(err, store.ErrNoState) || errors.Is(err, store.ErrNoDatabase) {
			fmt.Fprintf(stderr, "crossbeam: %s\n", err)
			return 1
		}
		fmt.Fprintln(stderr, "crossbeam: the article store is not available")
		return 1
	}
	defer st.Close()
	err = mcp.ServeConfig(ctx, st, mcp.Config{Socket: chosen}, stdin, stdout)
	if err == nil || errors.Is(err, context.Canceled) {
		return 0
	}
	fmt.Fprintln(stderr, "crossbeam: the mcp session ended")
	return 1
}
