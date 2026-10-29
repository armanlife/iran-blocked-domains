//go:build !windows

package tui

import "fmt"

// ownWindow reports whether the terminal was opened only for the checker;
// only Windows opens one for a double click
func ownWindow() bool {
	return false
}

// setWindowTitle sets the title of the terminal window or tab, and returns a
// function that gives the title back to the terminal
func setWindowTitle(title string) (restore func()) {
	fmt.Print("\x1b]0;" + title + "\x07")
	return func() { fmt.Print("\x1b]0;\x07") }
}
