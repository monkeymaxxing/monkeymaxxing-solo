package main

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeMmxScript stands in for `mmx secrets`. It keeps each value in a file
// named after its key, so a test can read what reached the vault, and it
// logs the directory and every argument, so a test can prove that no secret
// was passed as an argument. It finds its state next to itself.
const fakeMmxScript = `#!/bin/sh
state="$(dirname "$0")/state"
mkdir -p "$state/vault"
printf '%s | %s\n' "$(pwd)" "$*" >> "$state/arguments.log"
[ "$1" = secrets ] || { echo "fake mmx: unknown command" >&2; exit 2; }
shift
command="$1"
case "$command" in
list-keys)
  first=1
  printf '{"keys":['
  for file in "$state"/vault/*; do
    [ -e "$file" ] || continue
    [ "$first" = 1 ] || printf ','
    first=0
    printf '"%s"' "$(basename "$file")"
  done
  printf ']}\n'
  ;;
set)
  key="$2"
  if [ -e "$state/vault/$key" ] && [ "$3" != "--replace" ]; then
    echo "the key $key already exists" >&2
    exit 1
  fi
  cat > "$state/vault/$key"
  ;;
add-random)
  key="$2"
  length="$4"
  if [ -e "$state/vault/$key" ] && [ "$5" != "--replace" ]; then
    echo "the key $key already exists" >&2
    exit 1
  fi
  printf 'random-value-of-%s' "$length" > "$state/vault/$key"
  ;;
*)
  echo "fake mmx: unknown subcommand" >&2
  exit 2
  ;;
esac
`

// testWorld is one test's fake mmx, systems repository directory, and
// command environment.
type testWorld struct {
	t                   *testing.T
	stateDirectory      string
	repositoryDirectory string
	stdout, stderr      bytes.Buffer
	secretsToType       []string
	prompts             []string
	environment         *environment
}

func newTestWorld(t *testing.T) *testWorld {
	t.Helper()
	binDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDirectory, "mmx"), []byte(fakeMmxScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDirectory+string(os.PathListSeparator)+os.Getenv("PATH"))
	world := &testWorld{t: t, stateDirectory: filepath.Join(binDirectory, "state"), repositoryDirectory: t.TempDir()}
	world.environment = &environment{
		stdout: &world.stdout,
		stderr: &world.stderr,
		readSecret: func(label string) (string, error) {
			world.prompts = append(world.prompts, label)
			if len(world.secretsToType) == 0 {
				return "", errors.New("the test typed nothing")
			}
			value := world.secretsToType[0]
			world.secretsToType = world.secretsToType[1:]
			return value, nil
		},
		openVault:  func(directory string) vault { return mmxVault{repositoryDirectory: directory} },
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	return world
}

// run runs the command line and returns its exit code.
func (w *testWorld) run(arguments ...string) int {
	w.stdout.Reset()
	w.stderr.Reset()
	return run(w.environment, arguments)
}

func (w *testWorld) vaultValue(key string) (string, bool) {
	content, err := os.ReadFile(filepath.Join(w.stateDirectory, "vault", key))
	if err != nil {
		return "", false
	}
	return string(content), true
}

func (w *testWorld) storeInVault(key, value string) {
	w.t.Helper()
	directory := filepath.Join(w.stateDirectory, "vault")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, key), []byte(value), 0o644); err != nil {
		w.t.Fatal(err)
	}
}

func (w *testWorld) vaultKeyCount() int {
	entries, _ := os.ReadDir(filepath.Join(w.stateDirectory, "vault"))
	return len(entries)
}

func (w *testWorld) mmxArgumentsLog() string {
	content, _ := os.ReadFile(filepath.Join(w.stateDirectory, "arguments.log"))
	return string(content)
}

// requireNoSecret fails when any secret shows in what the command printed,
// in what it asked at the prompts, or in the arguments it gave mmx.
func (w *testWorld) requireNoSecret(secrets ...string) {
	w.t.Helper()
	for _, secret := range secrets {
		if secret == "" {
			w.t.Fatal("a test passed an empty secret")
		}
		for name, text := range map[string]string{"stdout": w.stdout.String(), "stderr": w.stderr.String(), "mmx arguments": w.mmxArgumentsLog(), "prompts": strings.Join(w.prompts, "\n")} {
			if strings.Contains(text, secret) {
				w.t.Errorf("the secret %q appears in %s:\n%s", secret, name, text)
			}
		}
	}
}

func TestVersionAndHelp(t *testing.T) {
	world := newTestWorld(t)
	if code := world.run("--version"); code != 0 || !strings.Contains(world.stdout.String(), "set-up-solo-services") {
		t.Fatalf("--version: code %d, stdout %q", code, world.stdout.String())
	}
	if code := world.run("--help"); code != 0 || !strings.Contains(world.stdout.String(), "secrets set-pushover") {
		t.Fatalf("--help: code %d, stdout %q", code, world.stdout.String())
	}
	if code := world.run(); code != 2 {
		t.Fatalf("no command: code %d, want 2", code)
	}
	if code := world.run("frobnicate"); code != 2 || !strings.Contains(world.stderr.String(), "not a command") {
		t.Fatalf("unknown command: code %d, stderr %q", code, world.stderr.String())
	}
	if code := world.run("secrets", "frobnicate"); code != 2 {
		t.Fatalf("unknown secrets subcommand: code %d, want 2", code)
	}
}

// The real command must talk to the real services and nowhere else.
func TestProductionEnvironmentUsesTheRealServiceAddresses(t *testing.T) {
	e := newEnvironment()
	if e.cloudflareAPIURL != "https://api.cloudflare.com/client/v4" || e.pushoverAPIURL != "https://api.pushover.net" {
		t.Fatalf("production addresses are %q and %q", e.cloudflareAPIURL, e.pushoverAPIURL)
	}
	if e.httpClient.Transport != nil {
		t.Fatal("production must use the default transport, which uses the system's certificate authorities")
	}
}
