package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSecretsListPrintsKeyNamesOnly(t *testing.T) {
	world := newTestWorld(t)
	world.storeInVault("zeta_key", "zeta-secret-value")
	world.storeInVault("alpha_key", "alpha-secret-value")
	if code := world.run("secrets", "list", "--repo-dir", world.repositoryDirectory); code != 0 {
		t.Fatalf("code %d, stderr %q", code, world.stderr.String())
	}
	if world.stdout.String() != "alpha_key\nzeta_key\n" {
		t.Fatalf("stdout is %q", world.stdout.String())
	}
	world.requireNoSecret("zeta-secret-value", "alpha-secret-value")
	if !strings.Contains(world.mmxArgumentsLog(), world.repositoryDirectory) {
		t.Fatalf("mmx did not run in the repository directory:\n%s", world.mmxArgumentsLog())
	}
}

func TestSecretsSetStoresTheTypedValueWithoutShowingIt(t *testing.T) {
	world := newTestWorld(t)
	world.secretsToType = []string{"hunter2-typed-value"}
	code := world.run("secrets", "set", "--repo-dir", world.repositoryDirectory, "--key", "stripe_api_key", "--prompt", "your Stripe API key")
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, world.stderr.String())
	}
	if value, _ := world.vaultValue("stripe_api_key"); value != "hunter2-typed-value" {
		t.Fatalf("the vault holds %q", value)
	}
	if len(world.prompts) != 1 || !strings.Contains(world.prompts[0], "your Stripe API key") {
		t.Fatalf("prompts: %q", world.prompts)
	}
	world.requireNoSecret("hunter2-typed-value")
	if !strings.Contains(world.stdout.String(), "Saved stripe_api_key") {
		t.Fatalf("stdout is %q", world.stdout.String())
	}
}

func TestSecretsSetNeedsReplaceForAnExistingKey(t *testing.T) {
	world := newTestWorld(t)
	world.storeInVault("stripe_api_key", "old-value")
	world.secretsToType = []string{"new-typed-value"}
	if code := world.run("secrets", "set", "--repo-dir", world.repositoryDirectory, "--key", "stripe_api_key"); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if !strings.Contains(world.stderr.String(), "--replace") {
		t.Fatalf("stderr %q does not tell the person about --replace", world.stderr.String())
	}
	if len(world.prompts) != 0 {
		t.Fatal("the command asked for a secret it would not save")
	}
	if value, _ := world.vaultValue("stripe_api_key"); value != "old-value" {
		t.Fatalf("the old value changed to %q", value)
	}

	if code := world.run("secrets", "set", "--repo-dir", world.repositoryDirectory, "--key", "stripe_api_key", "--replace"); code != 0 {
		t.Fatalf("replace: code %d, stderr %q", code, world.stderr.String())
	}
	if value, _ := world.vaultValue("stripe_api_key"); value != "new-typed-value" {
		t.Fatalf("the vault holds %q", value)
	}
	world.requireNoSecret("new-typed-value", "old-value")
}

func TestSecretsSetRefusesBadInput(t *testing.T) {
	world := newTestWorld(t)
	world.secretsToType = []string{"value"}
	for name, arguments := range map[string][]string{
		"no repo-dir":            {"secrets", "set", "--key", "a_key"},
		"repo-dir not a folder":  {"secrets", "set", "--repo-dir", "/does/not/exist", "--key", "a_key"},
		"no key":                 {"secrets", "set", "--repo-dir", world.repositoryDirectory},
		"key with a space":       {"secrets", "set", "--repo-dir", world.repositoryDirectory, "--key", "a key"},
		"extra argument":         {"secrets", "set", "--repo-dir", world.repositoryDirectory, "--key", "a_key", "oops"},
		"the value as an option": {"secrets", "set", "--repo-dir", world.repositoryDirectory, "--key", "a_key", "--value", "x"},
	} {
		if code := world.run(arguments...); code == 0 {
			t.Errorf("%s: code 0, want a failure", name)
		}
	}
	if world.vaultKeyCount() != 0 {
		t.Fatal("a refused command saved a key")
	}
}

func TestSecretsSetStopsWhenThereIsNoTerminal(t *testing.T) {
	world := newTestWorld(t)
	world.environment.readSecret = func(string) (string, error) { return "", errNoTerminal }
	if code := world.run("secrets", "set", "--repo-dir", world.repositoryDirectory, "--key", "a_key"); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if !strings.Contains(world.stderr.String(), "needs your own terminal") || world.vaultKeyCount() != 0 {
		t.Fatalf("stderr %q, keys %d", world.stderr.String(), world.vaultKeyCount())
	}
}

func TestSecretsGenerateMakesTheValueInsideMmx(t *testing.T) {
	world := newTestWorld(t)
	if code := world.run("secrets", "generate", "--repo-dir", world.repositoryDirectory, "--key", "session_signing_key"); code != 0 {
		t.Fatalf("code %d, stderr %q", code, world.stderr.String())
	}
	value, found := world.vaultValue("session_signing_key")
	if !found || value != "random-value-of-32" {
		t.Fatalf("the vault holds %q (found %v)", value, found)
	}
	if len(world.prompts) != 0 {
		t.Fatal("generate asked the person for a value")
	}
	world.requireNoSecret(value)

	// A second run needs --replace, and --length reaches mmx.
	if code := world.run("secrets", "generate", "--repo-dir", world.repositoryDirectory, "--key", "session_signing_key"); code != 1 {
		t.Fatalf("second run: code %d, want 1", code)
	}
	if code := world.run("secrets", "generate", "--repo-dir", world.repositoryDirectory, "--key", "session_signing_key", "--length", "48", "--replace"); code != 0 {
		t.Fatalf("replace: code %d, stderr %q", code, world.stderr.String())
	}
	if value, _ := world.vaultValue("session_signing_key"); value != "random-value-of-48" {
		t.Fatalf("the vault holds %q", value)
	}
	if code := world.run("secrets", "generate", "--repo-dir", world.repositoryDirectory, "--key", "another_key", "--length", "0"); code != 2 {
		t.Fatalf("length 0: code %d, want 2", code)
	}
}

// fakePushover answers Pushover's validate call and records the form it got.
type fakePushover struct {
	server *httptest.Server
	forms  []url.Values
}

func newFakePushover(t *testing.T, validToken, validUser string) *fakePushover {
	t.Helper()
	fake := &fakePushover{}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/1/users/validate.json" {
			t.Errorf("unexpected Pushover request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		fake.forms = append(fake.forms, form)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case form.Get("token") != validToken:
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"token":"invalid","errors":["application token is invalid, see https://pushover.net/api"],"status":0,"request":"abc"}`)
		case form.Get("user") != validUser:
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"user":"invalid","errors":["user identifier is invalid"],"status":0,"request":"abc"}`)
		default:
			io.WriteString(w, `{"status":1,"group":0,"devices":["phone"],"request":"abc"}`)
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func TestSecretsSetPushoverValidatesBeforeSaving(t *testing.T) {
	world := newTestWorld(t)
	fake := newFakePushover(t, "pushover-app-token-123", "pushover-user-key-456")
	world.environment.pushoverAPIURL = fake.server.URL
	world.secretsToType = []string{"pushover-app-token-123", "pushover-user-key-456"}
	if code := world.run("secrets", "set-pushover", "--repo-dir", world.repositoryDirectory); code != 0 {
		t.Fatalf("code %d, stderr %q", code, world.stderr.String())
	}
	if len(fake.forms) != 1 || fake.forms[0].Get("token") != "pushover-app-token-123" || fake.forms[0].Get("user") != "pushover-user-key-456" {
		t.Fatalf("Pushover received %v", fake.forms)
	}
	if value, _ := world.vaultValue("pushover_application_token"); value != "pushover-app-token-123" {
		t.Fatalf("application token in the vault: %q", value)
	}
	if value, _ := world.vaultValue("pushover_user_or_group_key"); value != "pushover-user-key-456" {
		t.Fatalf("user key in the vault: %q", value)
	}
	if len(world.prompts) != 2 {
		t.Fatalf("prompts: %q", world.prompts)
	}
	world.requireNoSecret("pushover-app-token-123", "pushover-user-key-456")
}

func TestSecretsSetPushoverSavesNothingWhenPushoverRefuses(t *testing.T) {
	for name, typed := range map[string][]string{
		"wrong application token": {"wrong-application-token", "pushover-user-key-456"},
		"wrong user key":          {"pushover-app-token-123", "wrong-user-key-000"},
	} {
		t.Run(name, func(t *testing.T) {
			world := newTestWorld(t)
			fake := newFakePushover(t, "pushover-app-token-123", "pushover-user-key-456")
			world.environment.pushoverAPIURL = fake.server.URL
			world.secretsToType = typed
			if code := world.run("secrets", "set-pushover", "--repo-dir", world.repositoryDirectory); code != 1 {
				t.Fatalf("code %d, want 1", code)
			}
			if !strings.Contains(world.stderr.String(), "invalid") || !strings.Contains(world.stderr.String(), "Nothing was saved") {
				t.Fatalf("stderr %q", world.stderr.String())
			}
			if world.vaultKeyCount() != 0 {
				t.Fatal("a refused Pushover key reached the vault")
			}
			world.requireNoSecret(typed...)
		})
	}
}

func TestSecretsSetPushoverNeedsReplaceForExistingKeys(t *testing.T) {
	world := newTestWorld(t)
	fake := newFakePushover(t, "new-token", "new-user")
	world.environment.pushoverAPIURL = fake.server.URL
	world.storeInVault("pushover_user_or_group_key", "old-user")
	world.secretsToType = []string{"new-token", "new-user"}
	if code := world.run("secrets", "set-pushover", "--repo-dir", world.repositoryDirectory); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if len(world.prompts) != 0 || len(fake.forms) != 0 {
		t.Fatal("the command asked for or sent keys it would not save")
	}
	if code := world.run("secrets", "set-pushover", "--repo-dir", world.repositoryDirectory, "--replace"); code != 0 {
		t.Fatalf("replace: code %d, stderr %q", code, world.stderr.String())
	}
	if value, _ := world.vaultValue("pushover_user_or_group_key"); value != "new-user" {
		t.Fatalf("user key in the vault: %q", value)
	}
	if value, _ := world.vaultValue("pushover_application_token"); value != "new-token" {
		t.Fatalf("application token in the vault: %q", value)
	}
}

func TestSecretsSetPushoverReportsAnUnreachablePushover(t *testing.T) {
	world := newTestWorld(t)
	closed := httptest.NewServer(http.NotFoundHandler())
	world.environment.pushoverAPIURL = closed.URL
	closed.Close()
	world.secretsToType = []string{"token-abc", "user-def"}
	if code := world.run("secrets", "set-pushover", "--repo-dir", world.repositoryDirectory); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if !strings.Contains(world.stderr.String(), "did not complete") || world.vaultKeyCount() != 0 {
		t.Fatalf("stderr %q, keys %d", world.stderr.String(), world.vaultKeyCount())
	}
	world.requireNoSecret("token-abc", "user-def")
}
