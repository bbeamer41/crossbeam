// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bbeamer41/crossbeam/internal/ai"
	"github.com/bbeamer41/crossbeam/internal/store"
)

const aiUsage = "crossbeam: usage: crossbeam ai key [--clear] [--state DIR] | crossbeam ai test [QUESTION] [--state DIR] | crossbeam ai log [-n COUNT] [--state DIR]"

func cmdAI(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	sub, rest := splitSub(args)
	if sub == "" || isHelp(sub) {
		fmt.Fprintln(stderr, aiUsage)
		if isHelp(sub) {
			return 0
		}
		return 2
	}
	fs := flag.NewFlagSet("ai "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", defaultState, "state directory")
	remove := false
	count := 20
	switch sub {
	case "key":
		fs.BoolVar(&remove, "clear", false, "remove the saved key")
	case "log":
		fs.IntVar(&count, "n", 20, "how many entries to show")
	case "test":
	default:
		fmt.Fprintln(stderr, aiUsage)
		return 2
	}
	if err := parseArgs(fs, rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if info, err := os.Stat(*state); err != nil || !info.IsDir() {
		fmt.Fprintf(stderr, "crossbeam: the state directory %s is not there\n", *state)
		return 1
	}
	switch sub {
	case "key":
		if fs.NArg() != 0 {
			fmt.Fprintln(stderr, aiUsage)
			return 2
		}
		return aiKey(*state, remove, stdin, stdout, stderr)
	case "log":
		if fs.NArg() != 0 || count < 1 {
			fmt.Fprintln(stderr, aiUsage)
			return 2
		}
		return aiLog(*state, count, stdout, stderr)
	default:
		return aiTest(ctx, *state, strings.Join(fs.Args(), " "), stdout, stderr)
	}
}

// aiKey saves the API key for Open WebUI or another server that wants
// one. The key is read without echo and is never printed.
func aiKey(state string, remove bool, stdin io.Reader, stdout, stderr io.Writer) int {
	if remove {
		if err := ai.WriteKey(state, ""); err != nil {
			fmt.Fprintln(stderr, "crossbeam: the AI key could not be removed")
			return 1
		}
		fmt.Fprintln(stdout, "AI key removed.")
		return 0
	}
	key, err := readPasswordLine(stdin, stderr, "AI server API key: ")
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", strings.ReplaceAll(err.Error(), "password", "key"))
		return 1
	}
	key = strings.TrimSpace(key)
	if key == "" {
		fmt.Fprintln(stderr, "crossbeam: no key was entered; use --clear to remove the saved key")
		return 1
	}
	if err := ai.ValidateKey(key); err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	if err := ai.WriteKey(state, key); err != nil {
		fmt.Fprintln(stderr, "crossbeam: the AI key could not be saved")
		return 1
	}
	fmt.Fprintln(stdout, "AI key saved.")
	return 0
}

func aiTest(ctx context.Context, state, question string, stdout, stderr io.Writer) int {
	if strings.TrimSpace(question) == "" {
		question = "In one sentence, what is packet radio?"
	}
	st, err := store.OpenExisting(state)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	defer st.Close()
	svc := ai.New(st.Settings, st.Dir())
	start := time.Now()
	reply, err := svc.Test(ctx, question)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	fmt.Fprintln(stdout, reply)
	fmt.Fprintf(stdout, "(answered in %.1f s)\n", time.Since(start).Seconds())
	return 0
}

func aiLog(state string, count int, stdout, stderr io.Writer) int {
	entries, err := ai.ReadLog(state, count)
	if err != nil {
		fmt.Fprintln(stderr, "crossbeam: the AI log could not be read")
		return 1
	}
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "The AI log is empty.")
		return 0
	}
	for _, e := range entries {
		fmt.Fprintf(stdout, "%s  %s %s  %s\n", e.Time.Local().Format("2006-01-02 15:04:05 MST"), e.Caller, e.Via, e.Outcome)
		fmt.Fprintf(stdout, "  Q: %s\n", e.Question)
		if e.Reply != "" {
			fmt.Fprintf(stdout, "  A: %s\n", strings.ReplaceAll(e.Reply, "\n", "\n     "))
		}
	}
	return 0
}
