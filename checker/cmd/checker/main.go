// Command checker detects domains blocked in Iran by DNS poisoning or SNI filtering.
//
// Run it without arguments for interactive mode, or with flags for scripts.
// It must be run inside Iran without VPN.
package main

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"

	"github.com/armanlife/iran-blocked-domains/checker/internal/cli"
	"github.com/armanlife/iran-blocked-domains/checker/internal/tui"
)

func main() {
	var err error
	if len(os.Args) == 1 && isTerminal(os.Stdin) && isTerminal(os.Stdout) {
		err = tui.Run()
	} else {
		err = cli.Run(os.Args[1:])
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func isTerminal(f *os.File) bool {
	return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
}
