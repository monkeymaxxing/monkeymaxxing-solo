// Command set-up-solo-services keeps a Monkeymaxxing Solo person's secrets
// out of their coding agent's conversation. Its `secrets` command asks for
// each secret at a hidden prompt on the person's own terminal and stores it
// in the systems repository's encrypted vault. Its `cloudflare` command sets
// up everything Solo needs at Cloudflare.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"
)

// version is overridden at release build time via -ldflags "-X main.version=...".
var version = "0.1.0"

// The real service addresses. They are fixed, and no flag or environment
// variable changes them: every run talks to the real services over the
// operating system's certificate authorities. Only the unit tests build an
// environment with other addresses.
const (
	cloudflareAPIURL = "https://api.cloudflare.com/client/v4"
	pushoverAPIURL   = "https://api.pushover.net"
)

// environment holds everything a command touches outside its own process,
// so the unit tests can replace it.
type environment struct {
	stdout, stderr io.Writer
	// readSecret asks for one secret at a hidden prompt.
	readSecret func(label string) (string, error)
	// openVault opens the vault of a systems repository directory.
	openVault func(repositoryDirectory string) vault
	// httpClient uses the system's certificate authorities, and a time
	// limit so a stalled request fails with a message instead of waiting
	// forever.
	httpClient       *http.Client
	cloudflareAPIURL string
	pushoverAPIURL   string
}

func newEnvironment() *environment {
	return &environment{
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		readSecret: readHiddenSecret,
		openVault: func(repositoryDirectory string) vault {
			return mmxVault{repositoryDirectory: repositoryDirectory}
		},
		httpClient:       &http.Client{Timeout: 60 * time.Second},
		cloudflareAPIURL: cloudflareAPIURL,
		pushoverAPIURL:   pushoverAPIURL,
	}
}

func main() {
	os.Exit(run(newEnvironment(), os.Args[1:]))
}

const usage = `set-up-solo-services keeps your secrets out of your coding agent's conversation.

Usage:
  set-up-solo-services secrets list          --repo-dir DIR
  set-up-solo-services secrets set           --repo-dir DIR --key NAME [--prompt LABEL] [--replace]
  set-up-solo-services secrets generate      --repo-dir DIR --key NAME [--length 32] [--replace]
  set-up-solo-services secrets set-pushover  --repo-dir DIR [--replace]
  set-up-solo-services cloudflare            --repo-dir DIR --domain DOMAIN --apps NAME=HOST[:UPLOADS_BUCKET],...
                                             --backup-bucket NAME [--lock-days 14] [--tunnel-name solo01]
                                             [--worker-name solo-outside-alarm] --worker-file PATH [--token-file PATH]
  set-up-solo-services --version

Secrets are typed at a hidden prompt on your own terminal and go straight into
secrets_vault.age through "mmx secrets". They are never printed and never
passed as command arguments.
`

func run(e *environment, arguments []string) int {
	if len(arguments) == 0 {
		fmt.Fprint(e.stderr, usage)
		return 2
	}
	var err error
	switch arguments[0] {
	case "--version", "version":
		fmt.Fprintln(e.stdout, "set-up-solo-services "+version)
		return 0
	case "--help", "-h", "help":
		fmt.Fprint(e.stdout, usage)
		return 0
	case "secrets":
		err = runSecrets(e, arguments[1:])
	case "cloudflare":
		err = runCloudflare(e, arguments[1:])
	default:
		fmt.Fprintf(e.stderr, "set-up-solo-services: %q is not a command.\n\n%s", arguments[0], usage)
		return 2
	}
	switch err {
	case nil:
		return 0
	case flag.ErrHelp:
		return 0
	}
	fmt.Fprintf(e.stderr, "set-up-solo-services: %v\n", err)
	if _, isUsage := err.(usageError); isUsage {
		return 2
	}
	return 1
}

// usageError is a mistake in the command line, as opposed to a failed run.
type usageError string

func (u usageError) Error() string { return string(u) }

// newFlagSet makes a flag set that reports its mistakes as usage errors.
func (e *environment) newFlagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(e.stderr)
	return flags
}

func parseFlags(flags *flag.FlagSet, arguments []string) error {
	if err := flags.Parse(arguments); err != nil {
		if err == flag.ErrHelp {
			return err
		}
		return usageError(err.Error())
	}
	if flags.NArg() > 0 {
		return usageError(fmt.Sprintf("unexpected argument %q", flags.Arg(0)))
	}
	return nil
}

func requireDirectory(flagName, path string) error {
	if path == "" {
		return usageError("--" + flagName + " is required")
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("--%s %s is not a directory", flagName, path)
	}
	return nil
}

// keyNamePattern is what a vault key looks like: letters, digits, and
// underscores, starting with a letter, so it also works as an environment
// variable name.
var keyNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
