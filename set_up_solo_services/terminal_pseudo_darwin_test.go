package main

import (
	"os"
	"syscall"
	"testing"
)

// openPseudoTerminal makes a pseudo-terminal pair: the test types into the
// master, and the command under test reads from the slave as if it were the
// person's terminal.
func openPseudoTerminal(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("cannot open a pseudo-terminal: %v", err)
	}
	if err := ioctl(master.Fd(), syscall.TIOCPTYGRANT, 0); err != nil {
		t.Fatal(err)
	}
	if err := ioctl(master.Fd(), syscall.TIOCPTYUNLK, 0); err != nil {
		t.Fatal(err)
	}
	var name [128]byte
	if err := ioctl(master.Fd(), syscall.TIOCPTYGNAME, uintptrOf(&name[0])); err != nil {
		t.Fatal(err)
	}
	length := 0
	for length < len(name) && name[length] != 0 {
		length++
	}
	slave, err = os.OpenFile(string(name[:length]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { slave.Close(); master.Close() })
	return master, slave
}
