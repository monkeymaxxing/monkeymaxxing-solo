package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
)

const (
	// workerCompatibilityDate fixes the Workers runtime behavior the alarm
	// was written and tested against, so a later runtime change cannot
	// change it silently.
	workerCompatibilityDate = "2025-09-01"
	durableObjectBinding    = "SOLO_OUTSIDE_ALARM"
	durableObjectClass      = "SoloOutsideAlarm"
	workerTokenSecretName   = "SOLO_OUTSIDE_ALARM_TOKEN"
	// firstMigrationTag names the migration that creates the Durable Object
	// class. Cloudflare applies each tag once.
	firstMigrationTag = "v1"
	// workerSchedule is the Cron Trigger that runs the active /health
	// check from Cloudflare's edge, every 5 minutes.
	workerSchedule = "*/5 * * * *"
)

type workerScript struct {
	exists       bool
	migrationTag string
}

// findWorker looks for the Worker among the account's Workers.
// https://developers.cloudflare.com/api/resources/workers/subresources/scripts/methods/list/
func (c *cloudflareClient) findWorker(name string) (workerScript, error) {
	var scripts []struct {
		ID           string `json:"id"`
		MigrationTag string `json:"migration_tag"`
	}
	if err := c.call(http.MethodGet, c.account("/workers/scripts"), nil, &scripts); err != nil {
		return workerScript{}, err
	}
	for _, script := range scripts {
		if script.ID == name {
			return workerScript{exists: true, migrationTag: script.MigrationTag}, nil
		}
	}
	return workerScript{}, nil
}

// uploadWorker uploads the Worker as one ES module with its Durable Object
// binding. The first upload also creates the Durable Object class with a
// SQLite database, which the Workers Free plan allows. A later upload keeps
// the secrets the Worker already holds.
// https://developers.cloudflare.com/api/resources/workers/subresources/scripts/methods/update/
func (c *cloudflareClient) uploadWorker(name, workerFile string, source []byte, existing workerScript) error {
	moduleName := filepath.Base(workerFile)
	metadata := map[string]any{
		"main_module":        moduleName,
		"compatibility_date": workerCompatibilityDate,
		"bindings": []map[string]any{{
			"type": "durable_object_namespace", "name": durableObjectBinding, "class_name": durableObjectClass,
		}},
		"keep_bindings": []string{"secret_text"},
	}
	if existing.migrationTag == "" {
		metadata["migrations"] = map[string]any{
			"new_tag":            firstMigrationTag,
			"new_sqlite_classes": []string{durableObjectClass},
		}
	}
	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	metadataHeader := textproto.MIMEHeader{}
	metadataHeader.Set("Content-Disposition", `form-data; name="metadata"`)
	metadataHeader.Set("Content-Type", "application/json")
	part, err := form.CreatePart(metadataHeader)
	if err != nil {
		return err
	}
	part.Write(encodedMetadata)
	moduleHeader := textproto.MIMEHeader{}
	moduleHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, moduleName, moduleName))
	moduleHeader.Set("Content-Type", "application/javascript+module")
	part, err = form.CreatePart(moduleHeader)
	if err != nil {
		return err
	}
	part.Write(source)
	if err := form.Close(); err != nil {
		return err
	}
	return c.send(http.MethodPut, c.account("/workers/scripts/"+url.PathEscape(name)), form.FormDataContentType(), &body, nil)
}

// ensureWorkersSubdomain makes sure the account has a workers.dev address
// and returns it. A new account has none, and Cloudflare asks the person to
// choose one on its website. This command picks "solo-" and six random
// hexadecimal characters, so no one needs to choose it.
// https://developers.cloudflare.com/api/resources/workers/subresources/subdomains/
func (c *cloudflareClient) ensureWorkersSubdomain(randomHex func(int) (string, error)) (string, error) {
	var current struct {
		Subdomain string `json:"subdomain"`
	}
	err := c.call(http.MethodGet, c.account("/workers/subdomain"), nil, &current)
	if err == nil && current.Subdomain != "" {
		return current.Subdomain, nil
	}
	var refusal *cloudflareError
	if err != nil && !errors.As(err, &refusal) {
		return "", err
	}
	suffix, err := randomHex(3)
	if err != nil {
		return "", err
	}
	chosen := "solo-" + suffix
	if err := c.call(http.MethodPut, c.account("/workers/subdomain"), map[string]string{"subdomain": chosen}, &current); err != nil {
		return "", err
	}
	if current.Subdomain != "" {
		chosen = current.Subdomain
	}
	return chosen, nil
}

// enableWorkersDev serves the Worker at its workers.dev address. Preview
// URLs stay off: they are extra public addresses of the same Worker.
// https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/subdomain/
func (c *cloudflareClient) enableWorkersDev(name string) error {
	return c.call(http.MethodPost, c.account("/workers/scripts/"+url.PathEscape(name)+"/subdomain"),
		map[string]any{"enabled": true, "previews_enabled": false}, nil)
}

func (c *cloudflareClient) workerHasSecret(name, secretName string) (bool, error) {
	var secrets []struct {
		Name string `json:"name"`
	}
	if err := c.call(http.MethodGet, c.account("/workers/scripts/"+url.PathEscape(name)+"/secrets"), nil, &secrets); err != nil {
		return false, err
	}
	for _, secret := range secrets {
		if secret.Name == secretName {
			return true, nil
		}
	}
	return false, nil
}

// https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/secrets/methods/update/
func (c *cloudflareClient) setWorkerSecret(name, secretName, value string) error {
	return c.call(http.MethodPut, c.account("/workers/scripts/"+url.PathEscape(name)+"/secrets"),
		map[string]string{"name": secretName, "text": value, "type": "secret_text"}, nil)
}

// installSchedule replaces the Worker's Cron Triggers with the one schedule.
// https://developers.cloudflare.com/api/resources/workers/subresources/scripts/subresources/schedules/
func (c *cloudflareClient) installSchedule(name string) error {
	return c.call(http.MethodPut, c.account("/workers/scripts/"+url.PathEscape(name)+"/schedules"),
		[]map[string]string{{"cron": workerSchedule}}, nil)
}
