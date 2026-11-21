// Tests of the logic of the Solo outside alarm Worker. Run from the repository root:
//   node --test worker/solo_outside_alarm_worker_test.mjs
// They use a fake of the Durable Object's storage and alarm, a fake clock, a
// fake send function, and a fake fetch, so no Cloudflare account and no npm
// package are needed.

import { registerHooks } from "node:module";
import { test } from "node:test";
import assert from "node:assert/strict";

// The Worker imports the DurableObject base class from Cloudflare's runtime;
// here a stand-in with the same constructor takes its place.
registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === "cloudflare:workers") {
      return { url: "data:text/javascript,export class DurableObject { constructor(ctx, env) { this.ctx = ctx; this.env = env; } }", shortCircuit: true };
    }
    return nextResolve(specifier, context);
  },
});
const worker = await import("./solo_outside_alarm_worker.js");

// FakeStorage has the Durable Object storage methods the Worker uses, and
// counts written keys. Like Cloudflare's, its put and delete refuse more than
// 128 keys in one call.
// https://developers.cloudflare.com/durable-objects/api/sqlite-storage-api/#put
class FakeStorage {
  constructor() {
    this.data = new Map();
    this.alarm = null;
    this.writtenKeys = [];
  }
  async get(key) {
    return structuredClone(this.data.get(key));
  }
  async put(keyOrEntries, value) {
    const entries = typeof keyOrEntries === "string" ? { [keyOrEntries]: value } : keyOrEntries;
    if (Object.keys(entries).length > 128) {
      throw new RangeError(`put() supports up to 128 key-value pairs at a time, not ${Object.keys(entries).length}`);
    }
    for (const [key, entry] of Object.entries(entries)) {
      this.data.set(key, structuredClone(entry));
      this.writtenKeys.push(key);
    }
  }
  async delete(keys) {
    if ([].concat(keys).length > 128) {
      throw new RangeError(`delete() supports up to 128 keys at a time, not ${[].concat(keys).length}`);
    }
    for (const key of [].concat(keys)) {
      this.data.delete(key);
    }
  }
  async list({ prefix }) {
    const keys = [...this.data.keys()].filter((key) => key.startsWith(prefix)).sort();
    return new Map(keys.map((key) => [key, structuredClone(this.data.get(key))]));
  }
  async getAlarm() {
    return this.alarm;
  }
  async setAlarm(ms) {
    this.alarm = ms;
  }
  async deleteAlarm() {
    this.alarm = null;
  }
}

// FakeServices answers the held requests the Worker sends, by URL. An answer
// is a status code, null for no answer, or an Error to throw.
class FakeServices {
  constructor() {
    this.sent = [];
    this.answers = new Map();
    // answerTexts holds the text of a service's answer, which a test reads.
    this.answerTexts = new Map();
  }
  answer(url, ...results) {
    this.answers.set(url, results);
  }
  send = async (request, readAnswer) => {
    this.sent.push(request);
    const results = this.answers.get(request.url) ?? [200];
    const result = results.length > 1 ? results.shift() : results[0];
    if (result instanceof Error) {
      throw result;
    }
    const text = this.answerTexts.get(request.url);
    return readAnswer && text !== undefined ? { http_status_code: result, answer_text: text } : { http_status_code: result };
  };
}

// FakeProbe is the fetch the health checks use. An answer is an HTTP status
// code or an Error to throw; the last answer repeats.
class FakeProbe {
  constructor() {
    this.requested = [];
    this.answers = new Map();
  }
  answer(url, ...results) {
    this.answers.set(url, results);
  }
  fetch = async (url, options) => {
    this.requested.push({ url, options });
    const results = this.answers.get(url) ?? [200];
    const result = results.length > 1 ? results.shift() : results[0];
    if (result instanceof Error) {
      throw result;
    }
    return new Response(null, { status: result });
  };
}

function heldRequest(url, body) {
  return { method: "POST", url, headers: { "Content-Type": "application/x-www-form-urlencoded" }, body };
}

function heldMessage(id, delaySeconds, url) {
  return {
    held_message_id: id,
    delay_seconds: delaySeconds,
    request: heldRequest(url, "message=solo01+stopped+checking+in"),
    test_request: heldRequest(url, "message=Solo+outside+alarm+test"),
  };
}

function healthCheck(id, url, extra = {}) {
  return { check_id: id, url, alert_request: heldRequest("https://api.pushover.net/1/messages.json?to=alerts", `message=${id}+is+down`), ...extra };
}

const alice = heldMessage("primary-contact-method-1", 600, "https://api.pushover.net/1/messages.json?to=alice");
const bob = heldMessage("backup-contact-method-2", 1200, "https://api.pushover.net/1/messages.json?to=bob");
const aliceRecoveryRequest = heldRequest("https://api.pushover.net/1/messages.json?to=alice", "message=solo01+is+back");
const aliceWithRecovery = { ...alice, recovery_request: aliceRecoveryRequest };
const bobRecoveryRequest = heldRequest("https://api.pushover.net/1/messages.json?to=bob", "message=solo01+is+back");
const bobWithRecovery = { ...bob, recovery_request: bobRecoveryRequest };
const healthUrl = "https://app.example.com/health";
const healthRecoveryRequest = heldRequest("https://api.pushover.net/1/messages.json?to=alerts", "message=app+is+back");
// 2026-10-27 03:00:00 UTC
const T = Date.UTC(2026, 9, 27, 3, 0, 0);
const second = 1000;
const minute = 60 * second;

// at is a clock that stays at one time, as if every request were answered at once.
const at = (time) => () => time;

const objectAddress = "https://solo-outside-alarm.example.workers.dev";

async function checkIn(storage, heldMessages, now, { healthChecks, services = new FakeServices() } = {}) {
  const body = healthChecks === undefined ? { held_messages: heldMessages } : { held_messages: heldMessages, health_checks: healthChecks };
  const response = await worker.handleObjectRequest(new Request(objectAddress + "/check-in", { method: "POST", body: JSON.stringify(body) }), storage, at(now), services.send, null);
  return { status: response.status, body: await response.json() };
}

// ---------------------------------------------------------------- Worker door

const TOKEN = "a-long-random-check-in-token";

function workerSetUp() {
  const storage = new FakeStorage();
  const services = new FakeServices();
  const probe = new FakeProbe();
  const object = new worker.SoloOutsideAlarm({ storage }, {});
  const objectRequests = [];
  const env = {
    SOLO_OUTSIDE_ALARM_TOKEN: TOKEN,
    SOLO_OUTSIDE_ALARM: {
      idFromName: (name) => name,
      get: (id) => ({
        fetch: (request, init) => {
          objectRequests.push({ id, request, init });
          return object.fetch(request instanceof Request ? request : new Request(request, init));
        },
      }),
    },
  };
  return { storage, services, probe, object, objectRequests, env };
}

function externalRequest(path, token, method = "POST", body = JSON.stringify({ held_messages: [] })) {
  return new Request(objectAddress + path, { method, headers: token ? { Authorization: "Bearer " + token } : {}, body: method === "POST" ? body : undefined });
}

test("the exported names are the ones the setup and the tests rely on", () => {
  assert.equal(worker.CHECK_IN_TOKEN_SECRET, "SOLO_OUTSIDE_ALARM_TOKEN");
  assert.equal(typeof worker.SoloOutsideAlarm, "function");
  assert.equal(typeof worker.default.fetch, "function");
  assert.equal(typeof worker.default.scheduled, "function");
  assert.equal(worker.default.fetch, worker.handleWorkerRequest);
  assert.equal(worker.default.scheduled, worker.handleScheduled);
});

test("only a POST /check-in or POST /send-test with the token reaches the Durable Object", async () => {
  const { env, storage, objectRequests } = workerSetUp();
  assert.equal((await worker.default.fetch(externalRequest("/check-in", "wrong"), env)).status, 401);
  assert.equal((await worker.default.fetch(externalRequest("/check-in", null), env)).status, 401);
  assert.equal((await worker.default.fetch(externalRequest("/send-test", "wrong"), env)).status, 401);
  // A token that starts like the right one, and one that extends it, are wrong.
  assert.equal((await worker.default.fetch(externalRequest("/check-in", TOKEN.slice(0, -1)), env)).status, 401);
  assert.equal((await worker.default.fetch(externalRequest("/check-in", TOKEN + "x"), env)).status, 401);
  assert.equal(objectRequests.length, 0, "a wrong token never touches the Durable Object");
  const accepted = await worker.default.fetch(externalRequest("/check-in", TOKEN), env);
  assert.equal(accepted.status, 200);
  assert.equal((await accepted.json()).held_messages_stored, 0);
  assert.ok(await storage.get("check_in"));
  assert.equal(objectRequests.length, 1);
  assert.equal(objectRequests[0].id, "solo-outside-alarm");
  assert.equal((await worker.default.fetch(externalRequest("/send-test", TOKEN), env)).status, 200);
});

test("the Worker answers 404 to every other method and path, even with the token", async () => {
  const { env, objectRequests } = workerSetUp();
  for (const [path, method] of [["/other", "POST"], ["/", "POST"], ["/check-in", "GET"], ["/check-in", "PUT"], ["/send-test", "GET"], ["/internal/run-health-checks", "POST"], ["/internal/run-health-checks", "GET"], ["/check-in/", "POST"]]) {
    const response = await worker.default.fetch(externalRequest(path, TOKEN, method), env);
    assert.equal(response.status, 404, `${method} ${path}`);
    const body = await response.json();
    assert.equal(body.error_code, "not_found");
    assert.equal(typeof body.error_message, "string");
  }
  // The 404 comes before the token check, so a caller without the token learns nothing more.
  assert.equal((await worker.default.fetch(externalRequest("/internal/run-health-checks", null), env)).status, 404);
  assert.equal(objectRequests.length, 0);
});

test("a Worker without its secret answers 503 and never touches the Durable Object", async () => {
  const { env, objectRequests } = workerSetUp();
  const withoutSecret = { SOLO_OUTSIDE_ALARM: env.SOLO_OUTSIDE_ALARM };
  const response = await worker.default.fetch(externalRequest("/check-in", TOKEN), withoutSecret);
  assert.equal(response.status, 503);
  assert.equal((await response.json()).error_code, "check_in_token_not_set");
  assert.equal((await worker.default.fetch(externalRequest("/send-test", TOKEN), { ...withoutSecret, SOLO_OUTSIDE_ALARM_TOKEN: "" })).status, 503);
  assert.equal(objectRequests.length, 0);
});

test("sameToken compares the whole value", () => {
  assert.equal(worker.sameToken("Bearer abc", "Bearer abc"), true);
  assert.equal(worker.sameToken("Bearer abd", "Bearer abc"), false);
  assert.equal(worker.sameToken("Bearer ab", "Bearer abc"), false);
  assert.equal(worker.sameToken("Bearer abcd", "Bearer abc"), false);
  assert.equal(worker.sameToken("", "Bearer abc"), false);
});

test("the Durable Object itself answers 404 to a GET or an unknown path", async () => {
  const storage = new FakeStorage();
  for (const [path, method] of [["/check-in", "GET"], ["/internal/run-health-checks", "GET"], ["/unknown", "POST"]]) {
    const response = await worker.handleObjectRequest(new Request(objectAddress + path, { method }), storage, at(T), null, null);
    assert.equal(response.status, 404, `${method} ${path}`);
  }
});

// ----------------------------------------------------------------- Scheduled

test("the scheduled handler has the Durable Object run the health checks", async () => {
  const { env, objectRequests, storage } = workerSetUp();
  // Make the object's fake fetch answer without real outgoing requests.
  await checkIn(storage, [], T, { healthChecks: [] });
  const waited = [];
  await worker.default.scheduled({ cron: "*/5 * * * *" }, env, { waitUntil: (promise) => waited.push(promise) });
  assert.equal(objectRequests.length, 1);
  assert.equal(objectRequests[0].id, "solo-outside-alarm");
  assert.equal(objectRequests[0].request, "https://solo-outside-alarm.internal/internal/run-health-checks");
  assert.equal(objectRequests[0].init.method, "POST");
  assert.equal(waited.length, 1, "the run is handed to waitUntil");
  await Promise.all(waited);
  // Without a context, it still runs.
  await worker.handleScheduled({}, env, undefined);
  assert.equal(objectRequests.length, 2);
});

test("the scheduled handler fails when the Durable Object's run fails", async () => {
  const env = { SOLO_OUTSIDE_ALARM: { idFromName: (name) => name, get: () => ({ fetch: async () => new Response("{}", { status: 500 }) }) } };
  await assert.rejects(worker.handleScheduled({}, env, undefined), /HTTP 500/);
});

// ---------------------------------------------------------------- Validation

test("a minimal check-in has no health checks, and defaults fill in the optional health check fields", async () => {
  assert.deepEqual(worker.checkCheckIn(JSON.stringify({ held_messages: [] })), { held_messages: [], health_checks: [] });
  const parsed = worker.checkCheckIn(JSON.stringify({ held_messages: [alice], health_checks: [healthCheck("app", healthUrl)] }));
  assert.deepEqual(parsed.held_messages, [alice]);
  assert.equal(parsed.health_checks[0].timeout_ms, 10000);
  assert.equal(parsed.health_checks[0].failures_before_alert, 2);
  assert.equal("recovery_request" in parsed.health_checks[0], false);
  const explicit = worker.checkCheckIn(JSON.stringify({ held_messages: [], health_checks: [healthCheck("app", healthUrl, { timeout_ms: 1000, failures_before_alert: 10, recovery_request: healthRecoveryRequest })] }));
  assert.deepEqual([explicit.health_checks[0].timeout_ms, explicit.health_checks[0].failures_before_alert, explicit.health_checks[0].recovery_request], [1000, 10, healthRecoveryRequest]);
  // A held message may carry a recovery request.
  assert.deepEqual(worker.checkCheckIn(JSON.stringify({ held_messages: [aliceWithRecovery] })).held_messages, [aliceWithRecovery]);
});

test("check-ins that break a rule are refused with the field, and change nothing", async () => {
  const tooLong = "https://app.example.com/" + "x".repeat(2048);
  const cases = [
    // held messages
    [(c) => { c.held_messages[0].request.url = "http://api.pushover.net/1/messages.json"; }, "held_messages[0].request.url"],
    [(c) => { c.held_messages[0].request.url = "https://user:pass@api.pushover.net/"; }, "held_messages[0].request.url"],
    [(c) => { c.held_messages[0].request.url = "https://api.pushover.net/#fragment"; }, "held_messages[0].request.url"],
    [(c) => { c.held_messages[0].request.url = "not a url"; }, "held_messages[0].request.url"],
    [(c) => { c.held_messages[0].request.url = "https://api.pushover.net/" + "x".repeat(8192); }, "held_messages[0].request.url"],
    [(c) => { c.held_messages[0].test_request.method = "DELETE"; }, "held_messages[0].test_request.method"],
    [(c) => { c.held_messages[0].request.headers.Host = "elsewhere.example.com"; }, "held_messages[0].request.headers"],
    [(c) => { c.held_messages[0].request.headers["Content-Length"] = "5"; }, "held_messages[0].request.headers"],
    [(c) => { c.held_messages[0].request.headers["Bad Name"] = "x"; }, "held_messages[0].request.headers"],
    [(c) => { c.held_messages[0].request.headers["X-Note"] = "line one\r\nInjected: yes"; }, "held_messages[0].request.headers"],
    [(c) => { c.held_messages[0].request.headers["X-Note"] = 5; }, "held_messages[0].request.headers"],
    [(c) => { c.held_messages[0].request.headers = []; }, "held_messages[0].request.headers"],
    [(c) => { c.held_messages[0].request.body = 5; }, "held_messages[0].request.body"],
    [(c) => { c.held_messages[0].request.extra = 1; }, "held_messages[0].request"],
    [(c) => { delete c.held_messages[0].request.body; }, "held_messages[0].request.body"],
    [(c) => { c.held_messages[0].delay_seconds = 179; }, "held_messages[0].delay_seconds"],
    [(c) => { c.held_messages[0].delay_seconds = 2147483648; }, "held_messages[0].delay_seconds"],
    [(c) => { c.held_messages[0].delay_seconds = 600.5; }, "held_messages[0].delay_seconds"],
    [(c) => { c.held_messages[0].delay_seconds = "600"; }, "held_messages[0].delay_seconds"],
    [(c) => { c.held_messages[0].held_message_id = "Upper"; }, "held_messages[0].held_message_id"],
    [(c) => { c.held_messages[0].held_message_id = "a".repeat(129); }, "held_messages[0].held_message_id"],
    [(c) => { c.held_messages[0].held_message_id = ""; }, "held_messages[0].held_message_id"],
    [(c) => { c.held_messages[0].extra = 1; }, "held_messages[0]"],
    [(c) => { delete c.held_messages[0].test_request; }, "held_messages[0].test_request"],
    [(c) => { c.held_messages[0].recovery_request = null; }, "held_messages[0].recovery_request"],
    [(c) => { c.held_messages[0].recovery_request = { ...aliceRecoveryRequest, method: "TRACE" }; }, "held_messages[0].recovery_request.method"],
    [(c) => { c.held_messages[0].request.body = "x".repeat(17000); }, "held_messages[0]"],
    [(c) => { c.held_messages[0] = "text"; }, "held_messages[0]"],
    [(c) => { c.held_messages = {}; }, "held_messages"],
    [(c) => { delete c.held_messages; }, "check-in.held_messages"],
    [(c) => { c.other = 1; }, "check-in"],
    // health checks
    [(c) => { c.health_checks[0].url = "http://app.example.com/health"; }, "health_checks[0].url"],
    [(c) => { c.health_checks[0].url = "https://user:pass@app.example.com/health"; }, "health_checks[0].url"],
    [(c) => { c.health_checks[0].url = "https://app.example.com/health#top"; }, "health_checks[0].url"],
    [(c) => { c.health_checks[0].url = tooLong; }, "health_checks[0].url"],
    [(c) => { c.health_checks[0].url = 5; }, "health_checks[0].url"],
    [(c) => { c.health_checks[0].check_id = "Upper"; }, "health_checks[0].check_id"],
    [(c) => { c.health_checks[0].check_id = "a".repeat(65); }, "health_checks[0].check_id"],
    [(c) => { c.health_checks[0].timeout_ms = 999; }, "health_checks[0].timeout_ms"],
    [(c) => { c.health_checks[0].timeout_ms = 15001; }, "health_checks[0].timeout_ms"],
    [(c) => { c.health_checks[0].timeout_ms = 2500.5; }, "health_checks[0].timeout_ms"],
    [(c) => { c.health_checks[0].failures_before_alert = 0; }, "health_checks[0].failures_before_alert"],
    [(c) => { c.health_checks[0].failures_before_alert = 11; }, "health_checks[0].failures_before_alert"],
    [(c) => { c.health_checks[0].failures_before_alert = "2"; }, "health_checks[0].failures_before_alert"],
    [(c) => { delete c.health_checks[0].alert_request; }, "health_checks[0].alert_request"],
    [(c) => { c.health_checks[0].alert_request.headers.Host = "x.example.com"; }, "health_checks[0].alert_request.headers"],
    [(c) => { c.health_checks[0].recovery_request = { method: "POST" }; }, "health_checks[0].recovery_request.url"],
    [(c) => { c.health_checks[0].extra = 1; }, "health_checks[0]"],
    [(c) => { c.health_checks[0].alert_request.body = "x".repeat(17000); }, "health_checks[0]"],
    [(c) => { c.health_checks = "app"; }, "health_checks"],
    [(c) => { c.health_checks = Array.from({ length: 33 }, (_, index) => healthCheck(`app-${index}`, healthUrl)); }, "health_checks"],
  ];
  for (const [change, field] of cases) {
    const body = { held_messages: [structuredClone(alice)], health_checks: [healthCheck("app", healthUrl)] };
    change(body);
    const storage = new FakeStorage();
    await checkIn(storage, [alice], T, { healthChecks: [healthCheck("earlier", healthUrl)] });
    const before = structuredClone([...storage.data]);
    const response = await worker.handleObjectRequest(new Request(objectAddress + "/check-in", { method: "POST", body: JSON.stringify(body) }), storage, at(T + minute), null, null);
    assert.equal(response.status, 400, field);
    const answer = await response.json();
    assert.equal(answer.error_code, "check_in_refused");
    assert.equal(answer.field, field);
    assert.ok(answer.error_message.startsWith(field), answer.error_message);
    assert.deepEqual([...storage.data], before, `${field}: a refused check-in changes nothing`);
    assert.equal(storage.alarm, T + 10 * minute);
  }
});

test("duplicate IDs, a body that is not JSON, and a body that is too large are refused", async () => {
  assert.equal((await checkIn(new FakeStorage(), [alice, alice], T)).body.field, "held_messages[1].held_message_id");
  assert.equal((await checkIn(new FakeStorage(), [], T, { healthChecks: [healthCheck("app", healthUrl), healthCheck("app", "https://other.example.com/health")] })).body.field, "health_checks[1].check_id");
  // The same ID in a held message and a health check is two different things.
  assert.equal((await checkIn(new FakeStorage(), [heldMessage("app", 600, "https://api.pushover.net/1/messages.json")], T, { healthChecks: [healthCheck("app", healthUrl)] })).status, 200);
  for (const text of ["{", "[]", "null", ""]) {
    const response = await worker.handleObjectRequest(new Request(objectAddress + "/check-in", { method: "POST", body: text }), new FakeStorage(), at(T), null, null);
    assert.equal(response.status, 400, text);
  }
  const tooLarge = JSON.stringify({ held_messages: [], padding: "x".repeat(worker.MAXIMUM_CHECK_IN_BYTES) });
  const response = await worker.handleObjectRequest(new Request(objectAddress + "/check-in", { method: "POST", body: tooLarge }), new FakeStorage(), at(T), null, null);
  assert.equal(response.status, 400);
  assert.match((await response.json()).error_message, /at most 1048576 bytes/);
});

test("32 health checks are allowed, and the size limits are exactly 16 KiB", async () => {
  const many = Array.from({ length: 32 }, (_, index) => healthCheck(`app-${index}`, `https://app${index}.example.com/health`));
  assert.equal((await checkIn(new FakeStorage(), [], T, { healthChecks: many })).status, 200);
  const sized = (limit) => {
    const message = structuredClone(alice);
    message.request.body = "";
    const base = new TextEncoder().encode(JSON.stringify(message)).length;
    message.request.body = "x".repeat(limit - base);
    return message;
  };
  assert.equal(new TextEncoder().encode(JSON.stringify(sized(worker.MAXIMUM_HELD_MESSAGE_BYTES))).length, 16384);
  assert.equal((await checkIn(new FakeStorage(), [sized(worker.MAXIMUM_HELD_MESSAGE_BYTES)], T)).status, 200);
  assert.equal((await checkIn(new FakeStorage(), [sized(worker.MAXIMUM_HELD_MESSAGE_BYTES + 1)], T)).status, 400);
  const check = healthCheck("app", healthUrl);
  check.alert_request.body = "";
  const base = new TextEncoder().encode(JSON.stringify(check)).length;
  check.alert_request.body = "x".repeat(worker.MAXIMUM_HEALTH_CHECK_BYTES - base);
  assert.equal((await checkIn(new FakeStorage(), [], T, { healthChecks: [check] })).status, 200);
  check.alert_request.body += "x";
  assert.equal((await checkIn(new FakeStorage(), [], T, { healthChecks: [check] })).body.field, "health_checks[0]");
});

test("a tab in a header value is allowed", async () => {
  const tab = structuredClone(alice);
  tab.request.headers["X-Note"] = "a\tb";
  assert.equal((await checkIn(new FakeStorage(), [tab], T)).status, 200);
});

// ------------------------------------------------------- Dead man's switch

test("the alarm sends each held message after its delay once check-ins stop", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  // The server checks in at 03:00:00 and freezes at 03:00:30.
  assert.equal((await checkIn(storage, [alice, bob], T)).status, 200);
  assert.equal(storage.alarm, T + 10 * minute);
  await worker.runAlarm(storage, at(T + 10 * minute), services.send);
  assert.deepEqual(services.sent.map((r) => r.url), [alice.request.url]);
  assert.equal(services.sent[0].body, alice.request.body);
  assert.equal(storage.alarm, T + 20 * minute);
  await worker.runAlarm(storage, at(T + 20 * minute), services.send);
  assert.deepEqual(services.sent.map((r) => r.url), [alice.request.url, bob.request.url]);
  assert.equal(storage.alarm, null);
  // An alarm that runs again, as alarms may, sends nothing twice.
  await worker.runAlarm(storage, at(T + 21 * minute), services.send);
  assert.equal(services.sent.length, 2);

  // The server comes back: its check-in's answer lists both sends.
  const answer = (await checkIn(storage, [alice, bob], T + 30 * minute)).body;
  assert.equal(answer.check_in_accepted_at, "2026-10-27T03:30:00Z");
  assert.deepEqual(answer.sends.map((s) => [s.send_id, s.held_message_id, s.send_outcome, s.http_status_code, s.due_at, s.silence_started_at, s.recovery_sent_at]), [
    [2, "backup-contact-method-2", "delivered", 200, "2026-10-27T03:20:00Z", "2026-10-27T03:00:00Z", null],
    [1, "primary-contact-method-1", "delivered", 200, "2026-10-27T03:10:00Z", "2026-10-27T03:00:00Z", null],
  ]);
  assert.equal(storage.alarm, T + 40 * minute);
});

test("a check-in before the delay moves the alarm and nothing is sent", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  await checkIn(storage, [alice], T);
  await checkIn(storage, [alice], T + minute);
  assert.equal(storage.alarm, T + 11 * minute);
  // An alarm set by an earlier check-in finds nothing due.
  await worker.runAlarm(storage, at(T + 10 * minute), services.send);
  assert.equal(services.sent.length, 0);
  // A check-in that holds nothing turns the alarm off.
  await checkIn(storage, [], T + 2 * minute);
  assert.equal(storage.alarm, null);
});

test("an alarm before any check-in does nothing", async () => {
  const services = new FakeServices();
  await worker.runAlarm(new FakeStorage(), at(T), services.send);
  assert.equal(services.sent.length, 0);
});

test("an unchanged check-in writes only the check-in time", async () => {
  const storage = new FakeStorage();
  const healthChecks = [healthCheck("app", healthUrl)];
  await checkIn(storage, [alice, bob], T, { healthChecks });
  storage.writtenKeys = [];
  await checkIn(storage, [alice, bob], T + minute, { healthChecks });
  assert.deepEqual(storage.writtenKeys, ["check_in"]);
  const changed = structuredClone(bob);
  changed.request.body = "message=changed";
  storage.writtenKeys = [];
  await checkIn(storage, [alice, changed], T + 2 * minute, { healthChecks });
  assert.deepEqual(storage.writtenKeys.sort(), ["check_in", "held_message:backup-contact-method-2"]);
  const changedCheck = structuredClone(healthChecks[0]);
  changedCheck.url = "https://app.example.com/other-health";
  storage.writtenKeys = [];
  await checkIn(storage, [alice, changed], T + 3 * minute, { healthChecks: [changedCheck] });
  assert.deepEqual(storage.writtenKeys.sort(), ["check_in", "health_check:app"]);
});

test("a check-in removes held messages and health checks that are no longer in it", async () => {
  const storage = new FakeStorage();
  await checkIn(storage, [alice, bob], T, { healthChecks: [healthCheck("app", healthUrl), healthCheck("admin", "https://admin.example.com/health")] });
  await checkIn(storage, [alice], T + minute, { healthChecks: [healthCheck("app", healthUrl)] });
  assert.deepEqual([...(await storage.list({ prefix: "held_message:" })).keys()], ["held_message:primary-contact-method-1"]);
  assert.deepEqual([...(await storage.list({ prefix: "health_check:" })).keys()], ["health_check:app"]);
});

test("a send whose outcome is unknown is sent again", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  await checkIn(storage, [alice], T);
  // The object stops while the request is out, before it records the outcome.
  const evicted = new Error("the Durable Object was evicted");
  await assert.rejects(worker.runAlarm(storage, at(T + 10 * minute), async (request) => {
    services.sent.push(request);
    throw evicted;
  }), evicted);
  // Cloudflare runs the alarm again; the send is due again 30 seconds later.
  await worker.runAlarm(storage, at(T + 10 * minute + 2 * second), services.send);
  assert.equal(services.sent.length, 1);
  await worker.runAlarm(storage, at(T + 10 * minute + 30 * second), services.send);
  assert.equal(services.sent.length, 2, "the message whose outcome was unknown went again");
  const answer = (await checkIn(storage, [alice], T + 15 * minute)).body;
  assert.deepEqual(answer.sends.map((s) => [s.send_outcome, s.attempts]), [["delivered", 2]]);
});

test("a request without an answer, 429, and 5xx are tried again; other refusals are final", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  services.answer(alice.request.url, null, 429, 503, 200);
  services.answer(bob.request.url, 400);
  const quickBob = { ...bob, delay_seconds: 600 };
  await checkIn(storage, [alice, quickBob], T);
  for (let attempt = 0; attempt < 4; attempt++) {
    await worker.runAlarm(storage, at(storage.alarm), services.send);
  }
  const answer = (await checkIn(storage, [], T + 20 * minute)).body;
  const outcomes = Object.fromEntries(answer.sends.map((s) => [s.held_message_id, [s.send_outcome, s.attempts]]));
  assert.deepEqual(outcomes, { "primary-contact-method-1": ["delivered", 4], "backup-contact-method-2": ["refused_by_service", 1] });
});

test("a message that keeps failing is tried every 30 seconds for 10 minutes and then given up", async () => {
  const storage = new FakeStorage();
  const down = new FakeServices();
  down.answer(alice.request.url, 503);
  await checkIn(storage, [alice], T);
  const attemptTimes = [];
  while (storage.alarm !== null) {
    attemptTimes.push(storage.alarm);
    await worker.runAlarm(storage, at(storage.alarm), down.send);
  }
  // At the due time, then every 30 seconds up to 10 minutes after it.
  assert.deepEqual(attemptTimes, Array.from({ length: 21 }, (_, index) => T + 10 * minute + index * 30 * second));
  assert.equal(down.sent.length, 21);
  const finished = (await checkIn(storage, [], T + 30 * minute)).body.sends[0];
  assert.equal(finished.send_outcome, "not_delivered_within_10_minutes");
  assert.equal(finished.http_status_code, 503);
});

test("a request that waited 30 seconds for no answer is tried again 30 seconds after the wait ended", async () => {
  const storage = new FakeStorage();
  await checkIn(storage, [alice], T);
  let now = T + 10 * minute;
  await worker.runAlarm(storage, () => now, async () => {
    now += 30 * second;
    return { http_status_code: null };
  });
  // The attempt started at 03:10:00 and ended at 03:10:30.
  assert.equal(storage.alarm, T + 11 * minute);
});

test("a check-in stops the sends in progress, and a late delivery is still recorded", async () => {
  const storage = new FakeStorage();
  let release;
  const slow = new Promise((resolve) => { release = resolve; });
  await checkIn(storage, [alice], T);
  const alarm = worker.runAlarm(storage, at(T + 10 * minute), () => slow);
  // The server checks in while the request is out.
  await new Promise((resolve) => setTimeout(resolve, 5));
  const during = (await checkIn(storage, [alice], T + 10 * minute + 5 * second)).body;
  assert.equal(during.sends[0].send_outcome, "stopped_by_check_in");
  assert.equal(storage.alarm, T + 10 * minute + 5 * second + 10 * minute, "the alarm no longer retries the stopped send");
  release({ http_status_code: 200 });
  await alarm;
  const after = (await checkIn(storage, [alice], T + 11 * minute)).body;
  assert.equal(after.sends[0].send_outcome, "delivered");
  assert.equal(storage.alarm, T + 21 * minute);
});

test("a check-in stops a send that is waiting to be retried", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  services.answer(alice.request.url, 503);
  await checkIn(storage, [alice], T);
  await worker.runAlarm(storage, at(T + 10 * minute), services.send);
  assert.equal(storage.alarm, T + 10 * minute + 30 * second);
  const answer = (await checkIn(storage, [alice], T + 10 * minute + 10 * second)).body;
  assert.deepEqual(answer.sends.map((s) => [s.send_state, s.send_outcome]), [["finished", "stopped_by_check_in"]]);
  await worker.runAlarm(storage, at(T + 10 * minute + 30 * second), services.send);
  assert.equal(services.sent.length, 1, "no retry after the check-in");
});

test("a held message that a check-in removed is not sent", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  services.answer(alice.request.url, 503);
  await checkIn(storage, [alice, bob], T);
  await worker.runAlarm(storage, at(T + 10 * minute), services.send);
  // Check-ins stop, the send retries, and a sudden check-in drops Alice's message.
  await checkIn(storage, [bob], T + 10 * minute + 5 * second);
  assert.equal(storage.alarm, T + 10 * minute + 5 * second + 20 * minute);
  await worker.runAlarm(storage, at(T + 10 * minute + 30 * second), services.send);
  assert.equal(services.sent.length, 1);
});

test("at most 40 messages go in one alarm run, and the alarm comes back at once for the rest", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  const many = Array.from({ length: 45 }, (_, index) => heldMessage(`primary-contact-method-${index}`, 600, `https://api.pushover.net/1/messages.json?to=${index}`));
  await checkIn(storage, many, T);
  await worker.runAlarm(storage, at(T + 10 * minute), services.send);
  assert.equal(services.sent.length, 40);
  assert.equal(storage.alarm, T + 10 * minute);
  await worker.runAlarm(storage, at(T + 10 * minute + second), services.send);
  assert.equal(services.sent.length, 45);
});

test("a message is attempted while 40 others keep getting no answer", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  const many = Array.from({ length: 41 }, (_, index) => heldMessage(`primary-contact-method-${index}`, 600, `https://api.pushover.net/1/messages.json?to=${index}`));
  for (const message of many.slice(0, 40)) {
    services.answer(message.request.url, null);
  }
  await checkIn(storage, many, T);
  await worker.runAlarm(storage, at(T + 10 * minute), services.send);
  await worker.runAlarm(storage, at(T + 10 * minute + 30 * second), services.send);
  assert.equal(services.sent.filter((r) => r.url === many[40].request.url).length, 1, "the 41st message was attempted in the first two runs");
});

test("more held messages than one storage call takes are stored, each sent once, and removed", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  const many = Array.from({ length: 200 }, (_, index) => heldMessage(`primary-contact-method-${index}`, 600, `https://api.pushover.net/1/messages.json?to=${index}`));
  assert.equal((await checkIn(storage, many, T)).status, 200);
  for (let run = 0; run < 5; run++) {
    await worker.runAlarm(storage, at(T + 10 * minute + run * second), services.send);
  }
  assert.equal(services.sent.length, 200);
  assert.equal(new Set(services.sent.map((r) => r.url)).size, 200);
  assert.equal(storage.alarm, null);
  const answer = (await checkIn(storage, [], T + 20 * minute)).body;
  assert.equal(answer.held_messages_stored, 0);
  assert.equal(answer.sends.length, 200);
  assert.deepEqual([...(await storage.list({ prefix: "held_message:" })).keys()], []);
});

test("the send log keeps 7 days and at most 200 sends", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  await checkIn(storage, [alice], T);
  await worker.runAlarm(storage, at(T + 10 * minute), services.send);
  // 8 days later, a check-in finds the send older than the log keeps.
  const answer = (await checkIn(storage, [alice], T + 8 * 24 * 60 * minute)).body;
  assert.deepEqual(answer.sends.map((s) => s.send_id), [1], "the send of the silence that just ended is still reported");
  const later = (await checkIn(storage, [alice], T + 8 * 24 * 60 * minute + minute)).body;
  assert.deepEqual(later.sends, []);
  assert.deepEqual([...(await storage.list({ prefix: "send:" })).keys()], []);

  const crowded = new FakeStorage();
  for (let id = 1; id <= 210; id++) {
    await crowded.put(`send:${String(id).padStart(12, "0")}`, { send_id: id, held_message_id: "x", silence_started_at: T - 2 * minute, due_at: T - minute, attempts: 1, send_state: "finished", send_outcome: "delivered", http_status_code: 200, finished_at: T - minute });
  }
  await checkIn(crowded, [], T);
  const keys = [...(await crowded.list({ prefix: "send:" })).keys()];
  assert.equal(keys.length, 200);
  assert.equal(keys[0], "send:" + String(11).padStart(12, "0"));
});

// ----------------------------------------------------------- Recovery requests

test("a recovery request is sent once when check-ins resume after the message was delivered", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  await checkIn(storage, [aliceWithRecovery, bobWithRecovery], T);
  await worker.runAlarm(storage, at(T + 10 * minute), services.send);
  // Only Alice's message went out; Bob's delay has not passed.
  assert.equal(services.sent.length, 1);
  const recoveryServices = new FakeServices();
  const resumed = (await checkIn(storage, [aliceWithRecovery, bobWithRecovery], T + 15 * minute, { services: recoveryServices })).body;
  assert.deepEqual(recoveryServices.sent, [aliceRecoveryRequest], "Bob was never alerted, so Bob gets no all-clear");
  assert.deepEqual(resumed.sends.map((s) => [s.held_message_id, s.recovery_sent_at, s.recovery_outcome]), [["primary-contact-method-1", "2026-10-27T03:15:00Z", "delivered"]]);
  // The next check-ins send nothing more.
  const again = new FakeServices();
  await checkIn(storage, [aliceWithRecovery, bobWithRecovery], T + 16 * minute, { services: again });
  await checkIn(storage, [aliceWithRecovery, bobWithRecovery], T + 17 * minute, { services: again });
  assert.equal(again.sent.length, 0);
  const stored = [...(await storage.list({ prefix: "send:" })).values()];
  assert.equal(stored[0].recovery_sent_at, T + 15 * minute);
});

test("a recovery request is sent for each delivered message, and never for a message without one or one that failed", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  const carol = { ...heldMessage("backup-contact-method-3", 600, "https://api.pushover.net/1/messages.json?to=carol"), recovery_request: heldRequest("https://api.pushover.net/1/messages.json?to=carol", "message=back") };
  const dave = { ...heldMessage("backup-contact-method-4", 600, "https://api.pushover.net/1/messages.json?to=dave"), recovery_request: heldRequest("https://api.pushover.net/1/messages.json?to=dave", "message=back") };
  services.answer(dave.request.url, 400);
  const heldMessages = [aliceWithRecovery, { ...bob, delay_seconds: 600 }, carol, dave];
  await checkIn(storage, heldMessages, T);
  await worker.runAlarm(storage, at(T + 10 * minute), services.send);
  const recoveryServices = new FakeServices();
  await checkIn(storage, heldMessages, T + 12 * minute, { services: recoveryServices });
  assert.deepEqual(recoveryServices.sent.map((r) => r.url).sort(), [aliceRecoveryRequest.url, carol.recovery_request.url].sort());
});

test("a recovery request that fails is recorded and not sent again", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  await checkIn(storage, [aliceWithRecovery], T);
  await worker.runAlarm(storage, at(T + 10 * minute), services.send);
  const recoveryServices = new FakeServices();
  recoveryServices.answer(aliceRecoveryRequest.url, 503);
  const resumed = (await checkIn(storage, [aliceWithRecovery], T + 11 * minute, { services: recoveryServices })).body;
  assert.equal(resumed.sends[0].recovery_outcome, "service_unavailable");
  await checkIn(storage, [aliceWithRecovery], T + 12 * minute, { services: recoveryServices });
  assert.equal(recoveryServices.sent.length, 1);
  // A request that throws before answering is also marked as sent before it goes out.
  const storage2 = new FakeStorage();
  await checkIn(storage2, [aliceWithRecovery], T);
  await worker.runAlarm(storage2, at(T + 10 * minute), services.send);
  const exploding = new FakeServices();
  exploding.answer(aliceRecoveryRequest.url, new Error("boom"));
  await assert.rejects(checkIn(storage2, [aliceWithRecovery], T + 11 * minute, { services: exploding }), /boom/);
  const retry = new FakeServices();
  await checkIn(storage2, [aliceWithRecovery], T + 12 * minute, { services: retry });
  assert.equal(retry.sent.length, 0, "a recovery request is never sent twice");
});

test("no recovery request goes out when nothing was delivered or the held message has none", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  await checkIn(storage, [aliceWithRecovery], T);
  // The server comes back before the delay.
  const early = new FakeServices();
  await checkIn(storage, [aliceWithRecovery], T + minute, { services: early });
  assert.equal(early.sent.length, 0);
  // A delivered message without a recovery request sends none.
  const plain = new FakeStorage();
  await checkIn(plain, [alice], T);
  await worker.runAlarm(plain, at(T + 10 * minute), services.send);
  const resumed = new FakeServices();
  await checkIn(plain, [alice], T + 11 * minute, { services: resumed });
  assert.equal(resumed.sent.length, 0);
});

test("a recovery request is sent after a silence of more than 7 days too", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  await checkIn(storage, [aliceWithRecovery], T);
  await worker.runAlarm(storage, at(T + 10 * minute), services.send);
  const recoveryServices = new FakeServices();
  const resumed = (await checkIn(storage, [aliceWithRecovery], T + 9 * 24 * 60 * minute, { services: recoveryServices })).body;
  assert.equal(recoveryServices.sent.length, 1);
  assert.equal(resumed.sends[0].recovery_outcome, "delivered");
});

test("a recovery request uses the held message as it was stored during the silence", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  await checkIn(storage, [aliceWithRecovery], T);
  await worker.runAlarm(storage, at(T + 10 * minute), services.send);
  // The server's first check-in after the silence drops the message; its recovery request still goes.
  const recoveryServices = new FakeServices();
  await checkIn(storage, [], T + 11 * minute, { services: recoveryServices });
  assert.deepEqual(recoveryServices.sent, [aliceRecoveryRequest]);
});

// ------------------------------------------------------------- Health checks

const alertUrl = "https://api.pushover.net/1/messages.json?to=alerts";

async function healthRun(storage, probe, services, now) {
  return worker.runHealthChecks(storage, at(now), probe.fetch, services.send);
}

test("runHealthChecks with no stored health checks does nothing", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  assert.deepEqual(await healthRun(storage, probe, services, T), { checked: 0, results: [] });
  await checkIn(storage, [alice], T);
  assert.deepEqual(await healthRun(storage, probe, services, T), { checked: 0, results: [] });
  assert.equal(probe.requested.length, 0);
});

test("one failed probe does not alert; the second in a row alerts once; the third does not alert again", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  probe.answer(healthUrl, 503);
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl, { recovery_request: healthRecoveryRequest })] });

  const first = await healthRun(storage, probe, services, T + 5 * minute);
  assert.equal(first.checked, 1);
  assert.deepEqual(first.results[0], { check_id: "app", healthy: false, http_status_code: 503, consecutive_failures: 1, alerting: false, alert_outcome: null, recovery_outcome: null });
  assert.equal(services.sent.length, 0);

  const second = await healthRun(storage, probe, services, T + 10 * minute);
  assert.deepEqual(second.results[0], { check_id: "app", healthy: false, http_status_code: 503, consecutive_failures: 2, alerting: true, alert_outcome: "delivered", recovery_outcome: null });
  assert.deepEqual(services.sent.map((r) => [r.url, r.body]), [[alertUrl, "message=app+is+down"]]);
  const state = await storage.get("health_state:app");
  assert.deepEqual(state, { consecutive_failures: 2, alerting: true, alerted_at: T + 10 * minute, last_status_code: 503, last_checked_at: T + 10 * minute, last_alert_outcome: "delivered", last_recovery_outcome: null });

  const third = await healthRun(storage, probe, services, T + 15 * minute);
  assert.equal(third.results[0].consecutive_failures, 3);
  assert.equal(third.results[0].alert_outcome, null);
  await healthRun(storage, probe, services, T + 20 * minute);
  assert.equal(services.sent.length, 1, "one alert for the whole outage");
});

test("a healthy probe resets the count, so failures that are not in a row never alert", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl)] });
  for (const [index, status] of [500, 200, 500, 200, 500].entries()) {
    probe.answer(healthUrl, status);
    await healthRun(storage, probe, services, T + (index + 1) * 5 * minute);
  }
  assert.equal(services.sent.length, 0);
  assert.equal((await storage.get("health_state:app")).consecutive_failures, 1);
});

test("recovery resets the count and sends the recovery request once", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl, { recovery_request: healthRecoveryRequest })] });
  probe.answer(healthUrl, 500);
  await healthRun(storage, probe, services, T + 5 * minute);
  await healthRun(storage, probe, services, T + 10 * minute);
  assert.equal(services.sent.length, 1);

  probe.answer(healthUrl, 204);
  const recovered = await healthRun(storage, probe, services, T + 15 * minute);
  assert.deepEqual(recovered.results[0], { check_id: "app", healthy: true, http_status_code: 204, consecutive_failures: 0, alerting: false, alert_outcome: null, recovery_outcome: "delivered" });
  assert.deepEqual(services.sent.map((r) => r.body), ["message=app+is+down", "message=app+is+back"]);
  assert.equal((await storage.get("health_state:app")).last_recovery_outcome, "delivered");

  await healthRun(storage, probe, services, T + 20 * minute);
  await healthRun(storage, probe, services, T + 25 * minute);
  assert.equal(services.sent.length, 2, "healthy runs after the recovery send nothing");

  // A new outage alerts again, after two failures.
  probe.answer(healthUrl, 500);
  await healthRun(storage, probe, services, T + 30 * minute);
  assert.equal(services.sent.length, 2);
  await healthRun(storage, probe, services, T + 35 * minute);
  assert.equal(services.sent.length, 3);
});

test("a check without a recovery request recovers quietly and can alert again later", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl, { failures_before_alert: 1 })] });
  probe.answer(healthUrl, 500);
  await healthRun(storage, probe, services, T + 5 * minute);
  assert.equal(services.sent.length, 1, "failures_before_alert of 1 alerts at the first failure");
  probe.answer(healthUrl, 200);
  const recovered = await healthRun(storage, probe, services, T + 10 * minute);
  assert.equal(recovered.results[0].alerting, false);
  assert.equal(services.sent.length, 1);
  probe.answer(healthUrl, 500);
  await healthRun(storage, probe, services, T + 15 * minute);
  assert.equal(services.sent.length, 2);
});

test("failures_before_alert sets how many failures in a row alert", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl, { failures_before_alert: 4 })] });
  probe.answer(healthUrl, 500);
  for (let run = 1; run <= 3; run++) {
    await healthRun(storage, probe, services, T + run * 5 * minute);
  }
  assert.equal(services.sent.length, 0);
  await healthRun(storage, probe, services, T + 20 * minute);
  assert.equal(services.sent.length, 1);
});

test("a network error or a timeout counts as a failed probe", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl)] });
  probe.answer(healthUrl, new TypeError("network"), new DOMException("The operation timed out.", "TimeoutError"), 200);
  const first = await healthRun(storage, probe, services, T + 5 * minute);
  assert.equal(first.results[0].http_status_code, null);
  assert.equal(first.results[0].consecutive_failures, 1);
  const second = await healthRun(storage, probe, services, T + 10 * minute);
  assert.equal(second.results[0].alerting, true);
  assert.equal(services.sent.length, 1);
  assert.equal((await storage.get("health_state:app")).last_status_code, null);
});

test("a non-2xx answer, including a redirect, counts as a failed probe", async () => {
  for (const status of [301, 302, 404, 500, 503]) {
    const storage = new FakeStorage();
    const probe = new FakeProbe();
    const services = new FakeServices();
    await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl)] });
    probe.answer(healthUrl, status);
    await healthRun(storage, probe, services, T + 5 * minute);
    await healthRun(storage, probe, services, T + 10 * minute);
    assert.equal(services.sent.length, 1, `HTTP ${status}`);
    assert.equal((await storage.get("health_state:app")).last_status_code, status);
  }
  for (const status of [200, 201, 204, 299]) {
    const storage = new FakeStorage();
    const probe = new FakeProbe();
    const services = new FakeServices();
    await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl, { failures_before_alert: 1 })] });
    probe.answer(healthUrl, status);
    await healthRun(storage, probe, services, T + 5 * minute);
    assert.equal(services.sent.length, 0, `HTTP ${status}`);
  }
});

test("the probe is a GET that follows no redirect, has a time limit, and a body that is dropped", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl, { timeout_ms: 1500 })] });
  await healthRun(storage, probe, new FakeServices(), T + 5 * minute);
  const { url, options } = probe.requested[0];
  assert.equal(url, healthUrl);
  assert.equal(options.method, "GET");
  assert.equal(options.redirect, "manual");
  assert.ok(options.signal instanceof AbortSignal);
  assert.equal(options.body, undefined);
  let cancelled = false;
  const status = await worker.probeOnce({ url: healthUrl, timeout_ms: 1000 }, async () => ({ status: 200, body: { cancel: async () => { cancelled = true; } } }));
  assert.equal(status, 200);
  assert.equal(cancelled, true);
  // A body that will not cancel does not change the answer.
  assert.equal(await worker.probeOnce({ url: healthUrl, timeout_ms: 1000 }, async () => ({ status: 200, body: { cancel: async () => { throw new Error("stuck"); } } })), 200);
});

test("a probe really stops at its time limit", async () => {
  const slow = (url, options) => new Promise((resolve, reject) => options.signal.addEventListener("abort", () => reject(options.signal.reason)));
  // Node's timeout timer does not keep the process alive, so this one does until the probe ends.
  const keepAlive = setTimeout(() => {}, 10000);
  const started = Date.now();
  try {
    assert.equal(await worker.probeOnce({ url: healthUrl, timeout_ms: 1000 }, slow), null);
  } finally {
    clearTimeout(keepAlive);
  }
  assert.ok(Date.now() - started < 3000);
});

test("each health check keeps its own count and state", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  const adminUrl = "https://admin.example.com/health";
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl), healthCheck("admin", adminUrl, { alert_request: heldRequest("https://hooks.slack.com/services/T0/B0/x", "{\"text\":\"admin down\"}") })] });
  probe.answer(adminUrl, 500);
  await healthRun(storage, probe, services, T + 5 * minute);
  const second = await healthRun(storage, probe, services, T + 10 * minute);
  assert.deepEqual(second.results.map((r) => [r.check_id, r.alerting]), [["app", false], ["admin", true]]);
  assert.deepEqual(services.sent.map((r) => r.url), ["https://hooks.slack.com/services/T0/B0/x"]);
});

test("the state is saved before the alert goes out, so a run that stops never alerts twice", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl, { failures_before_alert: 1 })] });
  probe.answer(healthUrl, 500);
  let seenWhileSending = null;
  const evicted = new Error("the Durable Object was evicted");
  await assert.rejects(worker.runHealthChecks(storage, at(T + 5 * minute), probe.fetch, async () => {
    seenWhileSending = await storage.get("health_state:app");
    throw evicted;
  }), evicted);
  assert.equal(seenWhileSending.alerting, true);
  const services = new FakeServices();
  await healthRun(storage, probe, services, T + 10 * minute);
  assert.equal(services.sent.length, 0);
  // The same holds for the recovery request.
  probe.answer(healthUrl, 200);
  await checkIn(storage, [], T + 11 * minute, { healthChecks: [healthCheck("app", healthUrl, { failures_before_alert: 1, recovery_request: healthRecoveryRequest })] });
  let stateWhileRecovering = null;
  await assert.rejects(worker.runHealthChecks(storage, at(T + 15 * minute), probe.fetch, async () => {
    stateWhileRecovering = await storage.get("health_state:app");
    throw evicted;
  }), evicted);
  assert.equal(stateWhileRecovering.alerting, false);
  await healthRun(storage, probe, services, T + 20 * minute);
  assert.equal(services.sent.length, 0);
});

test("an alert or a recovery request the service could not take is tried again at the next run", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl, { failures_before_alert: 1, recovery_request: healthRecoveryRequest })] });
  probe.answer(healthUrl, 500);
  services.answer(alertUrl, 503, null, 200);
  const first = await healthRun(storage, probe, services, T + 5 * minute);
  assert.deepEqual([first.results[0].alerting, first.results[0].alert_outcome], [false, "service_unavailable"]);
  assert.equal((await storage.get("health_state:app")).last_alert_outcome, "service_unavailable");
  const second = await healthRun(storage, probe, services, T + 10 * minute);
  assert.equal(second.results[0].alert_outcome, "no_answer");
  const third = await healthRun(storage, probe, services, T + 15 * minute);
  assert.deepEqual([third.results[0].alerting, third.results[0].alert_outcome], [true, "delivered"]);
  await healthRun(storage, probe, services, T + 20 * minute);
  assert.equal(services.sent.length, 3);

  // A recovery request that gets 503 is tried at the next healthy run.
  probe.answer(healthUrl, 200);
  services.answer(alertUrl, 503, 200);
  const recovering = await healthRun(storage, probe, services, T + 25 * minute);
  assert.deepEqual([recovering.results[0].alerting, recovering.results[0].recovery_outcome], [true, "service_unavailable"]);
  const recovered = await healthRun(storage, probe, services, T + 30 * minute);
  assert.deepEqual([recovered.results[0].alerting, recovered.results[0].recovery_outcome], [false, "delivered"]);
  assert.equal(services.sent.length, 5);
  await healthRun(storage, probe, services, T + 35 * minute);
  assert.equal(services.sent.length, 5);
});

test("a service that refuses an alert for good is not asked again", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl, { failures_before_alert: 1 })] });
  probe.answer(healthUrl, 500);
  services.answer(alertUrl, 400);
  await healthRun(storage, probe, services, T + 5 * minute);
  await healthRun(storage, probe, services, T + 10 * minute);
  assert.equal(services.sent.length, 1);
  assert.equal((await storage.get("health_state:app")).last_alert_outcome, "refused_by_service");
});

test("the state of a health check that a later check-in removed is deleted", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  const adminUrl = "https://admin.example.com/health";
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl), healthCheck("admin", adminUrl)] });
  probe.answer(adminUrl, 500);
  await healthRun(storage, probe, services, T + 5 * minute);
  assert.deepEqual([...(await storage.list({ prefix: "health_state:" })).keys()], ["health_state:admin", "health_state:app"]);
  await checkIn(storage, [], T + 6 * minute, { healthChecks: [healthCheck("app", healthUrl)] });
  assert.deepEqual([...(await storage.list({ prefix: "health_state:" })).keys()], ["health_state:app"]);
  const run = await healthRun(storage, probe, services, T + 10 * minute);
  assert.deepEqual(run.results.map((r) => r.check_id), ["app"]);
  assert.equal(probe.requested.filter((r) => r.url === adminUrl).length, 1, "the removed check is no longer probed");
  // A check-in with no health checks removes every state.
  await checkIn(storage, [], T + 11 * minute);
  assert.deepEqual([...(await storage.list({ prefix: "health_state:" })).keys()], []);
  assert.deepEqual(await healthRun(storage, probe, services, T + 15 * minute), { checked: 0, results: [] });
  // A check that comes back starts with a fresh count.
  await checkIn(storage, [], T + 16 * minute, { healthChecks: [healthCheck("admin", adminUrl)] });
  const fresh = await healthRun(storage, probe, services, T + 20 * minute);
  assert.equal(fresh.results[0].consecutive_failures, 1);
});

test("a failure count survives a check-in that keeps the same check", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  probe.answer(healthUrl, 500);
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl)] });
  await healthRun(storage, probe, services, T + 5 * minute);
  await checkIn(storage, [], T + 6 * minute, { healthChecks: [healthCheck("app", healthUrl)] });
  await healthRun(storage, probe, services, T + 10 * minute);
  assert.equal(services.sent.length, 1);
});

test("health checks run while check-ins are silent, and alert on their own", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  await checkIn(storage, [alice], T, { healthChecks: [healthCheck("app", healthUrl)] });
  probe.answer(healthUrl, 502);
  await healthRun(storage, probe, services, T + 5 * minute);
  await healthRun(storage, probe, services, T + 10 * minute);
  assert.deepEqual(services.sent.map((r) => r.url), [alertUrl]);
  assert.equal(storage.alarm, T + 10 * minute, "the dead man's switch alarm is unaffected");
});

test("a run keeps inside the 50 outgoing requests of the Workers Free plan", async () => {
  const storage = new FakeStorage();
  const probe = new FakeProbe();
  const services = new FakeServices();
  const many = Array.from({ length: 32 }, (_, index) => healthCheck(`app-${index}`, `https://app${index}.example.com/health`, { failures_before_alert: 1 }));
  await checkIn(storage, [], T, { healthChecks: many });
  for (const check of many) {
    probe.answer(check.url, 500);
  }
  const first = await healthRun(storage, probe, services, T + 5 * minute);
  assert.equal(probe.requested.length, 32);
  assert.equal(services.sent.length, worker.HEALTH_REQUESTS_PER_RUN);
  assert.ok(probe.requested.length + services.sent.length <= 50);
  assert.equal(first.results.filter((r) => r.alerting).length, worker.HEALTH_REQUESTS_PER_RUN);
  // The checks that were not served keep their failures and are alerted in the next runs.
  assert.ok(first.results.every((r) => r.consecutive_failures === 1));
  await healthRun(storage, probe, services, T + 10 * minute);
  await healthRun(storage, probe, services, T + 15 * minute);
  await healthRun(storage, probe, services, T + 20 * minute);
  assert.equal(services.sent.length, 32);
  assert.equal(new Set(services.sent.map((r) => r.body)).size, 32, "each check alerted exactly once");
  await healthRun(storage, probe, services, T + 25 * minute);
  assert.equal(services.sent.length, 32);
});

// --------------------------------------------------------------------- Tests

test("send-test sends each test request now and logs nothing", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  services.answer(bob.request.url, 503);
  await checkIn(storage, [alice, bob], T);
  const response = await worker.handleObjectRequest(new Request(objectAddress + "/send-test", { method: "POST" }), storage, at(T), services.send, null);
  assert.deepEqual(await response.json(), { test_sends: [
    { held_message_id: "primary-contact-method-1", send_outcome: "delivered", http_status_code: 200 },
    { held_message_id: "backup-contact-method-2", send_outcome: "service_unavailable", http_status_code: 503 },
  ] });
  assert.deepEqual(services.sent.map((r) => r.body), ["message=Solo+outside+alarm+test", "message=Solo+outside+alarm+test"]);
  assert.deepEqual((await checkIn(storage, [alice, bob], T + minute)).body.sends, []);
});

test("send-test names every kind of answer, and sends nothing before a check-in", async () => {
  const services = new FakeServices();
  assert.deepEqual(await worker.sendTests(new FakeStorage(), services.send), []);
  const storage = new FakeStorage();
  const refused = heldMessage("backup-contact-method-3", 600, "https://api.pushover.net/1/messages.json?to=carol");
  const silent = heldMessage("backup-contact-method-4", 600, "https://api.pushover.net/1/messages.json?to=dave");
  services.answer(refused.request.url, 400);
  services.answer(silent.request.url, null);
  await checkIn(storage, [refused, silent], T);
  assert.deepEqual((await worker.sendTests(storage, services.send)).map((s) => [s.send_outcome, s.http_status_code]), [["refused_by_service", 400], ["no_answer", null]]);
  // Test requests do not include recovery requests.
  const withRecovery = new FakeStorage();
  const recoveryServices = new FakeServices();
  await checkIn(withRecovery, [aliceWithRecovery], T);
  await worker.sendTests(withRecovery, recoveryServices.send);
  assert.deepEqual(recoveryServices.sent, [aliceWithRecovery.test_request]);
});

const receipt = "r5qkvhu7wg8wwtbm4l5x2dnxzy9c3a";

test("send-test returns the Pushover receipt of an accepted emergency notification, and only a valid one", async () => {
  const storage = new FakeStorage();
  const services = new FakeServices();
  const carol = heldMessage("backup-contact-method-3", 1200, "https://api.pushover.net/1/messages.json?to=carol");
  const dave = heldMessage("backup-contact-method-4", 1200, "https://api.pushover.net/1/messages.json?to=dave");
  const erin = heldMessage("backup-contact-method-5", 1200, "https://api.pushover.net/1/messages.json?to=erin");
  services.answerTexts.set(alice.request.url, JSON.stringify({ status: 1, request: "abc", receipt }));
  services.answerTexts.set(bob.request.url, JSON.stringify({ status: 1, request: "abc" }));
  services.answerTexts.set(carol.request.url, JSON.stringify({ status: 1, receipt: "not-a-receipt" }));
  services.answerTexts.set(dave.request.url, "not JSON");
  services.answerTexts.set(erin.request.url, JSON.stringify({ status: 0, receipt }));
  services.answer(erin.request.url, 400);
  await checkIn(storage, [alice, bob, carol, dave, erin], T);
  const response = await worker.handleObjectRequest(new Request(objectAddress + "/send-test", { method: "POST" }), storage, at(T), services.send, null);
  assert.deepEqual((await response.json()).test_sends.map((s) => s.pushover_receipt ?? null), [receipt, null, null, null, null]);
  assert.equal(worker.pushoverReceiptIn(JSON.stringify({ receipt })), receipt);
  assert.equal(worker.pushoverReceiptIn(JSON.stringify({ receipt: receipt + "x" })), null);
  assert.equal(worker.pushoverReceiptIn(JSON.stringify({ receipt: 5 })), null);
  assert.equal(worker.pushoverReceiptIn("null"), null);
});

test("send-test through the Worker reaches the Durable Object and returns the receipt", async () => {
  const { env, storage } = workerSetUp();
  await checkIn(storage, [alice], T);
  const fetchedUrls = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = async (url) => {
    fetchedUrls.push(String(url));
    return new Response(JSON.stringify({ status: 1, receipt }), { status: 200 });
  };
  try {
    const response = await worker.default.fetch(externalRequest("/send-test", TOKEN), env);
    assert.equal(response.status, 200);
    assert.deepEqual(await response.json(), { test_sends: [{ held_message_id: "primary-contact-method-1", send_outcome: "delivered", http_status_code: 200, pushover_receipt: receipt }] });
    assert.deepEqual(fetchedUrls, [alice.test_request.url]);
  } finally {
    globalThis.fetch = realFetch;
  }
});

test("the scheduled run reaches the Durable Object, probes with fetch, and alerts", async () => {
  const { env, storage } = workerSetUp();
  await checkIn(storage, [], T, { healthChecks: [healthCheck("app", healthUrl, { failures_before_alert: 1 })] });
  const fetched = [];
  const realFetch = globalThis.fetch;
  globalThis.fetch = async (url, options) => {
    fetched.push({ url: String(url), method: options.method });
    return new Response(null, { status: String(url) === healthUrl ? 500 : 200 });
  };
  try {
    await worker.default.scheduled({}, env, undefined);
  } finally {
    globalThis.fetch = realFetch;
  }
  assert.deepEqual(fetched, [{ url: healthUrl, method: "GET" }, { url: alertUrl, method: "POST" }]);
  assert.equal((await storage.get("health_state:app")).alerting, true);
});

test("the Durable Object's alarm sends the held messages that are due", async () => {
  const storage = new FakeStorage();
  const object = new worker.SoloOutsideAlarm({ storage }, {});
  const realNow = Date.now;
  const realFetch = globalThis.fetch;
  const fetched = [];
  globalThis.fetch = async (url, options) => {
    fetched.push({ url: String(url), method: options.method, body: options.body });
    return new Response("{}", { status: 200 });
  };
  try {
    Date.now = () => T;
    const body = JSON.stringify({ held_messages: [{ ...alice, delay_seconds: 180 }] });
    assert.equal((await object.fetch(new Request(objectAddress + "/check-in", { method: "POST", body }))).status, 200);
    assert.equal(storage.alarm, T + 3 * minute);
    Date.now = () => T + 3 * minute;
    await object.alarm();
  } finally {
    Date.now = realNow;
    globalThis.fetch = realFetch;
  }
  assert.deepEqual(fetched, [{ url: alice.request.url, method: "POST", body: alice.request.body }]);
});

test("sendOnce reads the answer only when asked, and only its start", async () => {
  const answer = JSON.stringify({ status: 1, receipt });
  assert.deepEqual(await worker.sendOnce(alice.request, async () => new Response(answer, { status: 200 })), { http_status_code: 200 });
  assert.deepEqual(await worker.sendOnce(alice.request, async () => new Response(answer, { status: 200 }), true), { http_status_code: 200, answer_text: answer });
  const long = await worker.sendOnce(alice.request, async () => new Response("x".repeat(worker.MAXIMUM_TEST_ANSWER_CHARACTERS + 10), { status: 200 }), true);
  assert.equal(long.answer_text.length, worker.MAXIMUM_TEST_ANSWER_CHARACTERS);
});

test("sendOnce reports no answer when the request fails, and never follows a redirect", async () => {
  let options;
  const answered = await worker.sendOnce(alice.request, async (url, given) => { options = given; return new Response("", { status: 302 }); });
  assert.deepEqual(answered, { http_status_code: 302 });
  assert.equal(options.redirect, "manual");
  assert.equal(options.body, alice.request.body);
  assert.deepEqual(await worker.sendOnce(alice.request, async () => { throw new TypeError("network"); }), { http_status_code: null });
  // A GET carries no body.
  let getOptions;
  await worker.sendOnce({ ...alice.request, method: "GET" }, async (url, given) => { getOptions = given; return new Response("", { status: 200 }); });
  assert.equal(getOptions.body, undefined);
});

test("nextWakeUp is null with no check-in and the earliest due time otherwise", () => {
  assert.equal(worker.nextWakeUp(undefined, new Map(), []), null);
  const checkInRecord = { accepted_at: T };
  const held = new Map([["a", { delay_seconds: 600 }], ["b", { delay_seconds: 300 }]]);
  assert.equal(worker.nextWakeUp(checkInRecord, held, []), T + 5 * minute);
  assert.equal(worker.nextWakeUp(checkInRecord, held, [{ held_message_id: "b", silence_started_at: T, send_state: "sending", next_attempt_at: T + 330 * second }]), T + 330 * second);
  assert.equal(worker.nextWakeUp(checkInRecord, held, [{ held_message_id: "b", silence_started_at: T, send_state: "finished" }, { held_message_id: "a", silence_started_at: T, send_state: "finished" }]), null);
});
