package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// cloudflareClient calls Cloudflare's v4 API with one bearer token.
// https://developers.cloudflare.com/fundamentals/api/how-to/make-api-calls/
type cloudflareClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
	accountID  string
}

// cloudflareError is Cloudflare's refusal. It holds only the HTTP status and
// Cloudflare's own error code and message, never a request or a response
// body, because those can carry a secret.
type cloudflareError struct {
	Call    string
	Status  int
	Code    int
	Message string
}

func (e *cloudflareError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("Cloudflare refused %s with HTTP %d", e.Call, e.Status)
	}
	return fmt.Sprintf("Cloudflare refused %s with HTTP %d, error %d: %s", e.Call, e.Status, e.Code, e.Message)
}

func refusedWith(err error, status int) bool {
	var refusal *cloudflareError
	return errors.As(err, &refusal) && refusal.Status == status
}

func (c *cloudflareClient) account(path string) string {
	return "/accounts/" + c.accountID + path
}

// call sends one request with a JSON body, or none, and decodes the
// answer's result into result.
func (c *cloudflareClient) call(method, path string, body, result any) error {
	var reader io.Reader
	contentType := ""
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader, contentType = bytes.NewReader(encoded), "application/json"
	}
	return c.send(method, path, contentType, reader, result)
}

func (c *cloudflareClient) send(method, path, contentType string, body io.Reader, result any) error {
	call := method + " " + strings.SplitN(path, "?", 2)[0]
	request, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("the request %s to Cloudflare did not complete: %v", call, stripURLFromError(err))
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("the answer to %s was cut off: %v", call, err)
	}
	var envelope struct {
		Success bool `json:"success"`
		Errors  []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
		Result json.RawMessage `json:"result"`
	}
	decodeErr := json.Unmarshal(answer, &envelope)
	if response.StatusCode < 200 || response.StatusCode > 299 {
		refusal := &cloudflareError{Call: call, Status: response.StatusCode}
		if decodeErr == nil && len(envelope.Errors) > 0 {
			refusal.Code, refusal.Message = envelope.Errors[0].Code, envelope.Errors[0].Message
		}
		return refusal
	}
	if decodeErr != nil {
		return fmt.Errorf("Cloudflare answered %s with something this command cannot read", call)
	}
	if result != nil && len(envelope.Result) > 0 && string(envelope.Result) != "null" {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("Cloudflare answered %s with something this command cannot read", call)
		}
	}
	return nil
}

// verifyToken checks that the token works and is active. A token owned by a
// person is verified at /user/tokens/verify. Cloudflare answers 401 there for
// a token owned by an account, which is then verified at the account's own
// address.
// https://developers.cloudflare.com/api/resources/user/subresources/tokens/methods/verify/
func (c *cloudflareClient) verifyToken() error {
	var answer struct {
		Status string `json:"status"`
	}
	err := c.call(http.MethodGet, "/user/tokens/verify", nil, &answer)
	if refusedWith(err, http.StatusUnauthorized) {
		var accounts []struct {
			ID string `json:"id"`
		}
		if listErr := c.call(http.MethodGet, "/accounts", nil, &accounts); listErr != nil || len(accounts) == 0 {
			return errors.New("Cloudflare does not accept this API token. Check that you pasted the whole token, and that it has not expired")
		}
		err = c.call(http.MethodGet, "/accounts/"+accounts[0].ID+"/tokens/verify", nil, &answer)
	}
	if err != nil {
		return err
	}
	if answer.Status != "active" {
		return fmt.Errorf("Cloudflare says the API token is %s, not active", answer.Status)
	}
	return nil
}

type cloudflareZone struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Account struct {
		ID string `json:"id"`
	} `json:"account"`
}

// findZone finds the zone of a domain the token can see.
// https://developers.cloudflare.com/api/resources/zones/methods/list/
func (c *cloudflareClient) findZone(domain string) (cloudflareZone, error) {
	var zones []cloudflareZone
	if err := c.call(http.MethodGet, "/zones?name="+url.QueryEscape(domain), nil, &zones); err != nil {
		return cloudflareZone{}, err
	}
	for _, zone := range zones {
		if zone.Name == domain {
			return zone, nil
		}
	}
	return cloudflareZone{}, fmt.Errorf("this Cloudflare token cannot see a zone for %s. Add %s to your Cloudflare account first, and give the token access to it", domain, domain)
}

// chooseAccount picks the account that owns the zone, and checks that the
// token can see that account.
// https://developers.cloudflare.com/api/resources/accounts/methods/list/
func (c *cloudflareClient) chooseAccount(zone cloudflareZone) (string, error) {
	var accounts []struct {
		ID string `json:"id"`
	}
	if err := c.call(http.MethodGet, "/accounts", nil, &accounts); err != nil {
		return "", err
	}
	for _, account := range accounts {
		if zone.Account.ID == "" && len(accounts) == 1 || account.ID == zone.Account.ID {
			return account.ID, nil
		}
	}
	return "", fmt.Errorf("this Cloudflare token cannot see the account that owns %s", zone.Name)
}

// ensureTunnel finds the tunnel by its name or makes it, and says whether it
// made it. A tunnel with "local" configuration keeps its routes in the
// systems repository's cloudflared configuration file.
// https://developers.cloudflare.com/api/resources/zero_trust/subresources/tunnels/subresources/cloudflared/
func (c *cloudflareClient) ensureTunnel(name string) (string, bool, error) {
	var tunnels []struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		DeletedAt *string `json:"deleted_at"`
	}
	if err := c.call(http.MethodGet, c.account("/cfd_tunnel?is_deleted=false&name="+url.QueryEscape(name)), nil, &tunnels); err != nil {
		return "", false, err
	}
	var ids []string
	for _, tunnel := range tunnels {
		if tunnel.Name == name && tunnel.DeletedAt == nil {
			ids = append(ids, tunnel.ID)
		}
	}
	switch len(ids) {
	case 1:
		return ids[0], false, nil
	case 0:
	default:
		return "", false, fmt.Errorf("the Cloudflare account has %d tunnels named %s. Delete the extra ones on Cloudflare's website, then run this command again", len(ids), name)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := c.call(http.MethodPost, c.account("/cfd_tunnel"), map[string]any{"name": name, "config_src": "local"}, &created); err != nil {
		return "", false, err
	}
	return created.ID, true, nil
}

func (c *cloudflareClient) tunnelToken(tunnelID string) (string, error) {
	var token string
	if err := c.call(http.MethodGet, c.account("/cfd_tunnel/"+tunnelID+"/token"), nil, &token); err != nil {
		return "", err
	}
	if token == "" {
		return "", errors.New("Cloudflare returned an empty tunnel token")
	}
	return token, nil
}

type dnsRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

// ensureProxiedCNAME makes host a proxied CNAME to target, and says what it
// did. It ignores non-address records at the same name (such as MX and TXT
// email records at an apex domain), and refuses to replace an existing A,
// AAAA, HTTPS, or SVCB record, which could be a working website.
// https://developers.cloudflare.com/api/resources/dns/subresources/records/
func (c *cloudflareClient) ensureProxiedCNAME(zoneID, host, target string) (string, error) {
	var allRecords []dnsRecord
	if err := c.call(http.MethodGet, "/zones/"+zoneID+"/dns_records?name="+url.QueryEscape(host), nil, &allRecords); err != nil {
		return "", err
	}
	var records []dnsRecord
	for _, record := range allRecords {
		switch strings.ToUpper(record.Type) {
		case "A", "AAAA", "CNAME", "HTTPS", "SVCB":
			records = append(records, record)
		}
	}
	desired := map[string]any{"type": "CNAME", "name": host, "content": target, "proxied": true, "ttl": 1,
		"comment": "Monkeymaxxing Solo: app behind the Cloudflare Tunnel"}
	switch len(records) {
	case 0:
		return "created", c.call(http.MethodPost, "/zones/"+zoneID+"/dns_records", desired, nil)
	case 1:
		record := records[0]
		if record.Type != "CNAME" {
			return "", fmt.Errorf("%s already has a DNS record of type %s. Solo will not replace it. Delete that record on Cloudflare's website if it is not needed, then run this command again", host, record.Type)
		}
		if strings.EqualFold(strings.TrimSuffix(record.Content, "."), target) && record.Proxied {
			return "unchanged", nil
		}
		return "updated", c.call(http.MethodPut, "/zones/"+zoneID+"/dns_records/"+record.ID, desired, nil)
	}
	return "", fmt.Errorf("%s has %d address DNS records (A, AAAA, CNAME, HTTPS, or SVCB). Delete the extra ones on Cloudflare's website, then run this command again", host, len(records))
}

// cacheRuleReference marks the one cache rule this command owns, so a rerun
// updates it and leaves the person's other cache rules alone.
const cacheRuleReference = "solo_respect_cache_control"

// ensureCacheRule makes the zone's cache rule for the apps' hosts: use the
// Cache-Control header the app sends when present, and bypass the cache when
// absent (bypass_by_default) so logged-in pages without Cache-Control are
// never cached at Cloudflare's edge.
// https://developers.cloudflare.com/cache/how-to/cache-rules/
func (c *cloudflareClient) ensureCacheRule(zoneID string, hosts []string) (string, error) {
	path := "/zones/" + zoneID + "/rulesets/phases/http_request_cache_settings/entrypoint"
	var current struct {
		Rules []map[string]any `json:"rules"`
	}
	if err := c.call(http.MethodGet, path, nil, &current); err != nil && !refusedWith(err, http.StatusNotFound) {
		return "", err
	}
	quoted := make([]string, len(hosts))
	for i, host := range hosts {
		quoted[i] = strconv.Quote(host)
	}
	ours := map[string]any{
		"ref":         cacheRuleReference,
		"description": "Monkeymaxxing Solo: respect Cache-Control when present, bypass cache otherwise",
		"expression":  "(http.host in {" + strings.Join(quoted, " ") + "})",
		"action":      "set_cache_settings",
		"enabled":     true,
		"action_parameters": map[string]any{
			"cache":    true,
			"edge_ttl": map[string]any{"mode": "bypass_by_default"},
		},
	}
	rules := []map[string]any{}
	outcome := "created"
	for _, rule := range current.Rules {
		delete(rule, "last_updated")
		delete(rule, "version")
		if rule["ref"] == cacheRuleReference {
			outcome = "updated"
			if sameJSON(withoutIdentifiers(rule), ours) {
				outcome = "unchanged"
			}
			continue
		}
		rules = append(rules, rule)
	}
	if outcome == "unchanged" {
		return outcome, nil
	}
	rules = append(rules, ours)
	return outcome, c.call(http.MethodPut, path, map[string]any{"rules": rules}, nil)
}

func withoutIdentifiers(rule map[string]any) map[string]any {
	copied := map[string]any{}
	for key, value := range rule {
		if key != "id" {
			copied[key] = value
		}
	}
	return copied
}

func sameJSON(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return bytes.Equal(left, right)
}

// ensureR2Bucket finds the bucket or makes it, and says whether it made it.
// https://developers.cloudflare.com/api/resources/r2/subresources/buckets/
func (c *cloudflareClient) ensureR2Bucket(name string) (bool, error) {
	err := c.call(http.MethodGet, c.account("/r2/buckets/"+url.PathEscape(name)), nil, nil)
	if err == nil {
		return false, nil
	}
	if !refusedWith(err, http.StatusNotFound) {
		return false, err
	}
	return true, c.call(http.MethodPost, c.account("/r2/buckets"), map[string]any{"name": name}, nil)
}

const (
	bucketLockWALRuleID  = "solo-backup-wal-lock"
	bucketLockDataRuleID = "solo-backup-data-lock"
	bucketLockWALPrefix  = "pgbackrest/archive/main/1"
	bucketLockDataPrefix = "pgbackrest/backup/main/2"
)

// ensureBucketLock makes every WAL segment and backup folder in the bucket
// undeletable and unoverwritable for lockDays days after it is written.
// Cloudflare R2 Bucket Locks block overwrites as well as deletes, so the
// rules lock only the immutable WAL and backup prefixes and leave
// pgBackRest's mutable index files (archive.info, backup.info, and their
// .copy files) writable for subsequent nightly backups.
// https://developers.cloudflare.com/r2/buckets/bucket-locks/
func (c *cloudflareClient) ensureBucketLock(bucket string, lockDays int) (string, error) {
	path := c.account("/r2/buckets/" + url.PathEscape(bucket) + "/lock")
	maxAgeSeconds := lockDays * 86400
	var current struct {
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
	if err := c.call(http.MethodGet, path, nil, &current); err != nil && !refusedWith(err, http.StatusNotFound) {
		return "", err
	}
	desired := []struct {
		id     string
		prefix string
	}{
		{id: bucketLockWALRuleID, prefix: bucketLockWALPrefix},
		{id: bucketLockDataRuleID, prefix: bucketLockDataPrefix},
	}
	outcome := "created"
	if len(current.Rules) > 0 {
		outcome = "updated"
	}
	if len(current.Rules) == len(desired) {
		allMatch := true
		for _, want := range desired {
			matched := false
			for _, rule := range current.Rules {
				if rule.ID == want.id && rule.Enabled && rule.Prefix == want.prefix &&
					rule.Condition.Type == "Age" && rule.Condition.MaxAgeSeconds == maxAgeSeconds {
					matched = true
					break
				}
			}
			if !matched {
				allMatch = false
				break
			}
		}
		if allMatch {
			return "unchanged", nil
		}
	}
	rules := make([]map[string]any, 0, len(desired))
	for _, want := range desired {
		rules = append(rules, map[string]any{
			"id": want.id, "enabled": true, "prefix": want.prefix,
			"condition": map[string]any{"type": "Age", "maxAgeSeconds": maxAgeSeconds},
		})
	}
	body := map[string]any{"rules": rules}
	return outcome, c.call(http.MethodPut, path, body, nil)
}

type accountToken struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

// permissionGroupID finds the one permission group with this name for the
// scope.
// https://developers.cloudflare.com/api/resources/accounts/subresources/tokens/subresources/permission_groups/
func (c *cloudflareClient) permissionGroupID(name, scope string) (string, error) {
	var groups []struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	query := "/tokens/permission_groups?name=" + url.QueryEscape(name)
	err := c.call(http.MethodGet, c.account(query), nil, &groups)
	var refusal *cloudflareError
	if errors.As(err, &refusal) {
		err = c.call(http.MethodGet, "/user"+query, nil, &groups)
	}
	if err != nil {
		return "", err
	}
	var matches []string
	for _, group := range groups {
		if group.Name != name {
			continue
		}
		for _, groupScope := range group.Scopes {
			if groupScope == scope {
				matches = append(matches, group.ID)
			}
		}
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("Cloudflare lists %d permission groups named %q, not one", len(matches), name)
	}
	return matches[0], nil
}

// accountTokensNamed lists the account's API tokens with this name.
// https://developers.cloudflare.com/api/resources/accounts/subresources/tokens/methods/list/
func (c *cloudflareClient) accountTokensNamed(name string) ([]accountToken, error) {
	var named []accountToken
	for page := 1; ; page++ {
		var tokens []accountToken
		if err := c.call(http.MethodGet, c.account(fmt.Sprintf("/tokens?page=%d&per_page=50", page)), nil, &tokens); err != nil {
			return nil, err
		}
		for _, token := range tokens {
			if token.Name == name {
				named = append(named, token)
			}
		}
		if len(tokens) < 50 {
			return named, nil
		}
	}
}

func (c *cloudflareClient) deleteAccountToken(id string) error {
	return c.call(http.MethodDelete, c.account("/tokens/"+id), nil, nil)
}

// createBucketItemToken makes an API token that can read and write the
// objects of one bucket and nothing else. It cannot change the bucket's lock,
// delete the bucket, or touch another bucket. The token's value is returned
// only now.
// https://developers.cloudflare.com/r2/api/tokens/
func (c *cloudflareClient) createBucketItemToken(tokenName, bucket, permissionGroupID string) (accountToken, error) {
	resource := "com.cloudflare.edge.r2.bucket." + c.accountID + "_default_" + bucket
	body := map[string]any{"name": tokenName, "policies": []map[string]any{{
		"effect":            "allow",
		"resources":         map[string]any{resource: "*"},
		"permission_groups": []map[string]string{{"id": permissionGroupID}},
	}}}
	var token accountToken
	if err := c.call(http.MethodPost, c.account("/tokens"), body, &token); err != nil {
		return accountToken{}, err
	}
	if token.ID == "" || token.Value == "" {
		return accountToken{}, errors.New("Cloudflare made the API token but did not return its value. Delete the token on Cloudflare's website and run this command again")
	}
	return token, nil
}

// r2Credentials turns an R2 API token into its S3 key pair: the access key ID
// is the token's ID, and the secret access key is the SHA-256 of its value.
// https://developers.cloudflare.com/r2/api/tokens/
func r2Credentials(token accountToken) (accessKeyID, secretAccessKey string) {
	digest := sha256.Sum256([]byte(token.Value))
	return token.ID, hex.EncodeToString(digest[:])
}
