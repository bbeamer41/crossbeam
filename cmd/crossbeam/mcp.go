// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/bbeamer41/crossbeam/internal/mcp"
	"github.com/bbeamer41/crossbeam/internal/store"
)

func cmdMCP(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultState, "state directory")
	if err := parseArgs(fs, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "crossbeam: usage: crossbeam mcp [--state DIR]")
		return 2
	}
	st, err := store.Open(*state)
	if err != nil {
		fmt.Fprintln(stderr, "crossbeam: the article store is not available")
		return 1
	}
	defer st.Close()
	err = mcp.Serve(ctx, st, stdin, stdout)
	if err == nil || errors.Is(err, context.Canceled) {
		return 0
	}
	fmt.Fprintln(stderr, "crossbeam: the mcp session ended")
	return 1
}
