package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// syncBuffer collects what the pseudo-terminal's master receives, which is
// everything the command shows on the person's screen.
type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if condition() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func echoIsOn(t *testing.T, terminal *os.File) bool {
	t.Helper()
	state, err := getTermios(terminal.Fd())
	if err != nil {
		t.Fatal(err)
	}
	return state.Lflag&syscall.ECHO != 0
}

func TestHiddenPromptTurnsEchoOffWhileTheSecretIsTypedAndBackOnAfter(t *testing.T) {
	master, slave := openPseudoTerminal(t)
	screen := &syncBuffer{}
	go func() {
		buffer := make([]byte, 4096)
		for {
			count, err := master.Read(buffer)
			if count > 0 {
				screen.Write(buffer[:count])
			}
			if err != nil {
				return
			}
		}
	}()
	if !echoIsOn(t, slave) {
		t.Fatal("a new terminal should echo")
	}

	type answer struct {
		value string
		err   error
	}
	answered := make(chan answer, 1)
	go func() {
		value, err := readHiddenLineFrom(slave, "Secret: ")
		answered <- answer{value, err}
	}()

	waitFor(t, "the prompt label", func() bool { return strings.Contains(screen.String(), "Secret: ") })
	if echoIsOn(t, slave) {
		t.Fatal("echo is still on while the prompt waits for the secret")
	}
	if _, err := master.WriteString("  pasted-secret-value  \n"); err != nil {
		t.Fatal(err)
	}
	var got answer
	select {
	case got = <-answered:
	case <-time.After(5 * time.Second):
		t.Fatal("the prompt never returned")
	}
	if got.err != nil || got.value != "pasted-secret-value" {
		t.Fatalf("value %q, error %v", got.value, got.err)
	}
	if !echoIsOn(t, slave) {
		t.Fatal("echo stayed off after the prompt")
	}
	waitFor(t, "the line break after the prompt", func() bool { return strings.HasSuffix(screen.String(), "\n") })
	if strings.Contains(screen.String(), "pasted-secret") {
		t.Fatalf("the secret was shown on the screen: %q", screen.String())
	}
}

func TestHiddenPromptRefusesAnEmptyAnswer(t *testing.T) {
	master, slave := openPseudoTerminal(t)
	go func() {
		buffer := make([]byte, 4096)
		for {
			if _, err := master.Read(buffer); err != nil {
				return
			}
		}
	}()
	result := make(chan error, 1)
	go func() {
		_, err := readHiddenLineFrom(slave, "Secret: ")
		result <- err
	}()
	waitFor(t, "echo off", func() bool { return !echoIsOn(t, slave) })
	master.WriteString("   \n")
	select {
	case err := <-result:
		if err != errEmptyValue {
			t.Fatalf("error %v, want %v", err, errEmptyValue)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the prompt never returned")
	}
	if !echoIsOn(t, slave) {
		t.Fatal("echo stayed off after an empty answer")
	}
}

// A secret never comes from a file or a pipe, which a coding agent could
// have written or could read.
func TestHiddenPromptRefusesAFileThatIsNotATerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "typed-ahead")
	if err := os.WriteFile(path, []byte("secret-from-a-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if value, err := readHiddenLineFrom(file, "Secret: "); err != errNoTerminal || value != "" {
		t.Fatalf("value %q, error %v", value, err)
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	writer.WriteString("secret-from-a-pipe\n")
	if value, err := readHiddenLineFrom(reader, "Secret: "); err != errNoTerminal || value != "" {
		t.Fatalf("value %q, error %v", value, err)
	}
}
