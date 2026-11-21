package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// The vault keys the cloudflare command saves.
const (
	tunnelTokenKey       = "cloudflare_tunnel_token"
	backupAccessKeyIDKey = "pgbackrest_r2_access_key_id"
	backupSecretKey      = "pgbackrest_r2_secret_access_key"
	alarmTokenKey        = "outside_alarm_check_in_token"
)

// minimumLockDays keeps a week of backups, plus a day, inside the lock.
const minimumLockDays = 8

var (
	domainPattern     = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+[a-z]{2,}$`)
	appNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	bucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)
	resourceName      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$`)
)

type soloApp struct {
	Name          string `json:"name"`
	Host          string `json:"host"`
	UploadsBucket string `json:"uploads_bucket,omitempty"`
}

// parseApps reads --apps: name=host[:uploads-bucket], comma separated.
func parseApps(text, domain string) ([]soloApp, error) {
	var apps []soloApp
	names, hosts := map[string]bool{}, map[string]bool{}
	for _, item := range strings.Split(text, ",") {
		name, rest, found := strings.Cut(strings.TrimSpace(item), "=")
		if !found || name == "" || rest == "" {
			return nil, usageError(fmt.Sprintf("--apps item %q needs the form name=host or name=host:uploads-bucket", item))
		}
		host, bucket, _ := strings.Cut(rest, ":")
		switch {
		case !appNamePattern.MatchString(name):
			return nil, usageError(fmt.Sprintf("app name %q must be lowercase letters, digits, and underscores, starting with a letter", name))
		case host != domain && !strings.HasSuffix(host, "."+domain):
			return nil, usageError(fmt.Sprintf("host %q of app %s is not inside --domain %s", host, name, domain))
		case !domainPattern.MatchString(host):
			return nil, usageError(fmt.Sprintf("host %q of app %s is not a valid host name", host, name))
		case bucket != "" && !bucketNamePattern.MatchString(bucket):
			return nil, usageError(fmt.Sprintf("uploads bucket %q of app %s must be 3 to 63 lowercase letters, digits, and hyphens", bucket, name))
		case names[name]:
			return nil, usageError(fmt.Sprintf("app name %s is listed twice", name))
		case hosts[host]:
			return nil, usageError(fmt.Sprintf("host %s is listed twice", host))
		}
		names[name], hosts[host] = true, true
		apps = append(apps, soloApp{Name: name, Host: host, UploadsBucket: bucket})
	}
	return apps, nil
}

type cloudflareSummary struct {
	AccountID    string    `json:"account_id"`
	ZoneID       string    `json:"zone_id"`
	TunnelID     string    `json:"tunnel_id"`
	R2S3Endpoint string    `json:"r2_s3_endpoint"`
	BackupBucket string    `json:"backup_bucket"`
	LockDays     int       `json:"lock_days"`
	WorkerURL    string    `json:"worker_url"`
	Apps         []soloApp `json:"apps"`
}

func (e *environment) say(format string, arguments ...any) {
	fmt.Fprintf(e.stderr, "  "+format+"\n", arguments...)
}

func randomHex(byteCount int) (string, error) {
	random := make([]byte, byteCount)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}

// readTokenFile reads a Cloudflare API token from a file that only its owner
// can read. A token in a file others can read is a token others can use.
func readTokenFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot read --token-file %s: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("--token-file %s is not a regular file", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("--token-file %s can be read by other users (mode %04o). Run: chmod 600 %s", path, info.Mode().Perm(), path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("cannot read --token-file %s: %v", path, err)
	}
	token := strings.TrimSpace(string(content))
	if token == "" {
		return "", fmt.Errorf("--token-file %s is empty", path)
	}
	return token, nil
}

func runCloudflare(e *environment, arguments []string) error {
	flags := e.newFlagSet("cloudflare")
	repositoryDirectory := flags.String("repo-dir", "", "your systems repository directory")
	domain := flags.String("domain", "", "your domain, such as example.com")
	appsText := flags.String("apps", "", "apps as name=host[:uploads-bucket], comma separated")
	backupBucket := flags.String("backup-bucket", "", "the R2 bucket for locked backups")
	lockDays := flags.Int("lock-days", 14, "days a backup object stays locked (at least 8)")
	tunnelName := flags.String("tunnel-name", "solo01", "the Cloudflare Tunnel's name")
	workerName := flags.String("worker-name", "solo-outside-alarm", "the outside alarm Worker's name")
	workerFile := flags.String("worker-file", "", "path to solo_outside_alarm_worker.js")
	tokenFile := flags.String("token-file", "", "a file (mode 0600) that holds the Cloudflare API token, instead of the hidden prompt")
	if err := parseFlags(flags, arguments); err != nil {
		return err
	}
	if err := requireDirectory("repo-dir", *repositoryDirectory); err != nil {
		return err
	}
	switch {
	case !domainPattern.MatchString(*domain):
		return usageError("--domain needs a domain such as example.com")
	case *appsText == "":
		return usageError("--apps is required, such as shop=shop.example.com:acme-shop-uploads")
	case !bucketNamePattern.MatchString(*backupBucket):
		return usageError("--backup-bucket needs 3 to 63 lowercase letters, digits, and hyphens")
	case *lockDays < minimumLockDays:
		return usageError(fmt.Sprintf("--lock-days must be at least %d, so a week of backups and a day to spare stay locked", minimumLockDays))
	case !resourceName.MatchString(*tunnelName):
		return usageError("--tunnel-name needs letters, digits, hyphens, and underscores")
	case !resourceName.MatchString(*workerName):
		return usageError("--worker-name needs letters, digits, hyphens, and underscores")
	case *workerFile == "":
		return usageError("--worker-file is required: the path to solo_outside_alarm_worker.js")
	}
	apps, err := parseApps(*appsText, *domain)
	if err != nil {
		return err
	}
	for _, app := range apps {
		if app.UploadsBucket == *backupBucket {
			return usageError(fmt.Sprintf("app %s uses the backup bucket for uploads. Give it its own uploads bucket", app.Name))
		}
	}
	source, err := os.ReadFile(*workerFile)
	if err != nil {
		return fmt.Errorf("cannot read --worker-file: %v", err)
	}

	var token string
	if *tokenFile != "" {
		token, err = readTokenFile(*tokenFile)
	} else {
		token, err = e.readSecret("Enter your Cloudflare API token (typing is hidden): ")
	}
	if err != nil {
		return err
	}
	if strings.ContainsAny(token, " \t\r\n\"'") {
		return fmt.Errorf("the Cloudflare API token has a space or a quote in it. Check that you pasted the whole token and nothing else")
	}

	store := e.openVault(*repositoryDirectory)
	saved, err := store.keyNames()
	if err != nil {
		return err
	}
	client := &cloudflareClient{baseURL: e.cloudflareAPIURL, token: token, httpClient: e.httpClient}

	e.say("Checking the Cloudflare API token.")
	if err := client.verifyToken(); err != nil {
		return err
	}
	zone, err := client.findZone(*domain)
	if err != nil {
		return err
	}
	if client.accountID, err = client.chooseAccount(zone); err != nil {
		return err
	}
	e.say("Account %s, zone %s (%s).", client.accountID, zone.ID, zone.Name)

	// The tunnel and the DNS records for each app.
	tunnelID, madeTunnel, err := client.ensureTunnel(*tunnelName)
	if err != nil {
		return err
	}
	e.say("Tunnel %s: %s.", *tunnelName, map[bool]string{true: "created", false: "found"}[madeTunnel])
	if madeTunnel || !saved[tunnelTokenKey] {
		tunnelToken, err := client.tunnelToken(tunnelID)
		if err != nil {
			return err
		}
		if err := saveSecret(store, saved, tunnelTokenKey, tunnelToken, true); err != nil {
			return err
		}
		e.say("Saved %s in %s.", tunnelTokenKey, vaultFileName)
	}
	hosts := make([]string, len(apps))
	for i, app := range apps {
		hosts[i] = app.Host
		outcome, err := client.ensureProxiedCNAME(zone.ID, app.Host, tunnelID+".cfargotunnel.com")
		if err != nil {
			return err
		}
		e.say("DNS record for %s: %s.", app.Host, outcome)
	}
	outcome, err := client.ensureCacheRule(zone.ID, hosts)
	if err != nil {
		return err
	}
	e.say("Cache rule: %s.", outcome)

	// The locked backup bucket, and the buckets for each app's uploads.
	if err := e.ensureBucket(client, *backupBucket); err != nil {
		return err
	}
	outcome, err = client.ensureBucketLock(*backupBucket, *lockDays)
	if err != nil {
		return err
	}
	e.say("Bucket Lock on %s (%d days): %s.", *backupBucket, *lockDays, outcome)
	for _, app := range apps {
		if app.UploadsBucket != "" {
			if err := e.ensureBucket(client, app.UploadsBucket); err != nil {
				return err
			}
		}
	}

	// Scoped R2 tokens: each can only read and write the objects of one bucket.
	permissionGroup, err := client.permissionGroupID("Workers R2 Storage Bucket Item Write", "com.cloudflare.edge.r2.bucket")
	if err != nil {
		return err
	}
	if err := e.ensureBucketToken(client, store, saved, permissionGroup, "solo-r2-backup", *backupBucket, backupAccessKeyIDKey, backupSecretKey); err != nil {
		return err
	}
	for _, app := range apps {
		if app.UploadsBucket == "" {
			continue
		}
		err := e.ensureBucketToken(client, store, saved, permissionGroup, "solo-r2-"+app.Name+"-uploads", app.UploadsBucket,
			app.Name+"_R2_ACCESS_KEY_ID", app.Name+"_R2_SECRET_ACCESS_KEY")
		if err != nil {
			return err
		}
	}

	workerURL, err := e.setUpWorker(client, store, saved, *workerName, *workerFile, source)
	if err != nil {
		return err
	}

	summary := cloudflareSummary{
		AccountID: client.accountID, ZoneID: zone.ID, TunnelID: tunnelID,
		R2S3Endpoint: client.accountID + ".r2.cloudflarestorage.com",
		BackupBucket: *backupBucket, LockDays: *lockDays, WorkerURL: workerURL, Apps: apps,
	}
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(e.stdout, string(encoded))
	return nil
}

func (e *environment) ensureBucket(client *cloudflareClient, name string) error {
	made, err := client.ensureR2Bucket(name)
	if err != nil {
		return err
	}
	e.say("R2 bucket %s: %s.", name, map[bool]string{true: "created", false: "found"}[made])
	return nil
}

// ensureBucketToken makes the scoped R2 token for one bucket and saves its S3
// key pair in the vault. Cloudflare shows a token's value only once, so a
// rerun keeps a token whose keys the vault already holds. When the vault lost
// the keys, the old token is deleted and a new one is made.
func (e *environment) ensureBucketToken(client *cloudflareClient, store vault, saved map[string]bool, permissionGroup, tokenName, bucket, accessKeyIDKey, secretKey string) error {
	existing, err := client.accountTokensNamed(tokenName)
	if err != nil {
		return err
	}
	if len(existing) == 1 && saved[accessKeyIDKey] && saved[secretKey] {
		e.say("R2 token %s for bucket %s: found, keys already in the vault.", tokenName, bucket)
		return nil
	}
	for _, old := range existing {
		if err := client.deleteAccountToken(old.ID); err != nil {
			return err
		}
	}
	token, err := client.createBucketItemToken(tokenName, bucket, permissionGroup)
	if err != nil {
		return err
	}
	accessKeyID, secretAccessKey := r2Credentials(token)
	if err := saveSecret(store, saved, accessKeyIDKey, accessKeyID, true); err != nil {
		return err
	}
	if err := saveSecret(store, saved, secretKey, secretAccessKey, true); err != nil {
		return err
	}
	e.say("R2 token %s for bucket %s: created. Saved %s and %s in %s.", tokenName, bucket, accessKeyIDKey, secretKey, vaultFileName)
	return nil
}

// setUpWorker uploads the outside alarm Worker, gives it its check-in token,
// and returns its public address.
func (e *environment) setUpWorker(client *cloudflareClient, store vault, saved map[string]bool, name, workerFile string, source []byte) (string, error) {
	existing, err := client.findWorker(name)
	if err != nil {
		return "", err
	}
	if err := client.uploadWorker(name, workerFile, source, existing); err != nil {
		return "", err
	}
	e.say("Worker %s: uploaded.", name)
	subdomain, err := client.ensureWorkersSubdomain(randomHex)
	if err != nil {
		return "", err
	}
	if err := client.enableWorkersDev(name); err != nil {
		return "", err
	}

	// The check-in token lives in the vault, for the server's check-ins, and
	// on the Worker, to accept them. Cloudflare never shows a Worker secret
	// again, and the vault is never read back, so a token missing on either
	// side is replaced on both.
	hasSecret, err := client.workerHasSecret(name, workerTokenSecretName)
	if err != nil {
		return "", err
	}
	if !saved[alarmTokenKey] || !hasSecret {
		value, err := randomHex(32)
		if err != nil {
			return "", err
		}
		if err := saveSecret(store, saved, alarmTokenKey, value, true); err != nil {
			return "", err
		}
		if err := client.setWorkerSecret(name, workerTokenSecretName, value); err != nil {
			return "", err
		}
		e.say("Check-in token: made. Saved %s in %s and set it on the Worker.", alarmTokenKey, vaultFileName)
	} else {
		e.say("Check-in token: found on the Worker and in the vault.")
	}
	if err := client.installSchedule(name); err != nil {
		return "", err
	}
	e.say("Cron Trigger %q: installed.", workerSchedule)
	return fmt.Sprintf("https://%s.%s.workers.dev", name, subdomain), nil
}
