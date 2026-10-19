// Copyright (C) 2026 K5LEB
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func main() {
	if len(os.Args) == 2 && (os.Args[1] == "version" || os.Args[1] == "--version") {
		fmt.Println("crossbeam", version)
		return
	}
	fmt.Printf("crossbeam %s\n", version)
	fmt.Println("Article formats only in this build. See docs/article.md.")
}
