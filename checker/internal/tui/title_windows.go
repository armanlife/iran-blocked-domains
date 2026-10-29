package tui

import (
	"syscall"
	"unsafe"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	getConsoleTitleW      = kernel32.NewProc("GetConsoleTitleW")
	setConsoleTitleW      = kernel32.NewProc("SetConsoleTitleW")
	getConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
)

// ownWindow reports whether the console was opened only for the checker, as
// by a double click: then the checker is the only process attached to it,
// while in a terminal the shell is attached too
func ownWindow() bool {
	pids := make([]uint32, 4)
	n, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return n == 1
}

// setWindowTitle sets the title of the console window or terminal tab, and
// returns a function that sets the previous title again. The console API is
// used because escape sequences are not handled before the terminal is set
// up for them.
func setWindowTitle(title string) (restore func()) {
	old := make([]uint16, 1024)
	n, _, _ := getConsoleTitleW.Call(uintptr(unsafe.Pointer(&old[0])), uintptr(len(old)))

	setTitle(title)
	return func() {
		if n > 0 {
			setTitle(syscall.UTF16ToString(old[:n]))
		}
	}
}

func setTitle(title string) {
	p, err := syscall.UTF16PtrFromString(title)
	if err == nil {
		setConsoleTitleW.Call(uintptr(unsafe.Pointer(p)))
	}
}
