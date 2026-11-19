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

const aiUsage = "crossbeam: usage: crossbeam ai key [--clear] [--state DIR] | crossbeam ai test [QUESTION] [--state DIR] | crossbeam ai log [-n COUNT] [--state DIR] | crossbeam ai senders list|approve ID|deny ID|revoke ID [--state DIR]"

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
	case "test", "senders":
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
	case "senders":
		return aiSenders(*state, fs.Args(), stdout, stderr)
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
	if cfg, err := svc.Config(ctx); err == nil {
		if warn := ai.URLWarning(cfg.URL, nil); warn != "" {
			fmt.Fprintf(stderr, "crossbeam: %s\n", warn)
		}
	}
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

func aiSenders(state string, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprintln(stderr, aiUsage)
		return 2
	}
	op := args[0]
	switch op {
	case "list":
		if len(args) != 1 {
			fmt.Fprintln(stderr, aiUsage)
			return 2
		}
	case "approve", "deny", "revoke":
		if len(args) != 2 {
			fmt.Fprintln(stderr, aiUsage)
			return 2
		}
	default:
		fmt.Fprintln(stderr, aiUsage)
		return 2
	}
	st, err := store.OpenExisting(state)
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	defer st.Close()
	svc := ai.New(st.Settings, st.Dir())
	if op == "list" {
		lists, err := svc.Senders()
		if err != nil {
			fmt.Fprintln(stderr, "crossbeam: the AI sender list could not be read")
			return 1
		}
		printSenders(stdout, "Pending", lists.Pending)
		printSenders(stdout, "Approved", lists.Approved)
		printSenders(stdout, "Denied", lists.Denied)
		return 0
	}
	id, err := canonicalSender(args[1])
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 2
	}
	switch op {
	case "approve":
		err = svc.Approve(id)
	case "deny":
		err = svc.Deny(id)
	default:
		err = svc.Revoke(id)
	}
	if err != nil {
		fmt.Fprintf(stderr, "crossbeam: %s\n", err)
		return 1
	}
	switch op {
	case "approve":
		fmt.Fprintf(stdout, "Approved %s.\n", id)
	case "deny":
		fmt.Fprintf(stdout, "Blocked %s.\n", id)
	default:
		fmt.Fprintf(stdout, "Revoked %s.\n", id)
	}
	return 0
}

func canonicalSender(id string) (string, error) {
	network, address, err := ai.ParseSenderID(id)
	if err != nil {
		return "", err
	}
	return network + ":" + address, nil
}

func printSenders(stdout io.Writer, title string, list []ai.Sender) {
	fmt.Fprintln(stdout, title)
	if len(list) == 0 {
		fmt.Fprintln(stdout, "  (none)")
		return
	}
	for _, s := range list {
		when := ""
		if !s.Seen.IsZero() {
			when = "  " + s.Seen.Local().Format("2006-01-02 15:04:05 MST")
		}
		name := s.Address
		if s.Name != "" {
			name = s.Address + " " + s.Name
		}
		fmt.Fprintf(stdout, "  %s %s%s\n", s.Network, name, when)
		if s.Text != "" {
			fmt.Fprintf(stdout, "    %s\n", s.Text)
		}
	}
}
