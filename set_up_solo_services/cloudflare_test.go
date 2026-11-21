package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const testWorkerSource = `import { DurableObject } from "cloudflare:workers";
export class SoloOutsideAlarm extends DurableObject {}
export default { fetch() { return new Response("ok"); } };
`

// cloudflareSetUp is one test's fake Cloudflare, fake mmx, and worker file.
type cloudflareSetUp struct {
	*testWorld
	fake       *fakeCloudflare
	workerFile string
}

func newCloudflareSetUp(t *testing.T) *cloudflareSetUp {
	t.Helper()
	world := newTestWorld(t)
	fake := newFakeCloudflare(t)
	world.environment.cloudflareAPIURL = fake.server.URL
	workerFile := filepath.Join(t.TempDir(), "solo_outside_alarm_worker.js")
	if err := os.WriteFile(workerFile, []byte(testWorkerSource), 0o644); err != nil {
		t.Fatal(err)
	}
	world.secretsToType = []string{fakeCloudflareToken}
	return &cloudflareSetUp{testWorld: world, fake: fake, workerFile: workerFile}
}

func (s *cloudflareSetUp) arguments(extra ...string) []string {
	arguments := []string{"cloudflare", "--repo-dir", s.repositoryDirectory, "--domain", "example.com",
		"--apps", "shop=shop.example.com:acme-shop-uploads,blog=blog.example.com",
		"--backup-bucket", "acme-solo-backups", "--worker-file", s.workerFile}
	return append(arguments, extra...)
}

func (s *cloudflareSetUp) mustRun(extra ...string) {
	s.t.Helper()
	if code := s.run(s.arguments(extra...)...); code != 0 {
		s.t.Fatalf("code %d, stderr:\n%s", code, s.stderr.String())
	}
}

func (s *cloudflareSetUp) vaultChanges() int {
	log := s.mmxArgumentsLog()
	return strings.Count(log, "| secrets set ") + strings.Count(log, "| secrets add-random ")
}

// everySecret lists each value that must never be printed: the API token,
// the tunnel token, each R2 token value, and each value in the vault.
func (s *cloudflareSetUp) everySecret() []string {
	secrets := []string{fakeCloudflareToken, fakeTunnelToken}
	for _, token := range s.fake.tokens {
		secrets = append(secrets, token.Value)
	}
	entries, _ := os.ReadDir(filepath.Join(s.stateDirectory, "vault"))
	for _, entry := range entries {
		value, _ := s.vaultValue(entry.Name())
		secrets = append(secrets, value)
	}
	return secrets
}

func sha256Hex(text string) string {
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:])
}

func TestCloudflareSetsUpEverythingSoloNeeds(t *testing.T) {
	s := newCloudflareSetUp(t)
	s.mustRun()
	fake := s.fake

	// The summary on stdout is JSON with no secret in it.
	var summary map[string]any
	if err := json.Unmarshal(s.stdout.Bytes(), &summary); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, s.stdout.String())
	}
	subdomain := fake.subdomain
	if !regexp.MustCompile(`^solo-[0-9a-f]{6}$`).MatchString(subdomain) {
		t.Fatalf("workers.dev subdomain %q", subdomain)
	}
	wantSummary := map[string]any{
		"account_id": fakeAccountID, "zone_id": fakeZoneID, "tunnel_id": fakeTunnelID,
		"r2_s3_endpoint": fakeAccountID + ".r2.cloudflarestorage.com",
		"backup_bucket":  "acme-solo-backups", "lock_days": float64(14),
		"worker_url": "https://solo-outside-alarm." + subdomain + ".workers.dev",
		"apps": []any{
			map[string]any{"name": "shop", "host": "shop.example.com", "uploads_bucket": "acme-shop-uploads"},
			map[string]any{"name": "blog", "host": "blog.example.com"},
		},
	}
	if !reflect.DeepEqual(summary, wantSummary) {
		t.Fatalf("summary\n got: %v\nwant: %v", summary, wantSummary)
	}
	s.requireNoSecret(s.everySecret()...)
	if len(s.prompts) != 1 || !strings.Contains(s.prompts[0], "Cloudflare API token") {
		t.Fatalf("prompts: %q", s.prompts)
	}

	// Tunnel and DNS.
	if value, _ := s.vaultValue("cloudflare_tunnel_token"); value != fakeTunnelToken {
		t.Errorf("tunnel token in the vault: %q", value)
	}
	wantTarget := fakeTunnelID + ".cfargotunnel.com"
	wantRecords := []fakeDNSRecord{
		{ID: "dns-1", Type: "CNAME", Name: "shop.example.com", Content: wantTarget, Proxied: true},
		{ID: "dns-2", Type: "CNAME", Name: "blog.example.com", Content: wantTarget, Proxied: true},
	}
	if !reflect.DeepEqual(fake.dnsRecords, wantRecords) {
		t.Errorf("DNS records: %+v", fake.dnsRecords)
	}

	// Cache Rule.
	if len(fake.cacheRules) != 1 {
		t.Fatalf("cache rules: %v", fake.cacheRules)
	}
	rule := fake.cacheRules[0]
	parameters, _ := rule["action_parameters"].(map[string]any)
	edgeTTL, _ := parameters["edge_ttl"].(map[string]any)
	if rule["action"] != "set_cache_settings" || rule["ref"] != cacheRuleReference || parameters["cache"] != true || edgeTTL["mode"] != "bypass_by_default" {
		t.Errorf("cache rule: %v", rule)
	}
	if expression, _ := rule["expression"].(string); expression != `(http.host in {"shop.example.com" "blog.example.com"})` {
		t.Errorf("cache rule expression: %s", expression)
	}

	// Buckets and the Bucket Lock on the backup bucket only (two prefix rules
	// locking WAL archives and backup folders while leaving pgBackRest's
	// mutable backup.info / archive.info index files writable).
	if !fake.buckets["acme-solo-backups"] || !fake.buckets["acme-shop-uploads"] || len(fake.buckets) != 2 {
		t.Errorf("buckets: %v", fake.buckets)
	}
	if len(fake.locks) != 1 {
		t.Fatalf("locks: %v", fake.locks)
	}
	var lock struct {
		Rules []struct {
			ID        string `json:"id"`
			Enabled   bool   `json:"enabled"`
			Prefix    string `json:"prefix"`
			Condition struct {
				Type          string `json:"type"`
				MaxAgeSeconds int    `json:"maxAgeSeconds"`
			} `json:"condition"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(fake.locks["acme-solo-backups"], &lock); err != nil || len(lock.Rules) != 2 {
		t.Fatalf("backup bucket lock: %s", fake.locks["acme-solo-backups"])
	}
	wantPrefixes := map[string]string{
		bucketLockWALRuleID:  bucketLockWALPrefix,
		bucketLockDataRuleID: bucketLockDataPrefix,
	}
	for _, got := range lock.Rules {
		if !got.Enabled || got.Prefix != wantPrefixes[got.ID] || got.Condition.Type != "Age" || got.Condition.MaxAgeSeconds != 14*86400 {
			t.Errorf("lock rule: %+v", got)
		}
		delete(wantPrefixes, got.ID)
	}
	if len(wantPrefixes) != 0 {
		t.Errorf("missing lock rules: %v", wantPrefixes)
	}

	// Scoped R2 tokens: one per bucket, each reaching only its own bucket,
	// and the S3 key pair in the vault derived from the token's value.
	if len(fake.tokens) != 2 {
		t.Fatalf("tokens: %+v", fake.tokens)
	}
	for _, check := range []struct{ tokenName, bucket, idKey, secretKey string }{
		{"solo-r2-backup", "acme-solo-backups", "pgbackrest_r2_access_key_id", "pgbackrest_r2_secret_access_key"},
		{"solo-r2-shop-uploads", "acme-shop-uploads", "shop_R2_ACCESS_KEY_ID", "shop_R2_SECRET_ACCESS_KEY"},
	} {
		token, found := fake.tokenNamed(check.tokenName)
		if !found {
			t.Errorf("no token named %s", check.tokenName)
			continue
		}
		var policies []struct {
			Effect           string            `json:"effect"`
			Resources        map[string]string `json:"resources"`
			PermissionGroups []map[string]string
		}
		if err := json.Unmarshal(token.Policies, &policies); err != nil || len(policies) != 1 {
			t.Errorf("%s policies: %s", check.tokenName, token.Policies)
			continue
		}
		wantResources := map[string]string{"com.cloudflare.edge.r2.bucket." + fakeAccountID + "_default_" + check.bucket: "*"}
		if policies[0].Effect != "allow" || !reflect.DeepEqual(policies[0].Resources, wantResources) {
			t.Errorf("%s resources: %+v", check.tokenName, policies[0])
		}
		var raw []struct {
			PermissionGroups []map[string]string `json:"permission_groups"`
		}
		_ = json.Unmarshal(token.Policies, &raw)
		if len(raw[0].PermissionGroups) != 1 || raw[0].PermissionGroups[0]["id"] != fakeGroupID {
			t.Errorf("%s permission groups: %+v", check.tokenName, raw[0].PermissionGroups)
		}
		if value, _ := s.vaultValue(check.idKey); value != token.ID {
			t.Errorf("%s is %q, want the token's ID %q", check.idKey, value, token.ID)
		}
		if value, _ := s.vaultValue(check.secretKey); value != sha256Hex(token.Value) {
			t.Errorf("%s is %q, want the SHA-256 of the token's value", check.secretKey, value)
		}
	}
	if _, found := s.vaultValue("blog_R2_ACCESS_KEY_ID"); found {
		t.Error("an app without an uploads bucket got R2 keys")
	}

	// The Worker.
	worker := fake.workers["solo-outside-alarm"]
	if worker == nil {
		t.Fatal("the Worker was not uploaded")
	}
	if worker.Module != testWorkerSource {
		t.Errorf("uploaded module: %q", worker.Module)
	}
	wantBindings := []any{map[string]any{"type": "durable_object_namespace", "name": "SOLO_OUTSIDE_ALARM", "class_name": "SoloOutsideAlarm"}}
	if !reflect.DeepEqual(worker.Metadata["bindings"], wantBindings) || worker.Metadata["main_module"] != "solo_outside_alarm_worker.js" {
		t.Errorf("worker metadata: %v", worker.Metadata)
	}
	wantMigrations := map[string]any{"new_tag": "v1", "new_sqlite_classes": []any{"SoloOutsideAlarm"}}
	if !reflect.DeepEqual(worker.Metadata["migrations"], wantMigrations) || worker.MigrationTag != "v1" {
		t.Errorf("worker migrations: %v", worker.Metadata["migrations"])
	}
	if !reflect.DeepEqual(worker.Schedules, []map[string]string{{"cron": "*/5 * * * *"}}) {
		t.Errorf("schedules: %v", worker.Schedules)
	}
	if worker.WorkersDev["enabled"] != true || worker.WorkersDev["previews_enabled"] != false {
		t.Errorf("workers.dev: %v", worker.WorkersDev)
	}
	alarmToken, _ := s.vaultValue("outside_alarm_check_in_token")
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(alarmToken) || worker.Secrets["SOLO_OUTSIDE_ALARM_TOKEN"] != alarmToken {
		t.Errorf("the vault's check-in token %q and the Worker's %q differ or are malformed", alarmToken, worker.Secrets["SOLO_OUTSIDE_ALARM_TOKEN"])
	}
	s.requireNoSecret(alarmToken)
}

func TestCloudflareRerunChangesNothingExceptTheWorkerUpload(t *testing.T) {
	s := newCloudflareSetUp(t)
	s.mustRun()
	alarmToken, _ := s.vaultValue("outside_alarm_check_in_token")
	changesBefore := s.vaultChanges()
	tokensBefore := append([]fakeToken(nil), s.fake.tokens...)

	s.fake.resetRequests()
	s.secretsToType = []string{fakeCloudflareToken}
	s.mustRun()

	wantWrites := []string{
		"PUT /accounts/" + fakeAccountID + "/workers/scripts/solo-outside-alarm",
		"POST /accounts/" + fakeAccountID + "/workers/scripts/solo-outside-alarm/subdomain",
		"PUT /accounts/" + fakeAccountID + "/workers/scripts/solo-outside-alarm/schedules",
	}
	if writes := s.fake.writes(); !reflect.DeepEqual(writes, wantWrites) {
		t.Errorf("a rerun wrote\n%v\nwant\n%v", writes, wantWrites)
	}
	if s.vaultChanges() != changesBefore {
		t.Errorf("a rerun changed the vault:\n%s", s.mmxArgumentsLog())
	}
	if !reflect.DeepEqual(s.fake.tokens, tokensBefore) {
		t.Error("a rerun replaced an R2 token")
	}
	worker := s.fake.workers["solo-outside-alarm"]
	if worker.Uploads != 2 || worker.Metadata["migrations"] != nil {
		t.Errorf("second upload: uploads %d, migrations %v", worker.Uploads, worker.Metadata["migrations"])
	}
	if !reflect.DeepEqual(worker.Metadata["keep_bindings"], []any{"secret_text"}) {
		t.Errorf("keep_bindings: %v", worker.Metadata["keep_bindings"])
	}
	if value, _ := s.vaultValue("outside_alarm_check_in_token"); value != alarmToken || worker.Secrets["SOLO_OUTSIDE_ALARM_TOKEN"] != alarmToken {
		t.Error("a rerun changed the check-in token")
	}
	if s.fake.subdomainPuts != 1 || len(s.fake.dnsRecords) != 2 || len(s.fake.cacheRules) != 1 {
		t.Errorf("subdomain puts %d, DNS records %d, cache rules %d", s.fake.subdomainPuts, len(s.fake.dnsRecords), len(s.fake.cacheRules))
	}
	s.requireNoSecret(s.everySecret()...)
}

func TestCloudflareReplacesAnR2TokenWhoseKeysTheVaultLost(t *testing.T) {
	s := newCloudflareSetUp(t)
	s.mustRun()
	oldToken, _ := s.fake.tokenNamed("solo-r2-backup")
	if err := os.Remove(filepath.Join(s.stateDirectory, "vault", "pgbackrest_r2_secret_access_key")); err != nil {
		t.Fatal(err)
	}

	s.secretsToType = []string{fakeCloudflareToken}
	s.mustRun()
	newToken, found := s.fake.tokenNamed("solo-r2-backup")
	if !found || newToken.ID == oldToken.ID {
		t.Fatalf("the token was not replaced: %+v", newToken)
	}
	if len(s.fake.tokens) != 2 {
		t.Errorf("tokens: %+v", s.fake.tokens)
	}
	if value, _ := s.vaultValue("pgbackrest_r2_access_key_id"); value != newToken.ID {
		t.Errorf("access key ID %q, want %q", value, newToken.ID)
	}
	if value, _ := s.vaultValue("pgbackrest_r2_secret_access_key"); value != sha256Hex(newToken.Value) {
		t.Errorf("secret access key %q", value)
	}
	s.requireNoSecret(s.everySecret()...)
}

func TestCloudflareReplacesAWorkerSecretTheVaultLost(t *testing.T) {
	s := newCloudflareSetUp(t)
	s.mustRun()
	oldToken, _ := s.vaultValue("outside_alarm_check_in_token")
	os.Remove(filepath.Join(s.stateDirectory, "vault", "outside_alarm_check_in_token"))

	s.secretsToType = []string{fakeCloudflareToken}
	s.mustRun()
	newToken, _ := s.vaultValue("outside_alarm_check_in_token")
	if newToken == "" || newToken == oldToken || s.fake.workers["solo-outside-alarm"].Secrets["SOLO_OUTSIDE_ALARM_TOKEN"] != newToken {
		t.Errorf("old token %q, new token %q, Worker's %q", oldToken, newToken, s.fake.workers["solo-outside-alarm"].Secrets["SOLO_OUTSIDE_ALARM_TOKEN"])
	}
}

func TestCloudflareFixesWhatItFindsAndKeepsThePersonsOwnRules(t *testing.T) {
	s := newCloudflareSetUp(t)
	s.fake.tunnelExists = true
	s.fake.buckets["acme-solo-backups"] = true
	s.fake.locks["acme-solo-backups"] = json.RawMessage(`{"rules":[{"id":"solo-backup-lock","enabled":true,"prefix":"","condition":{"type":"Age","maxAgeSeconds":86400}}]}`)
	s.fake.dnsRecords = []fakeDNSRecord{{ID: "old-1", Type: "CNAME", Name: "shop.example.com", Content: "other.example.net", Proxied: false}}
	s.fake.cacheRules = []map[string]any{
		{"id": "mine", "ref": "mine", "expression": `(http.host eq "docs.example.com")`, "action": "set_cache_settings", "enabled": true,
			"action_parameters": map[string]any{"cache": true}, "version": "1", "last_updated": "2026-01-01T00:00:00Z"},
		{"id": "old", "ref": cacheRuleReference, "expression": `(http.host in {"shop.example.com"})`, "action": "set_cache_settings", "enabled": true,
			"action_parameters": map[string]any{"cache": false}},
	}
	s.mustRun("--lock-days", "30")

	if len(s.fake.dnsRecords) != 2 || s.fake.dnsRecords[0].ID != "old-1" || s.fake.dnsRecords[0].Content != fakeTunnelID+".cfargotunnel.com" || !s.fake.dnsRecords[0].Proxied {
		t.Errorf("DNS records: %+v", s.fake.dnsRecords)
	}
	if len(s.fake.cacheRules) != 2 || s.fake.cacheRules[0]["ref"] != "mine" || s.fake.cacheRules[1]["ref"] != cacheRuleReference {
		t.Fatalf("cache rules: %v", s.fake.cacheRules)
	}
	if expression, _ := s.fake.cacheRules[1]["expression"].(string); !strings.Contains(expression, "blog.example.com") {
		t.Errorf("our cache rule was not updated: %v", s.fake.cacheRules[1])
	}
	if !strings.Contains(string(s.fake.locks["acme-solo-backups"]), "2592000") {
		t.Errorf("lock: %s", s.fake.locks["acme-solo-backups"])
	}
	if s.fake.count("POST /accounts/"+fakeAccountID+"/cfd_tunnel") != 0 {
		t.Error("an existing tunnel was created again")
	}
	if value, _ := s.vaultValue("cloudflare_tunnel_token"); value != fakeTunnelToken {
		t.Errorf("tunnel token in the vault: %q", value)
	}
}

func TestCloudflareRefusesToReplaceARecordOfAnotherType(t *testing.T) {
	s := newCloudflareSetUp(t)
	s.fake.dnsRecords = []fakeDNSRecord{{ID: "a-1", Type: "A", Name: "shop.example.com", Content: "203.0.113.7", Proxied: true}}
	if code := s.run(s.arguments()...); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if !strings.Contains(s.stderr.String(), "shop.example.com already has a DNS record of type A") {
		t.Errorf("stderr %q", s.stderr.String())
	}
	if s.fake.dnsRecords[0].Type != "A" {
		t.Error("the A record was replaced")
	}
}

func TestCloudflareAllowsCNAMEAtApexWithMXAndTXTRecords(t *testing.T) {
	s := newCloudflareSetUp(t)
	s.fake.dnsRecords = []fakeDNSRecord{
		{ID: "mx-1", Type: "MX", Name: "example.com", Content: "mx.fastmail.com", Proxied: false},
		{ID: "txt-1", Type: "TXT", Name: "example.com", Content: "v=spf1 include:spf.messagingengine.com ~all", Proxied: false},
	}
	if code := s.run("cloudflare", "--repo-dir", s.repositoryDirectory, "--domain", "example.com",
		"--apps", "shop=example.com", "--backup-bucket", "acme-solo-backups", "--worker-file", s.workerFile); code != 0 {
		t.Fatalf("code %d, stderr:\n%s", code, s.stderr.String())
	}
	var foundCNAME, foundMX, foundTXT bool
	for _, record := range s.fake.dnsRecords {
		if record.Name == "example.com" && record.Type == "CNAME" && record.Content == fakeTunnelID+".cfargotunnel.com" && record.Proxied {
			foundCNAME = true
		}
		if record.ID == "mx-1" {
			foundMX = true
		}
		if record.ID == "txt-1" {
			foundTXT = true
		}
	}
	if !foundCNAME || !foundMX || !foundTXT {
		t.Fatalf("expected CNAME beside intact MX and TXT records, got %+v", s.fake.dnsRecords)
	}
}

func TestCloudflareVerifiesAnAccountOwnedToken(t *testing.T) {
	s := newCloudflareSetUp(t)
	s.fake.accountToken = true
	s.mustRun()
	if s.fake.count("GET /accounts/"+fakeAccountID+"/tokens/verify") != 1 {
		t.Error("the account's token verify address was not used")
	}
}

func TestCloudflareExplainsARefusedToken(t *testing.T) {
	s := newCloudflareSetUp(t)
	s.secretsToType = []string{"a-token-cloudflare-does-not-know"}
	if code := s.run(s.arguments()...); code != 1 {
		t.Fatalf("code %d, want 1", code)
	}
	if !strings.Contains(s.stderr.String(), "does not accept this API token") {
		t.Errorf("stderr %q", s.stderr.String())
	}
	if len(s.fake.writes()) != 0 || s.vaultKeyCount() != 0 {
		t.Error("a refused token changed something")
	}
	s.requireNoSecret("a-token-cloudflare-does-not-know")
}

func TestCloudflareExplainsAMissingZone(t *testing.T) {
	s := newCloudflareSetUp(t)
	arguments := s.arguments()
	for i, argument := range arguments {
		if argument == "example.com" {
			arguments[i] = "elsewhere.com"
		}
		arguments[i] = strings.ReplaceAll(arguments[i], "example.com", "elsewhere.com")
	}
	if code := s.run(arguments...); code != 1 || !strings.Contains(s.stderr.String(), "cannot see a zone for elsewhere.com") {
		t.Fatalf("code %d, stderr %q", code, s.stderr.String())
	}
}

func TestCloudflareReadsTheTokenFromAPrivateFile(t *testing.T) {
	s := newCloudflareSetUp(t)
	tokenFile := filepath.Join(t.TempDir(), "cloudflare_token")
	if err := os.WriteFile(tokenFile, []byte(fakeCloudflareToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.secretsToType = nil
	s.mustRun("--token-file", tokenFile)
	if len(s.prompts) != 0 {
		t.Errorf("a token file was given, and the command still asked: %q", s.prompts)
	}
	s.requireNoSecret(fakeCloudflareToken)
}

func TestTokenFileMustBePrivate(t *testing.T) {
	directory := t.TempDir()
	for name, test := range map[string]struct {
		mode    os.FileMode
		content string
		want    string
	}{
		"group readable": {0o640, "token", "can be read by other users"},
		"world readable": {0o644, "token", "can be read by other users"},
		"group writable": {0o620, "token", "can be read by other users"},
		"empty":          {0o600, " \n", "is empty"},
	} {
		path := filepath.Join(directory, strings.ReplaceAll(name, " ", "_"))
		if err := os.WriteFile(path, []byte(test.content), test.mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, test.mode); err != nil {
			t.Fatal(err)
		}
		if _, err := readTokenFile(path); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error %v, want one with %q", name, err, test.want)
		}
	}
	if _, err := readTokenFile(filepath.Join(directory, "missing")); err == nil {
		t.Error("a missing token file was accepted")
	}
	if _, err := readTokenFile(directory); err == nil {
		t.Error("a directory was accepted as a token file")
	}
	private := filepath.Join(directory, "private")
	if err := os.WriteFile(private, []byte("  the-token \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if token, err := readTokenFile(private); err != nil || token != "the-token" {
		t.Errorf("token %q, error %v", token, err)
	}
}

func TestCloudflareTokenFileRefusalStopsBeforeAnyCall(t *testing.T) {
	s := newCloudflareSetUp(t)
	tokenFile := filepath.Join(t.TempDir(), "cloudflare_token")
	if err := os.WriteFile(tokenFile, []byte(fakeCloudflareToken), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(tokenFile, 0o644); err != nil {
		t.Fatal(err)
	}
	if code := s.run(s.arguments("--token-file", tokenFile)...); code != 1 || !strings.Contains(s.stderr.String(), "chmod 600") {
		t.Fatalf("code %d, stderr %q", code, s.stderr.String())
	}
	if s.fake.count("GET") != 0 {
		t.Error("the command called Cloudflare with a token from an unsafe file")
	}
	s.requireNoSecret(fakeCloudflareToken)
}

func TestCloudflareRefusesBadFlags(t *testing.T) {
	s := newCloudflareSetUp(t)
	replace := func(flag, value string) []string {
		arguments := s.arguments()
		for i, argument := range arguments {
			if argument == flag {
				arguments[i+1] = value
				return arguments
			}
		}
		return append(arguments, flag, value)
	}
	for name, test := range map[string]struct {
		arguments []string
		want      string
	}{
		"lock of 7 days":           {replace("--lock-days", "7"), "--lock-days must be at least 8"},
		"bad domain":               {replace("--domain", "not a domain"), "--domain needs a domain"},
		"host outside the domain":  {replace("--apps", "shop=shop.other.com"), "not inside --domain"},
		"no equals sign":           {replace("--apps", "shop.example.com"), "needs the form name=host"},
		"bad app name":             {replace("--apps", "Shop=shop.example.com"), "must be lowercase"},
		"duplicate app":            {replace("--apps", "a=a.example.com,a=b.example.com"), "listed twice"},
		"duplicate host":           {replace("--apps", "a=a.example.com,b=a.example.com"), "listed twice"},
		"bad bucket":               {replace("--apps", "shop=shop.example.com:UPPER"), "uploads bucket"},
		"uploads in backup bucket": {replace("--apps", "shop=shop.example.com:acme-solo-backups"), "uses the backup bucket"},
		"bad backup bucket":        {replace("--backup-bucket", "X"), "--backup-bucket"},
		"missing worker file":      {replace("--worker-file", "/does/not/exist.js"), "cannot read --worker-file"},
		"no apps":                  {replace("--apps", ""), "--apps is required"},
		"token as an argument":     {append(s.arguments(), "--token", "cf-secret"), "flag provided but not defined"},
	} {
		s.secretsToType = []string{fakeCloudflareToken}
		code := s.run(test.arguments...)
		if code == 0 || !strings.Contains(s.stderr.String(), test.want) {
			t.Errorf("%s: code %d, stderr %q, want it to mention %q", name, code, s.stderr.String(), test.want)
		}
	}
	if s.fake.count("GET") != 0 || len(s.prompts) != 0 {
		t.Error("a refused command line still reached Cloudflare or asked for the token")
	}
}

func TestParseApps(t *testing.T) {
	apps, err := parseApps("shop=shop.example.com:acme-shop-uploads, blog=example.com", "example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := []soloApp{{Name: "shop", Host: "shop.example.com", UploadsBucket: "acme-shop-uploads"}, {Name: "blog", Host: "example.com"}}
	if !reflect.DeepEqual(apps, want) {
		t.Errorf("apps: %+v", apps)
	}
}

func TestR2CredentialsFollowCloudflaresRule(t *testing.T) {
	// Cloudflare's documentation: the access key ID is the token's ID, and
	// the secret access key is the SHA-256 hash of the token's value.
	id, secret := r2Credentials(accountToken{ID: "abc", Value: "value"})
	if id != "abc" || secret != "cd42404d52ad55ccfa9aca4adc828aa5800ad9d385a0671fbcbf724118320619" {
		t.Errorf("credentials %q %q", id, secret)
	}
}
