package main

import (
	"fmt"
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
	var unlock int32
	if err := ioctl(master.Fd(), syscall.TIOCSPTLCK, uintptrOf(&unlock)); err != nil {
		t.Fatal(err)
	}
	var number uint32
	if err := ioctl(master.Fd(), syscall.TIOCGPTN, uintptrOf(&number)); err != nil {
		t.Fatal(err)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { slave.Close(); master.Close() })
	return master, slave
}
