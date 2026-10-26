// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	versionpkg "github.com/bbeamer41/crossbeam/internal/version"
)

const version = versionpkg.String

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(args) == 0 || (len(args) == 1 && isHelp(args[0])) {
		printHelp(stdout)
		return 0
	}
	switch args[0] {
	case "version", "--version":
		if len(args) != 1 {
			return unknown(args, stderr)
		}
		fmt.Fprintln(stdout, "crossbeam", version)
		return 0
	case "help", "--help", "-h":
		return unknown(args, stderr)
	case "serve":
		return cmdServe(ctx, args[1:], stdout, stderr)
	case "sysop":
		return cmdSysop(ctx, args[1:], stdin, stdout, stderr)
	case "settings":
		return cmdSettings(ctx, args[1:], stdout, stderr)
	case "status":
		return cmdStatus(ctx, args[1:], stdout, stderr)
	case "article":
		return cmdArticle(ctx, args[1:], stdout, stderr)
	case "connect":
		return cmdConnect(ctx, args[1:], stdin, stdout, stderr)
	case "bulletin":
		return cmdBulletin(ctx, args[1:], stdout, stderr)
	case "page":
		return cmdPage(ctx, args[1:], stdout, stderr)
	default:
		return unknown(args, stderr)
	}
}

func printHelp(stdout io.Writer) {
	fmt.Fprintf(stdout, "crossbeam %s\n", version)
	fmt.Fprintln(stdout, "Admin panel, article store, AX/UDP link, NET/ROM routing, KISS radio, FBB bulletins, NomadNet pages, and article formats. See docs/article.md, docs/link.md, docs/bulletin.md, docs/pages.md, docs/netrom.md, docs/radio.md, and docs/settings.md.")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "  crossbeam serve [--state DIR] [--listen HOST:PORT] [--link HOST:PORT] [--peers LIST] [--kiss HOST:PORT] [--socket PATH] [--socket-mode 0600|0660]")
	fmt.Fprintln(stdout, "  crossbeam connect CALL [--state DIR | --socket PATH]")
	fmt.Fprintln(stdout, "  crossbeam bulletin send CALL [--state DIR | --socket PATH]")
	fmt.Fprintln(stdout, "  crossbeam sysop password [--state DIR]")
	fmt.Fprintln(stdout, "  crossbeam settings [--state DIR | --socket PATH]")
	fmt.Fprintln(stdout, "  crossbeam settings get KEY [--state DIR | --socket PATH]")
	fmt.Fprintln(stdout, "  crossbeam settings set KEY VALUE [--state DIR | --socket PATH]")
	fmt.Fprintln(stdout, "  crossbeam status [--state DIR | --socket PATH]")
	fmt.Fprintln(stdout, "  crossbeam article preview --title TITLE [--author CALL] [--id ID] [--created TIME] [--body TEXT | --body-file PATH]")
	fmt.Fprintln(stdout, "  crossbeam article save --title TITLE [--author CALL] [--id ID] [--created TIME] [--body TEXT | --body-file PATH] [--state DIR | --socket PATH]")
	fmt.Fprintln(stdout, "  crossbeam article list [--state DIR | --socket PATH]")
	fmt.Fprintln(stdout, "  crossbeam article delete ROW [--state DIR]")
	fmt.Fprintln(stdout, "  crossbeam page publish [--state DIR]")
	fmt.Fprintln(stdout, "  crossbeam page ingest [--state DIR]")
	fmt.Fprintln(stdout, "  crossbeam version")
}

func isHelp(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func unknown(args []string, stderr io.Writer) int {
	fmt.Fprintf(stderr, "crossbeam: unknown command %q\n", strings.Join(args, " "))
	return 2
}
