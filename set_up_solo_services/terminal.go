package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"unsafe"
)

// The hidden prompt turns the terminal's echo off with the standard
// library's ioctl, so the command needs no dependency for it. The terminal
// constants differ by system: terminal_darwin.go and terminal_linux.go.
//
// A secret is only ever read from the person's own terminal, /dev/tty, and
// never from standard input. Reason: standard input can be a pipe that a
// coding agent controls or a script that logs it, while /dev/tty is the
// keyboard of the person running this command.

var errNoTerminal = errors.New("this command asks for secrets at a hidden prompt, so it needs your own terminal. Run it yourself in a terminal window, not through a pipe or your coding agent")

// errEmptyValue is Enter at a hidden prompt with nothing typed.
var errEmptyValue = errors.New("nothing was entered")

func ioctl(fd, request, argument uintptr) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, argument); errno != 0 {
		return errno
	}
	return nil
}

func uintptrOf[T any](pointer *T) uintptr { return uintptr(unsafe.Pointer(pointer)) }

func getTermios(fd uintptr) (*syscall.Termios, error) {
	var state syscall.Termios
	if err := ioctl(fd, ioctlGetTermios, uintptrOf(&state)); err != nil {
		return nil, err
	}
	return &state, nil
}

func setTermios(fd uintptr, state *syscall.Termios) error {
	return ioctl(fd, ioctlSetTermios, uintptrOf(state))
}

// readHiddenSecret asks for one secret on the person's terminal.
func readHiddenSecret(label string) (string, error) {
	terminal, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", errNoTerminal
	}
	defer terminal.Close()
	return readHiddenLineFrom(terminal, label)
}

// readHiddenLineFrom writes label to terminal, reads one line from it with
// echo off, and turns echo back on. It refuses a file that is not a
// terminal, so a secret never comes from a pipe or a file.
func readHiddenLineFrom(terminal *os.File, label string) (string, error) {
	fd := terminal.Fd()
	saved, err := getTermios(fd)
	if err != nil {
		return "", errNoTerminal
	}
	hidden := *saved
	hidden.Lflag &^= syscall.ECHO
	hidden.Lflag |= syscall.ICANON | syscall.ISIG
	// Echo goes off before the label shows, so a value pasted as soon as the
	// label appears never shows on the screen.
	if err := setTermios(fd, &hidden); err != nil {
		return "", err
	}
	// A Ctrl-C ends the command, so it must give the terminal its echo back.
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt, syscall.SIGTERM)
	stopWatching := make(chan struct{})
	go func() {
		select {
		case <-interrupted:
			_ = setTermios(fd, saved)
			fmt.Fprintln(terminal)
			os.Exit(130)
		case <-stopWatching:
		}
	}()
	defer func() {
		signal.Stop(interrupted)
		close(stopWatching)
	}()

	// Keys typed before the question appeared must not answer it.
	flushUnreadTerminalInput(fd)
	fmt.Fprint(terminal, label)
	line, readErr := bufio.NewReader(terminal).ReadString('\n')
	_ = setTermios(fd, saved)
	fmt.Fprintln(terminal)
	if readErr != nil && (readErr != io.EOF || line == "") {
		return "", errors.New("no value was entered")
	}
	value := strings.TrimSpace(line)
	if value == "" {
		return "", errEmptyValue
	}
	return value, nil
}
