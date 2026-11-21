package main

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const (
	fakeCloudflareToken = "cf-api-token-for-tests-0123456789"
	fakeAccountID       = "acct0123456789"
	fakeZoneID          = "zone0123456789"
	fakeTunnelID        = "11111111-2222-3333-4444-555555555555"
	fakeTunnelToken     = "eyJhIjoidHVubmVsLXRva2VuLXZhbHVlIn0="
	fakeGroupID         = "pg-r2-bucket-item-write"
)

type fakeDNSRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

type fakeToken struct {
	ID       string
	Name     string
	Value    string
	Policies json.RawMessage
}

type fakeWorker struct {
	MigrationTag string
	Module       string
	Metadata     map[string]any
	Secrets      map[string]string
	Schedules    []map[string]string
	WorkersDev   map[string]any
	Uploads      int
}

// fakeCloudflare is a strict stand-in for the parts of Cloudflare's v4 API
// that the cloudflare command uses. It answers only the paths and methods
// the real API has, checks the token, and fails the test on any other
// request.
type fakeCloudflare struct {
	t      *testing.T
	server *httptest.Server

	mu       sync.Mutex
	requests []string

	tunnelExists  bool
	dnsRecords    []fakeDNSRecord
	cacheRules    []map[string]any
	buckets       map[string]bool
	locks         map[string]json.RawMessage
	tokens        []fakeToken
	nextTokenID   int
	workers       map[string]*fakeWorker
	subdomain     string
	subdomainPuts int
	// accountToken makes /user/tokens/verify answer 401, as Cloudflare does
	// for a token owned by an account.
	accountToken bool
}

func newFakeCloudflare(t *testing.T) *fakeCloudflare {
	t.Helper()
	fake := &fakeCloudflare{t: t, buckets: map[string]bool{}, locks: map[string]json.RawMessage{}, workers: map[string]*fakeWorker{}}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeCloudflare) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var writes []string
	for _, request := range f.requests {
		if !strings.HasPrefix(request, "GET ") {
			writes = append(writes, request)
		}
	}
	return writes
}

func (f *fakeCloudflare) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := 0
	for _, request := range f.requests {
		if strings.HasPrefix(request, prefix) {
			total++
		}
	}
	return total
}

func (f *fakeCloudflare) resetRequests() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = nil
}

func (f *fakeCloudflare) tokenNamed(name string) (fakeToken, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, token := range f.tokens {
		if token.Name == name {
			return token, true
		}
	}
	return fakeToken{}, false
}

func reply(w http.ResponseWriter, status int, result any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"success": status < 300, "errors": []any{}, "messages": []any{}, "result": result})
}

func refuse(w http.ResponseWriter, status, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"success": false, "errors": []map[string]any{{"code": code, "message": message}}, "result": nil})
}

func readJSON(r *http.Request, into any) bool {
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(into) == nil
}

func (f *fakeCloudflare) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	if r.Header.Get("Authorization") != "Bearer "+fakeCloudflareToken {
		refuse(w, http.StatusUnauthorized, 9109, "Invalid access token")
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	key := r.Method + " " + path
	switch {
	case key == "GET user/tokens/verify":
		if f.accountToken {
			refuse(w, http.StatusUnauthorized, 1000, "Invalid API Token")
			return
		}
		reply(w, 200, map[string]any{"id": "token-owner-id", "status": "active"})
	case key == "GET accounts":
		reply(w, 200, []any{map[string]any{"id": fakeAccountID, "name": "Test account"}})
	case key == "GET accounts/"+fakeAccountID+"/tokens/verify":
		reply(w, 200, map[string]any{"id": "token-owner-id", "status": "active"})
	case key == "GET zones":
		if r.URL.Query().Get("name") != "example.com" {
			reply(w, 200, []any{})
			return
		}
		reply(w, 200, []any{map[string]any{"id": fakeZoneID, "name": "example.com", "status": "active", "account": map[string]any{"id": fakeAccountID}}})
	case len(parts) >= 2 && parts[0] == "zones" && parts[1] == fakeZoneID:
		f.zone(w, r, parts[2:])
	case len(parts) >= 3 && parts[0] == "accounts" && parts[1] == fakeAccountID:
		f.account(w, r, parts[2:])
	default:
		f.t.Errorf("the fake Cloudflare has no route for %s", key)
		refuse(w, http.StatusNotFound, 7000, "No route for that URI")
	}
}

func (f *fakeCloudflare) zone(w http.ResponseWriter, r *http.Request, rest []string) {
	key := r.Method + " " + strings.Join(rest, "/")
	switch {
	case key == "GET dns_records":
		name := r.URL.Query().Get("name")
		found := []fakeDNSRecord{}
		for _, record := range f.dnsRecords {
			if record.Name == name {
				found = append(found, record)
			}
		}
		reply(w, 200, found)
	case key == "POST dns_records" || (r.Method == http.MethodPut && len(rest) == 2 && rest[0] == "dns_records"):
		var record fakeDNSRecord
		var extra struct {
			TTL int `json:"ttl"`
		}
		body, _ := io.ReadAll(r.Body)
		if json.Unmarshal(body, &record) != nil || json.Unmarshal(body, &extra) != nil || record.Type != "CNAME" || !record.Proxied || extra.TTL != 1 {
			f.t.Errorf("unexpected DNS record body %s", body)
			refuse(w, 400, 9005, "bad record")
			return
		}
		if r.Method == http.MethodPost {
			record.ID = fmt.Sprintf("dns-%d", len(f.dnsRecords)+1)
			f.dnsRecords = append(f.dnsRecords, record)
			reply(w, 200, record)
			return
		}
		for i := range f.dnsRecords {
			if f.dnsRecords[i].ID == rest[1] {
				record.ID = rest[1]
				f.dnsRecords[i] = record
				reply(w, 200, record)
				return
			}
		}
		refuse(w, 404, 81044, "Record does not exist.")
	case key == "GET rulesets/phases/http_request_cache_settings/entrypoint":
		if f.cacheRules == nil {
			refuse(w, 404, 10003, "Could not find entrypoint ruleset in the http_request_cache_settings phase")
			return
		}
		reply(w, 200, map[string]any{"id": "ruleset1", "phase": "http_request_cache_settings", "version": "3", "rules": f.cacheRules})
	case key == "PUT rulesets/phases/http_request_cache_settings/entrypoint":
		var body struct {
			Rules []map[string]any `json:"rules"`
		}
		if !readJSON(r, &body) || len(body.Rules) == 0 {
			refuse(w, 400, 20021, "bad ruleset")
			return
		}
		for i, rule := range body.Rules {
			if rule["ref"] == nil || rule["ref"] == "" {
				rule["ref"] = fmt.Sprintf("generated-%d", i)
			}
			rule["id"] = fmt.Sprintf("rule-%d", i)
			rule["version"], rule["last_updated"] = "1", "2026-01-01T00:00:00Z"
		}
		f.cacheRules = body.Rules
		reply(w, 200, map[string]any{"id": "ruleset1", "rules": body.Rules})
	default:
		f.t.Errorf("the fake Cloudflare has no zone route for %s", key)
		refuse(w, 404, 7000, "No route for that URI")
	}
}

func (f *fakeCloudflare) account(w http.ResponseWriter, r *http.Request, rest []string) {
	key := r.Method + " " + strings.Join(rest, "/")
	switch {
	case key == "GET cfd_tunnel":
		if r.URL.Query().Get("is_deleted") != "false" || r.URL.Query().Get("name") != "solo01" {
			f.t.Errorf("unexpected tunnel query %s", r.URL.RawQuery)
		}
		tunnels := []any{}
		if f.tunnelExists {
			tunnels = append(tunnels, map[string]any{"id": fakeTunnelID, "name": "solo01", "deleted_at": nil})
		}
		reply(w, 200, tunnels)
	case key == "POST cfd_tunnel":
		var body struct {
			Name      string `json:"name"`
			ConfigSrc string `json:"config_src"`
		}
		if !readJSON(r, &body) || body.Name != "solo01" || body.ConfigSrc != "local" {
			f.t.Errorf("unexpected tunnel body %+v", body)
		}
		f.tunnelExists = true
		reply(w, 200, map[string]any{"id": fakeTunnelID, "name": "solo01"})
	case key == "GET cfd_tunnel/"+fakeTunnelID+"/token":
		reply(w, 200, fakeTunnelToken)
	case len(rest) >= 2 && rest[0] == "r2" && rest[1] == "buckets":
		f.r2(w, r, rest[2:])
	case len(rest) >= 1 && rest[0] == "tokens":
		f.apiTokens(w, r, rest[1:])
	case len(rest) >= 1 && rest[0] == "workers":
		f.workersAPI(w, r, rest[1:])
	default:
		f.t.Errorf("the fake Cloudflare has no account route for %s", key)
		refuse(w, 404, 7000, "No route for that URI")
	}
}

func (f *fakeCloudflare) r2(w http.ResponseWriter, r *http.Request, rest []string) {
	switch {
	case r.Method == http.MethodPost && len(rest) == 0:
		var body struct {
			Name string `json:"name"`
		}
		if !readJSON(r, &body) || f.buckets[body.Name] {
			refuse(w, 409, 10004, "The bucket you tried to create already exists, and you own it.")
			return
		}
		f.buckets[body.Name] = true
		reply(w, 200, map[string]any{"name": body.Name})
	case r.Method == http.MethodGet && len(rest) == 1:
		if !f.buckets[rest[0]] {
			refuse(w, 404, 10006, "The specified bucket does not exist.")
			return
		}
		reply(w, 200, map[string]any{"name": rest[0]})
	case len(rest) == 2 && rest[1] == "lock" && r.Method == http.MethodGet:
		if !f.buckets[rest[0]] {
			refuse(w, 404, 10006, "The specified bucket does not exist.")
			return
		}
		if lock, found := f.locks[rest[0]]; found {
			reply(w, 200, lock)
			return
		}
		reply(w, 200, map[string]any{"rules": []any{}})
	case len(rest) == 2 && rest[1] == "lock" && r.Method == http.MethodPut:
		var raw json.RawMessage
		if !readJSON(r, &raw) || !f.buckets[rest[0]] {
			refuse(w, 400, 10005, "bad lock")
			return
		}
		f.locks[rest[0]] = raw
		reply(w, 200, map[string]any{})
	default:
		f.t.Errorf("the fake Cloudflare has no R2 route for %s %v", r.Method, rest)
		refuse(w, 404, 7000, "No route for that URI")
	}
}

func (f *fakeCloudflare) apiTokens(w http.ResponseWriter, r *http.Request, rest []string) {
	switch {
	case r.Method == http.MethodGet && len(rest) == 1 && rest[0] == "permission_groups":
		groups := []any{
			map[string]any{"id": "pg-other", "name": "Workers R2 Storage Bucket Item Write", "scopes": []string{"com.cloudflare.api.account"}},
			map[string]any{"id": fakeGroupID, "name": "Workers R2 Storage Bucket Item Write", "scopes": []string{"com.cloudflare.edge.r2.bucket"}},
		}
		reply(w, 200, groups)
	case r.Method == http.MethodGet && len(rest) == 0:
		listed := []any{}
		for _, token := range f.tokens {
			listed = append(listed, map[string]any{"id": token.ID, "name": token.Name, "status": "active"})
		}
		reply(w, 200, listed)
	case r.Method == http.MethodPost && len(rest) == 0:
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &body) != nil || body.Name == "" {
			refuse(w, 400, 1001, "a token needs a name")
			return
		}
		f.nextTokenID++
		var parsed map[string]json.RawMessage
		_ = json.Unmarshal(raw, &parsed)
		token := fakeToken{ID: fmt.Sprintf("r2tokenid%02d", f.nextTokenID), Name: body.Name, Value: fmt.Sprintf("cfat_r2_token_value_%02d", f.nextTokenID), Policies: parsed["policies"]}
		f.tokens = append(f.tokens, token)
		reply(w, 200, map[string]any{"id": token.ID, "name": token.Name, "value": token.Value, "status": "active"})
	case r.Method == http.MethodDelete && len(rest) == 1:
		for i, token := range f.tokens {
			if token.ID == rest[0] {
				f.tokens = append(f.tokens[:i], f.tokens[i+1:]...)
				reply(w, 200, map[string]any{"id": rest[0]})
				return
			}
		}
		refuse(w, 404, 1003, "token not found")
	default:
		f.t.Errorf("the fake Cloudflare has no token route for %s %v", r.Method, rest)
		refuse(w, 404, 7000, "No route for that URI")
	}
}

func (f *fakeCloudflare) workersAPI(w http.ResponseWriter, r *http.Request, rest []string) {
	key := r.Method + " " + strings.Join(rest, "/")
	switch {
	case key == "GET scripts":
		scripts := []any{}
		for name, worker := range f.workers {
			scripts = append(scripts, map[string]any{"id": name, "migration_tag": worker.MigrationTag})
		}
		reply(w, 200, scripts)
	case key == "GET subdomain":
		if f.subdomain == "" {
			refuse(w, 404, 10007, "This account does not have a workers.dev subdomain.")
			return
		}
		reply(w, 200, map[string]any{"subdomain": f.subdomain})
	case key == "PUT subdomain":
		var body struct {
			Subdomain string `json:"subdomain"`
		}
		if !readJSON(r, &body) || body.Subdomain == "" {
			refuse(w, 400, 10021, "bad subdomain")
			return
		}
		f.subdomain = body.Subdomain
		f.subdomainPuts++
		reply(w, 200, map[string]any{"subdomain": f.subdomain})
	case len(rest) >= 2 && rest[0] == "scripts":
		f.script(w, r, rest[1], rest[2:])
	default:
		f.t.Errorf("the fake Cloudflare has no Workers route for %s", key)
		refuse(w, 404, 7000, "No route for that URI")
	}
}

func (f *fakeCloudflare) script(w http.ResponseWriter, r *http.Request, name string, rest []string) {
	worker := f.workers[name]
	if len(rest) == 0 && r.Method == http.MethodPut {
		f.upload(w, r, name)
		return
	}
	if worker == nil {
		refuse(w, 404, 10007, "This Worker does not exist on your account.")
		return
	}
	key := r.Method + " " + strings.Join(rest, "/")
	switch key {
	case "POST subdomain":
		var body map[string]any
		if !readJSON(r, &body) || body["enabled"] != true {
			refuse(w, 400, 10021, "bad subdomain request")
			return
		}
		worker.WorkersDev = body
		reply(w, 200, body)
	case "GET secrets":
		secrets := []any{}
		for secretName := range worker.Secrets {
			secrets = append(secrets, map[string]string{"name": secretName, "type": "secret_text"})
		}
		reply(w, 200, secrets)
	case "PUT secrets":
		var body struct {
			Name string `json:"name"`
			Text string `json:"text"`
			Type string `json:"type"`
		}
		if !readJSON(r, &body) || body.Name == "" || body.Text == "" || body.Type != "secret_text" {
			refuse(w, 400, 10021, "invalid secret")
			return
		}
		worker.Secrets[body.Name] = body.Text
		reply(w, 200, map[string]string{"name": body.Name, "type": body.Type})
	case "PUT schedules":
		var schedules []map[string]string
		if !readJSON(r, &schedules) {
			refuse(w, 400, 10021, "schedules must be a list")
			return
		}
		worker.Schedules = schedules
		reply(w, 200, map[string]any{"schedules": schedules})
	default:
		f.t.Errorf("the fake Cloudflare has no Worker script route for %s", key)
		refuse(w, 404, 7000, "No route for that URI")
	}
}

func (f *fakeCloudflare) upload(w http.ResponseWriter, r *http.Request, name string) {
	mediaType, parameters, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		refuse(w, 400, 10021, "the upload must be multipart/form-data")
		return
	}
	reader := multipart.NewReader(r.Body, parameters["boundary"])
	parts, types := map[string]string{}, map[string]string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			refuse(w, 400, 10021, "unreadable multipart body")
			return
		}
		data, _ := io.ReadAll(part)
		parts[part.FormName()], types[part.FormName()] = string(data), part.Header.Get("Content-Type")
	}
	var metadata struct {
		MainModule        string           `json:"main_module"`
		CompatibilityDate string           `json:"compatibility_date"`
		Bindings          []map[string]any `json:"bindings"`
		KeepBindings      []string         `json:"keep_bindings"`
		Migrations        *struct {
			NewTag           string   `json:"new_tag"`
			NewSQLiteClasses []string `json:"new_sqlite_classes"`
		} `json:"migrations"`
	}
	if err := json.Unmarshal([]byte(parts["metadata"]), &metadata); err != nil || metadata.MainModule == "" || metadata.CompatibilityDate == "" {
		refuse(w, 400, 10021, "metadata needs main_module and compatibility_date")
		return
	}
	module, found := parts[metadata.MainModule]
	if !found || types[metadata.MainModule] != "application/javascript+module" {
		refuse(w, 400, 10021, "no ES module part named by main_module")
		return
	}
	worker := f.workers[name]
	isNew := worker == nil
	if isNew {
		worker = &fakeWorker{Secrets: map[string]string{}}
	}
	switch {
	case metadata.Migrations != nil && !isNew && worker.MigrationTag == metadata.Migrations.NewTag:
		refuse(w, 400, 10074, "migration tag "+metadata.Migrations.NewTag+" was already applied")
		return
	case metadata.Migrations == nil && worker.MigrationTag == "":
		refuse(w, 400, 10074, "a new Durable Object class needs a migration")
		return
	}
	if metadata.Migrations != nil {
		worker.MigrationTag = metadata.Migrations.NewTag
	}
	keeps := false
	for _, kept := range metadata.KeepBindings {
		keeps = keeps || kept == "secret_text"
	}
	if !keeps {
		worker.Secrets = map[string]string{}
	}
	worker.Module, worker.Uploads = module, worker.Uploads+1
	worker.Metadata = nil
	_ = json.Unmarshal([]byte(parts["metadata"]), &worker.Metadata)
	f.workers[name] = worker
	reply(w, 200, map[string]any{"id": name, "migration_tag": worker.MigrationTag})
}
