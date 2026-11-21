package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const (
	pushoverApplicationTokenKey = "pushover_application_token"
	pushoverUserKeyKey          = "pushover_user_or_group_key"
)

func runSecrets(e *environment, arguments []string) error {
	if len(arguments) == 0 {
		return usageError("secrets needs a subcommand: list, set, generate, or set-pushover")
	}
	switch arguments[0] {
	case "list":
		return runSecretsList(e, arguments[1:])
	case "set":
		return runSecretsSet(e, arguments[1:])
	case "generate":
		return runSecretsGenerate(e, arguments[1:])
	case "set-pushover":
		return runSecretsSetPushover(e, arguments[1:])
	}
	return usageError(fmt.Sprintf("%q is not a secrets subcommand. Use list, set, generate, or set-pushover", arguments[0]))
}

func runSecretsList(e *environment, arguments []string) error {
	flags := e.newFlagSet("secrets list")
	repositoryDirectory := flags.String("repo-dir", "", "your systems repository directory")
	if err := parseFlags(flags, arguments); err != nil {
		return err
	}
	if err := requireDirectory("repo-dir", *repositoryDirectory); err != nil {
		return err
	}
	names, err := e.openVault(*repositoryDirectory).keyNames()
	if err != nil {
		return err
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	for _, name := range sorted {
		fmt.Fprintln(e.stdout, name)
	}
	return nil
}

func runSecretsSet(e *environment, arguments []string) error {
	flags := e.newFlagSet("secrets set")
	repositoryDirectory := flags.String("repo-dir", "", "your systems repository directory")
	key := flags.String("key", "", "the name to store the secret under")
	label := flags.String("prompt", "", "what to call the secret at the prompt")
	replace := flags.Bool("replace", false, "replace the secret when the vault already holds it")
	if err := parseFlags(flags, arguments); err != nil {
		return err
	}
	if err := requireDirectory("repo-dir", *repositoryDirectory); err != nil {
		return err
	}
	if !keyNamePattern.MatchString(*key) {
		return usageError("--key needs a name of letters, digits, and underscores that starts with a letter")
	}
	store := e.openVault(*repositoryDirectory)
	existing, err := store.keyNames()
	if err != nil {
		return err
	}
	if existing[*key] && !*replace {
		return fmt.Errorf("the vault already holds %s. Add --replace to enter a new value for it", *key)
	}
	name := *label
	if name == "" {
		name = *key
	}
	value, err := e.readSecret("Enter " + name + " (typing is hidden): ")
	if err != nil {
		return err
	}
	if err := saveSecret(store, existing, *key, value, *replace); err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "Saved %s in %s.\n", *key, vaultFileName)
	return nil
}

func runSecretsGenerate(e *environment, arguments []string) error {
	flags := e.newFlagSet("secrets generate")
	repositoryDirectory := flags.String("repo-dir", "", "your systems repository directory")
	key := flags.String("key", "", "the name to store the secret under")
	length := flags.Int("length", 32, "how many characters the secret has")
	replace := flags.Bool("replace", false, "replace the secret when the vault already holds it")
	if err := parseFlags(flags, arguments); err != nil {
		return err
	}
	if err := requireDirectory("repo-dir", *repositoryDirectory); err != nil {
		return err
	}
	if !keyNamePattern.MatchString(*key) {
		return usageError("--key needs a name of letters, digits, and underscores that starts with a letter")
	}
	if *length < 1 {
		return usageError("--length must be at least 1")
	}
	store := e.openVault(*repositoryDirectory)
	existing, err := store.keyNames()
	if err != nil {
		return err
	}
	if existing[*key] && !*replace {
		return fmt.Errorf("the vault already holds %s. Add --replace to make a new value for it", *key)
	}
	// mmx makes the value itself, so it never exists in this process.
	if err := store.addRandom(*key, *length, existing[*key]); err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "Made a random %d-character secret and saved it as %s in %s. It was not printed.\n", *length, *key, vaultFileName)
	return nil
}

func runSecretsSetPushover(e *environment, arguments []string) error {
	flags := e.newFlagSet("secrets set-pushover")
	repositoryDirectory := flags.String("repo-dir", "", "your systems repository directory")
	replace := flags.Bool("replace", false, "replace the Pushover keys when the vault already holds them")
	if err := parseFlags(flags, arguments); err != nil {
		return err
	}
	if err := requireDirectory("repo-dir", *repositoryDirectory); err != nil {
		return err
	}
	store := e.openVault(*repositoryDirectory)
	existing, err := store.keyNames()
	if err != nil {
		return err
	}
	for _, key := range []string{pushoverApplicationTokenKey, pushoverUserKeyKey} {
		if existing[key] && !*replace {
			return fmt.Errorf("the vault already holds %s. Add --replace to enter new Pushover keys", key)
		}
	}
	applicationToken, err := e.readSecret("Enter your Pushover application API token (typing is hidden): ")
	if err != nil {
		return err
	}
	userKey, err := e.readSecret("Enter your Pushover user key (typing is hidden): ")
	if err != nil {
		return err
	}
	// Check both with Pushover before saving, so a typo never reaches the vault.
	if err := e.validatePushover(applicationToken, userKey); err != nil {
		return err
	}
	if err := saveSecret(store, existing, pushoverApplicationTokenKey, applicationToken, *replace); err != nil {
		return err
	}
	if err := saveSecret(store, existing, pushoverUserKeyKey, userKey, *replace); err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "Pushover accepted both keys. Saved %s and %s in %s.\n", pushoverApplicationTokenKey, pushoverUserKeyKey, vaultFileName)
	return nil
}

// validatePushover asks Pushover whether the application token and the user
// key work together.
// https://pushover.net/api#validate
func (e *environment) validatePushover(applicationToken, userKey string) error {
	form := url.Values{"token": {applicationToken}, "user": {userKey}}
	response, err := e.httpClient.PostForm(e.pushoverAPIURL+"/1/users/validate.json", form)
	if err != nil {
		return fmt.Errorf("the request to Pushover did not complete: %v", stripURLFromError(err))
	}
	defer response.Body.Close()
	var answer struct {
		Status int      `json:"status"`
		Errors []string `json:"errors"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&answer)
	if response.StatusCode == http.StatusOK && decodeErr == nil && answer.Status == 1 {
		return nil
	}
	if decodeErr == nil && len(answer.Errors) > 0 {
		// Pushover's messages name the problem ("user identifier is invalid")
		// and never repeat a key.
		return fmt.Errorf("Pushover did not accept the keys: %s. Nothing was saved", strings.Join(answer.Errors, "; "))
	}
	return fmt.Errorf("Pushover did not accept the keys (HTTP %d). Nothing was saved", response.StatusCode)
}

// stripURLFromError keeps Go's url.Error from repeating the request's
// address. A short message reads better.
func stripURLFromError(err error) error {
	var urlError *url.Error
	if errors.As(err, &urlError) && urlError.Err != nil {
		return urlError.Err
	}
	return err
}
