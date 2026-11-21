package main

import "syscall"

const (
	ioctlGetTermios = syscall.TIOCGETA
	ioctlSetTermios = syscall.TIOCSETA
)

// flushUnreadTerminalInput throws away the keys typed and not yet read
// (TIOCFLUSH with FREAD, which is 1).
func flushUnreadTerminalInput(fd uintptr) {
	flush := int32(1)
	_ = ioctl(fd, syscall.TIOCFLUSH, uintptrOf(&flush))
}
