package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// vaultFileName is the vault's file in the person's systems repository.
const vaultFileName = "secrets_vault.age"

// vault is the systems repository's encrypted secrets vault. This command
// only lists its key names and adds values. It never reads a value back, and
// it never writes the vault file itself: `mmx secrets` owns the vault's format.
type vault interface {
	// keyNames lists the names of the stored secrets, never their values.
	keyNames() (map[string]bool, error)
	// set stores one value. replace says an old value of the key is replaced.
	set(key, value string, replace bool) error
	// addRandom makes a random value of length characters inside mmx.
	addRandom(key string, length int, replace bool) error
}

// mmxVault runs `mmx secrets` in the systems repository. Each value goes to
// mmx on standard input, never in arguments, so no value shows in the
// computer's process list.
type mmxVault struct {
	repositoryDirectory string
}

func (v mmxVault) run(standardInput string, arguments ...string) (string, string, error) {
	command := exec.Command("mmx", append([]string{"secrets"}, arguments...)...)
	command.Dir = v.repositoryDirectory
	if standardInput != "" {
		command.Stdin = strings.NewReader(standardInput)
	}
	var output, errorOutput bytes.Buffer
	command.Stdout, command.Stderr = &output, &errorOutput
	err := command.Run()
	if errors.Is(err, exec.ErrNotFound) {
		err = errors.New("mmx is not installed, or it is not on your PATH")
	}
	return output.String(), errorOutput.String(), err
}

func (v mmxVault) keyNames() (map[string]bool, error) {
	output, errorOutput, err := v.run("", "list-keys", "--json")
	if err != nil {
		return nil, fmt.Errorf("mmx secrets list-keys failed: %s", firstLine(errorOutput, err))
	}
	var listing struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal([]byte(output), &listing); err != nil {
		return nil, fmt.Errorf("mmx secrets list-keys printed something this command cannot read: %v", err)
	}
	names := map[string]bool{}
	for _, key := range listing.Keys {
		names[key] = true
	}
	return names, nil
}

func (v mmxVault) set(key, value string, replace bool) error {
	arguments := []string{"set", key}
	if replace {
		arguments = append(arguments, "--replace")
	}
	// mmx never prints a value, also not in an error message, so its
	// message is safe to show.
	_, errorOutput, err := v.run(value, arguments...)
	if err != nil {
		return fmt.Errorf("mmx secrets set %s failed: %s", key, firstLine(errorOutput, err))
	}
	return nil
}

func (v mmxVault) addRandom(key string, length int, replace bool) error {
	arguments := []string{"add-random", key, "--length", fmt.Sprint(length)}
	if replace {
		arguments = append(arguments, "--replace")
	}
	_, errorOutput, err := v.run("", arguments...)
	if err != nil {
		return fmt.Errorf("mmx secrets add-random %s failed: %s", key, firstLine(errorOutput, err))
	}
	return nil
}

func firstLine(text string, fallback error) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return fallback.Error()
	}
	return strings.SplitN(text, "\n", 2)[0]
}

// saveSecret stores a value, replacing the key's old value only when the
// person allowed it. It tells the person when the key already exists.
func saveSecret(store vault, existing map[string]bool, key, value string, replace bool) error {
	if existing[key] && !replace {
		return fmt.Errorf("the vault already holds %s. Add --replace to enter a new value for it", key)
	}
	if err := store.set(key, value, existing[key]); err != nil {
		return err
	}
	existing[key] = true
	return nil
}
