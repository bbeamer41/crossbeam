// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (len(args) == 1 && isHelp(args[0])) {
		fmt.Fprintf(stdout, "crossbeam %s\n", version)
		fmt.Fprintln(stdout, "Article formats only in this build. See docs/article.md.")
		return 0
	}
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		fmt.Fprintln(stdout, "crossbeam", version)
		return 0
	}
	fmt.Fprintf(stderr, "crossbeam: unknown command %q\n", strings.Join(args, " "))
	return 2
}

func isHelp(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}
