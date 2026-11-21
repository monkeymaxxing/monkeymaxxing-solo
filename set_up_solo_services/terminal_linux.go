package main

import "syscall"

const (
	ioctlGetTermios = syscall.TCGETS
	ioctlSetTermios = syscall.TCSETS
	// tcflsh is TCFLSH, which package syscall lacks.
	tcflsh = 0x540b
)

// flushUnreadTerminalInput throws away the keys typed and not yet read
// (tcflush with TCIFLUSH, which is 0).
func flushUnreadTerminalInput(fd uintptr) {
	_ = ioctl(fd, tcflsh, 0)
}
