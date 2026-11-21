// The Solo outside alarm: a Cloudflare Worker with one SQLite-backed Durable
// Object, in your own Cloudflare account (the Free plan is enough). It watches
// your server from outside, so it still works when the server is frozen, out of
// memory, or offline. It does two jobs:
//
// 1. Dead man's switch. Your server checks in every minute with its "held
//    messages": ready-made HTTPS requests (to Pushover, Slack, Discord,
//    Telegram, or any webhook), each with a delay. Every check-in moves the
//    Durable Object's alarm. When the check-ins stop, the alarm sends each
//    held message after its delay. When check-ins start again, the Worker
//    sends each delivered message's optional "recovery request" once.
// 2. Edge health checks. A Cron Trigger (set to every 5 minutes when you upload
//    this file) asks the Durable Object to request each health check URL from
//    Cloudflare's network. A check that fails several times in a row sends its
//    alert request once; the first healthy answer after that sends its
//    recovery request once.
//
// The file is uploaded through Cloudflare's API as one ES module. It needs no
// build step and no npm package. The logic lives in plain functions that take
// the storage, the clock, and the send and probe functions, so
// solo_outside_alarm_worker_test.mjs runs every path under `node --test` with
// a fake storage, a fake clock, and a fake fetch. docs/design.md describes the
// behavior.

import { DurableObject } from "cloudflare:workers";

export const CHECK_IN_TOKEN_SECRET = "SOLO_OUTSIDE_ALARM_TOKEN";
export const DURABLE_OBJECT_NAME = "solo-outside-alarm";
// The Worker asks its own Durable Object to run the health checks through
// this address. Only the Worker's scheduled() handler calls it: the public
// fetch handler answers any path other than /check-in and /send-test with 404.
export const RUN_HEALTH_CHECKS_URL = "https://solo-outside-alarm.internal/internal/run-health-checks";

// A check-in of 1 MiB holds 64 held messages at their largest, and hundreds
// at their real size of about 1 KiB; the bound keeps one faulty check-in
// from filling the object's storage.
export const MAXIMUM_CHECK_IN_BYTES = 1024 * 1024;
// Each held message and each health check has at most three requests, and a
// request's URL can take 8 KiB; 16 KiB leaves room for headers and bodies
// without letting one entry take much of the check-in.
export const MAXIMUM_HELD_MESSAGE_BYTES = 16 * 1024;
export const MAXIMUM_HEALTH_CHECK_BYTES = 16 * 1024;
// Three minutes: the server checks in every minute and at once when it starts,
// so a reboot leaves at most about 2.5 minutes without a check-in, and a
// shorter delay would alert you for a reboot.
export const MINIMUM_DELAY_SECONDS = 180;
// The largest 32-bit integer, so every program's integer type holds it.
export const MAXIMUM_DELAY_SECONDS = 2147483647;
// The Workers Free plan allows 50 outgoing requests per invocation. A run of
// health checks makes one probe request per check, so at most 32 checks
// leave room for the alert and recovery requests below.
export const MAXIMUM_HEALTH_CHECKS = 32;
// Alert and recovery requests that one health check run may send. With 32
// probes this is at most 42 outgoing requests, inside the Free plan's 50. A
// check that is not served in this run waits for the next run, 5 minutes
// later, with its failure count kept.
export const HEALTH_REQUESTS_PER_RUN = 10;
export const MINIMUM_HEALTH_CHECK_TIMEOUT_MS = 1000;
export const MAXIMUM_HEALTH_CHECK_TIMEOUT_MS = 15000;
export const DEFAULT_HEALTH_CHECK_TIMEOUT_MS = 10000;
export const MINIMUM_FAILURES_BEFORE_ALERT = 1;
export const MAXIMUM_FAILURES_BEFORE_ALERT = 10;
// Two failed probes in a row, 5 minutes apart, skip the short blip of a
// restart or a deploy while still alerting within about 10 minutes.
export const DEFAULT_FAILURES_BEFORE_ALERT = 2;
// The Workers Free plan allows 50 outgoing requests per invocation; the
// rest of the due messages go in the next alarm, set for at once.
export const SENDS_PER_ALARM = 40;
export const RETRY_INTERVAL_MS = 30 * 1000;
// A message that no service accepted within 10 minutes of its due time is
// given up; by then it is no longer a timely wake-up.
export const SEND_WINDOW_MS = 10 * 60 * 1000;
export const ANSWER_TIME_LIMIT_MS = 30 * 1000;
// The send log keeps 7 days, at most 200 sends: the server reads it from the
// answer to a check-in, and it is there for people to read.
export const SEND_LOG_MS = 7 * 24 * 60 * 60 * 1000;
export const SEND_LOG_ENTRIES = 200;
// Cloudflare's storage API takes at most 128 keys in one put or delete
// (https://developers.cloudflare.com/durable-objects/api/sqlite-storage-api/#put),
// so a larger write goes in parts.
export const KEYS_PER_STORAGE_CALL = 128;

const FORBIDDEN_HEADERS = new Set(["host", "content-length", "transfer-encoding", "connection", "keep-alive", "proxy-authorization", "proxy-connection", "te", "trailer", "upgrade"]);
const HEADER_NAME = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;
// RFC 9110 allows a tab in a field value and no other control character.
const HEADER_VALUE_CONTROL_CHARACTER = /[\x00-\x08\x0a-\x1f\x7f]/;
const HELD_MESSAGE_ID = /^[a-z0-9-]{1,128}$/;
const HEALTH_CHECK_ID = /^[a-z0-9-]{1,64}$/;
// Pushover's receipt for an emergency notification: 30 letters and digits.
const PUSHOVER_RECEIPT = /^[A-Za-z0-9]{30}$/;
// A service's answer is read only for a test, and only this much of it: the
// receipt of an emergency notification is a short JSON object.
export const MAXIMUM_TEST_ANSWER_CHARACTERS = 4096;

export class CheckInRefused extends Error {
  constructor(field, message) {
    super(message);
    this.field = field;
  }
}

function isPlainObject(value) {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function checkKeys(value, requiredKeys, optionalKeys, field) {
  if (!isPlainObject(value)) {
    throw new CheckInRefused(field, "must be a JSON object");
  }
  for (const key of Object.keys(value)) {
    if (!requiredKeys.includes(key) && !optionalKeys.includes(key)) {
      throw new CheckInRefused(field, `has an unknown field ${JSON.stringify(key)}`);
    }
  }
  for (const key of requiredKeys) {
    if (!(key in value)) {
      throw new CheckInRefused(`${field}.${key}`, "is required");
    }
  }
}

function checkHttpsUrl(value, maximumLength, field) {
  let url;
  try {
    url = typeof value === "string" && value.length <= maximumLength ? new URL(value) : null;
  } catch {
    url = null;
  }
  if (!url || url.protocol !== "https:" || url.username || url.password || value.includes("#")) {
    throw new CheckInRefused(field, `must be an https:// URL of at most ${maximumLength} characters with no user name, password, or # part`);
  }
}

function checkHeldRequest(request, field) {
  checkKeys(request, ["method", "url", "headers", "body"], [], field);
  if (!["GET", "POST", "PUT", "PATCH"].includes(request.method)) {
    throw new CheckInRefused(`${field}.method`, "must be GET, POST, PUT, or PATCH");
  }
  checkHttpsUrl(request.url, 8192, `${field}.url`);
  if (!isPlainObject(request.headers)) {
    throw new CheckInRefused(`${field}.headers`, "must be a JSON object of header names and values");
  }
  for (const [name, value] of Object.entries(request.headers)) {
    if (!HEADER_NAME.test(name) || FORBIDDEN_HEADERS.has(name.toLowerCase())) {
      throw new CheckInRefused(`${field}.headers`, "has a header name that is not allowed");
    }
    if (typeof value !== "string" || HEADER_VALUE_CONTROL_CHARACTER.test(value)) {
      throw new CheckInRefused(`${field}.headers`, "has a header value with a control character other than a tab");
    }
  }
  if (typeof request.body !== "string") {
    throw new CheckInRefused(`${field}.body`, "must be a string");
  }
}

function checkSize(value, maximumBytes, field) {
  if (new TextEncoder().encode(JSON.stringify(value)).length > maximumBytes) {
    throw new CheckInRefused(field, `must be at most ${maximumBytes} bytes in JSON`);
  }
}

function checkHeldMessage(message, field) {
  checkKeys(message, ["held_message_id", "delay_seconds", "request", "test_request"], ["recovery_request"], field);
  if (typeof message.held_message_id !== "string" || !HELD_MESSAGE_ID.test(message.held_message_id)) {
    throw new CheckInRefused(`${field}.held_message_id`, "must have 1 to 128 lowercase letters, digits, or hyphens");
  }
  if (!Number.isInteger(message.delay_seconds) || message.delay_seconds < MINIMUM_DELAY_SECONDS || message.delay_seconds > MAXIMUM_DELAY_SECONDS) {
    throw new CheckInRefused(`${field}.delay_seconds`, `must be a whole number from ${MINIMUM_DELAY_SECONDS} to ${MAXIMUM_DELAY_SECONDS}`);
  }
  checkHeldRequest(message.request, `${field}.request`);
  checkHeldRequest(message.test_request, `${field}.test_request`);
  if ("recovery_request" in message) {
    checkHeldRequest(message.recovery_request, `${field}.recovery_request`);
  }
  checkSize(message, MAXIMUM_HELD_MESSAGE_BYTES, field);
}

function checkWholeNumber(value, minimum, maximum, field) {
  if (!Number.isInteger(value) || value < minimum || value > maximum) {
    throw new CheckInRefused(field, `must be a whole number from ${minimum} to ${maximum}`);
  }
}

// checkHealthCheck returns the health check with its defaults filled in, so
// two check-ins that say the same thing store the same bytes.
function checkHealthCheck(check, field) {
  checkKeys(check, ["check_id", "url", "alert_request"], ["timeout_ms", "failures_before_alert", "recovery_request"], field);
  if (typeof check.check_id !== "string" || !HEALTH_CHECK_ID.test(check.check_id)) {
    throw new CheckInRefused(`${field}.check_id`, "must have 1 to 64 lowercase letters, digits, or hyphens");
  }
  checkHttpsUrl(check.url, 2048, `${field}.url`);
  const timeoutMs = check.timeout_ms ?? DEFAULT_HEALTH_CHECK_TIMEOUT_MS;
  checkWholeNumber(timeoutMs, MINIMUM_HEALTH_CHECK_TIMEOUT_MS, MAXIMUM_HEALTH_CHECK_TIMEOUT_MS, `${field}.timeout_ms`);
  const failuresBeforeAlert = check.failures_before_alert ?? DEFAULT_FAILURES_BEFORE_ALERT;
  checkWholeNumber(failuresBeforeAlert, MINIMUM_FAILURES_BEFORE_ALERT, MAXIMUM_FAILURES_BEFORE_ALERT, `${field}.failures_before_alert`);
  checkHeldRequest(check.alert_request, `${field}.alert_request`);
  if ("recovery_request" in check) {
    checkHeldRequest(check.recovery_request, `${field}.recovery_request`);
  }
  checkSize(check, MAXIMUM_HEALTH_CHECK_BYTES, field);
  return {
    check_id: check.check_id,
    url: check.url,
    timeout_ms: timeoutMs,
    failures_before_alert: failuresBeforeAlert,
    alert_request: check.alert_request,
    ...("recovery_request" in check ? { recovery_request: check.recovery_request } : {}),
  };
}

// checkCheckIn returns { held_messages, health_checks } from a check-in body,
// or throws CheckInRefused naming the first field that breaks a rule. A refused
// check-in changes nothing that is stored, so a faulty request is refused when
// the server hands it over, never when the alarm sends it.
export function checkCheckIn(text) {
  if (new TextEncoder().encode(text).length > MAXIMUM_CHECK_IN_BYTES) {
    throw new CheckInRefused("", `must be at most ${MAXIMUM_CHECK_IN_BYTES} bytes`);
  }
  let body;
  try {
    body = JSON.parse(text);
  } catch {
    throw new CheckInRefused("", "must be JSON");
  }
  checkKeys(body, ["held_messages"], ["health_checks"], "check-in");
  if (!Array.isArray(body.held_messages)) {
    throw new CheckInRefused("held_messages", "must be a list");
  }
  const healthChecks = body.health_checks ?? [];
  if (!Array.isArray(healthChecks)) {
    throw new CheckInRefused("health_checks", "must be a list");
  }
  if (healthChecks.length > MAXIMUM_HEALTH_CHECKS) {
    throw new CheckInRefused("health_checks", `must have at most ${MAXIMUM_HEALTH_CHECKS} health checks`);
  }
  const seenHeldMessageIds = new Set();
  body.held_messages.forEach((message, index) => {
    const field = `held_messages[${index}]`;
    checkHeldMessage(message, field);
    if (seenHeldMessageIds.has(message.held_message_id)) {
      throw new CheckInRefused(`${field}.held_message_id`, "must be unique in the check-in");
    }
    seenHeldMessageIds.add(message.held_message_id);
  });
  const seenCheckIds = new Set();
  const checkedHealthChecks = healthChecks.map((check, index) => {
    const field = `health_checks[${index}]`;
    const checked = checkHealthCheck(check, field);
    if (seenCheckIds.has(checked.check_id)) {
      throw new CheckInRefused(`${field}.check_id`, "must be unique in the check-in");
    }
    seenCheckIds.add(checked.check_id);
    return checked;
  });
  return { held_messages: body.held_messages, health_checks: checkedHealthChecks };
}

// sameToken compares the Authorization header with the expected one in time
// that does not depend on where they differ.
export function sameToken(given, expected) {
  const a = new TextEncoder().encode(given);
  const b = new TextEncoder().encode(expected);
  let difference = a.length ^ b.length;
  for (let index = 0; index < b.length; index++) {
    difference |= (a[index] ?? 0) ^ b[index];
  }
  return difference === 0;
}

function isoTime(ms) {
  return ms === null || ms === undefined ? null : new Date(ms).toISOString().replace(/\.\d{3}Z$/, "Z");
}

function sendKey(sendId) {
  return "send:" + String(sendId).padStart(12, "0");
}

// putInOrder writes [key, value] entries, at most KEYS_PER_STORAGE_CALL at a
// time, in the order given: a write that stops part way has written only
// the earlier entries.
async function putInOrder(storage, entries) {
  for (let start = 0; start < entries.length; start += KEYS_PER_STORAGE_CALL) {
    await storage.put(Object.fromEntries(entries.slice(start, start + KEYS_PER_STORAGE_CALL)));
  }
}

async function deleteKeys(storage, keys) {
  for (let start = 0; start < keys.length; start += KEYS_PER_STORAGE_CALL) {
    await storage.delete(keys.slice(start, start + KEYS_PER_STORAGE_CALL));
  }
}

async function readSends(storage) {
  return [...(await storage.list({ prefix: "send:" })).values()];
}

async function readStored(storage, prefix, ids) {
  const found = new Map();
  for (const id of ids ?? []) {
    const value = await storage.get(prefix + id);
    if (value) {
      found.set(id, value);
    }
  }
  return found;
}

function readHeldMessages(storage, checkIn) {
  return readStored(storage, "held_message:", checkIn?.held_message_ids);
}

async function readHealthChecks(storage, checkIn) {
  return [...(await readStored(storage, "health_check:", checkIn?.health_check_ids)).values()];
}

// nextWakeUp is when the alarm must run next: the first due time of a held
// message not yet sent in this silence, or the next attempt of a send in
// progress; null when nothing waits.
export function nextWakeUp(checkIn, held, sends) {
  if (!checkIn) {
    return null;
  }
  const sentThisSilence = new Set(sends.filter((s) => s.silence_started_at === checkIn.accepted_at).map((s) => s.held_message_id));
  const times = [];
  for (const [id, message] of held) {
    if (!sentThisSilence.has(id)) {
      times.push(checkIn.accepted_at + message.delay_seconds * 1000);
    }
  }
  for (const s of sends) {
    if (s.send_state === "sending" && s.silence_started_at === checkIn.accepted_at) {
      times.push(s.next_attempt_at);
    }
  }
  return times.length === 0 ? null : Math.min(...times);
}

async function scheduleAlarm(storage) {
  const checkIn = await storage.get("check_in");
  const wakeUp = nextWakeUp(checkIn, await readHeldMessages(storage, checkIn), await readSends(storage));
  if (wakeUp === null) {
    await storage.deleteAlarm();
  } else {
    await storage.setAlarm(wakeUp);
  }
}

function publicSend(s) {
  return {
    send_id: s.send_id,
    held_message_id: s.held_message_id,
    silence_started_at: isoTime(s.silence_started_at),
    due_at: isoTime(s.due_at),
    attempts: s.attempts,
    send_state: s.send_state,
    send_outcome: s.send_outcome,
    http_status_code: s.http_status_code,
    finished_at: isoTime(s.finished_at),
    recovery_sent_at: isoTime(s.recovery_sent_at),
    recovery_outcome: s.recovery_outcome ?? null,
  };
}

function retryable(statusCode) {
  return statusCode === null || statusCode === 429 || (statusCode >= 500 && statusCode <= 599);
}

function isHealthyStatus(statusCode) {
  return statusCode !== null && statusCode >= 200 && statusCode <= 299;
}

// outcomeOf names what a service's answer to one request that is not retried
// (a test, a recovery, or an alert) means.
function outcomeOf(statusCode) {
  if (statusCode === null) {
    return "no_answer";
  }
  if (isHealthyStatus(statusCode)) {
    return "delivered";
  }
  return retryable(statusCode) ? "service_unavailable" : "refused_by_service";
}

// acceptCheckIn stores a checked set of held messages and health checks as the
// new set, starts a new silence at now, and moves the alarm. It rewrites an
// entry only when it changed, so an unchanged check-in writes one row. The
// server is back, so sends still in progress stop, and the recovery request of
// each held message that was delivered during the silence goes out once.
//
// A send that was still in progress when this check-in came, and was
// delivered a moment later, gets no recovery request: that overlap is rare,
// and the recovery request is a courtesy, not an alert.
export async function acceptCheckIn(storage, payload, now, send) {
  const previous = await storage.get("check_in");
  const heldMessages = payload.held_messages;
  const healthChecks = payload.health_checks;
  const heldIds = heldMessages.map((m) => m.held_message_id);
  const healthIds = healthChecks.map((c) => c.check_id);
  const writes = [];
  for (const [prefix, entries, idOf] of [["held_message:", heldMessages, (m) => m.held_message_id], ["health_check:", healthChecks, (c) => c.check_id]]) {
    for (const entry of entries) {
      if (JSON.stringify(await storage.get(prefix + idOf(entry))) !== JSON.stringify(entry)) {
        writes.push([prefix + idOf(entry), entry]);
      }
    }
  }
  const removed = [
    ...(previous?.held_message_ids ?? []).filter((id) => !heldIds.includes(id)).map((id) => "held_message:" + id),
    ...(previous?.health_check_ids ?? []).filter((id) => !healthIds.includes(id)).map((id) => "health_check:" + id),
    // The state of a health check that is no longer in the set: its failure
    // count and whether it is alerting. A check that returns starts fresh.
    ...[...(await storage.list({ prefix: "health_state:" })).keys()].filter((key) => !healthIds.includes(key.slice("health_state:".length))),
  ];
  const sends = await readSends(storage);
  // The recovery requests go out after the check-in is stored. Each is marked
  // as sent before it goes out, so a failure never sends one twice.
  const recoveries = [];
  for (const s of sends) {
    if (previous && s.silence_started_at === previous.accepted_at && s.send_outcome === "delivered" && !s.recovery_sent_at && recoveries.length < SENDS_PER_ALARM) {
      const message = await storage.get("held_message:" + s.held_message_id);
      if (message?.recovery_request) {
        s.recovery_sent_at = now;
        recoveries.push({ record: s, request: message.recovery_request });
        writes.push([sendKey(s.send_id), s]);
      }
    }
  }
  const expired = [];
  for (const [index, s] of sends.entries()) {
    const fromPreviousSilence = previous && s.silence_started_at === previous.accepted_at;
    if (!fromPreviousSilence && (s.due_at < now - SEND_LOG_MS || index < sends.length - SEND_LOG_ENTRIES)) {
      expired.push(sendKey(s.send_id));
    } else if (s.send_state === "sending") {
      Object.assign(s, { send_state: "finished", send_outcome: "stopped_by_check_in", finished_at: now });
      writes.push([sendKey(s.send_id), s]);
    }
  }
  // The check-in is written last, and the removed entries are deleted after
  // it: a write that stops part way keeps the earlier check-in with all its
  // entries, and the next check-in writes the rest.
  writes.push(["check_in", { accepted_at: now, held_message_ids: heldIds, health_check_ids: healthIds }]);
  await putInOrder(storage, writes);
  await deleteKeys(storage, [...removed, ...expired]);
  await scheduleAlarm(storage);
  await Promise.all(recoveries.map(async ({ record, request }) => {
    record.recovery_outcome = outcomeOf((await send(request)).http_status_code);
    await storage.put(sendKey(record.send_id), record);
  }));
  const kept = sends.filter((s) => !expired.includes(sendKey(s.send_id)));
  return {
    check_in_accepted_at: isoTime(now),
    held_messages_stored: heldMessages.length,
    health_checks_stored: healthChecks.length,
    sends: kept.reverse().map(publicSend),
  };
}

// sendOnce sends one held request and reports the HTTP status code, or null
// when no answer came: then nobody knows whether the service acted on it.
// With readAnswer, which only a test sets, it also reports the start of the
// answer's text as answer_text.
export async function sendOnce(request, fetchFunction, readAnswer = false) {
  try {
    const response = await fetchFunction(request.url, {
      method: request.method,
      headers: request.headers,
      body: request.method === "GET" ? undefined : request.body,
      redirect: "manual",
      signal: AbortSignal.timeout(ANSWER_TIME_LIMIT_MS),
    });
    let answerText = null;
    try {
      if (readAnswer) {
        answerText = (await response.text()).slice(0, MAXIMUM_TEST_ANSWER_CHARACTERS);
      } else {
        await response.body?.cancel();
      }
    } catch {
      // The answer's body is needed only for a test, which then has no receipt.
    }
    return answerText === null ? { http_status_code: response.status } : { http_status_code: response.status, answer_text: answerText };
  } catch {
    return { http_status_code: null };
  }
}

// recordAttempt saves what one attempt, which ended at endedAt, found. A
// send that a check-in stopped meanwhile becomes delivered when the service
// accepted it, and otherwise stays stopped. The next attempt is 30 seconds
// after this one ended, so a request that waited 30 seconds for no answer
// is not due again at once.
function recordAttempt(record, result, endedAt) {
  record.http_status_code = result.http_status_code;
  if (isHealthyStatus(result.http_status_code)) {
    return Object.assign(record, { send_state: "finished", send_outcome: "delivered", finished_at: endedAt });
  }
  if (record.send_state === "finished") {
    return record;
  }
  if (!retryable(result.http_status_code)) {
    return Object.assign(record, { send_state: "finished", send_outcome: "refused_by_service", finished_at: endedAt });
  }
  if (endedAt + RETRY_INTERVAL_MS > record.due_at + SEND_WINDOW_MS) {
    return Object.assign(record, { send_state: "finished", send_outcome: "not_delivered_within_10_minutes", finished_at: endedAt });
  }
  return Object.assign(record, { next_attempt_at: endedAt + RETRY_INTERVAL_MS });
}

async function attemptSend(storage, sendId, message, now, clock, send) {
  // Saved before the request goes out: if this run ends before it records the
  // outcome, the send is due again 30 seconds later and goes again. A
  // duplicate alert is acceptable; a dropped one is not.
  const started = await storage.get(sendKey(sendId));
  started.attempts += 1;
  started.next_attempt_at = now + RETRY_INTERVAL_MS;
  await storage.put(sendKey(sendId), started);
  const result = await send(message.request);
  const current = await storage.get(sendKey(sendId));
  await storage.put(sendKey(sendId), recordAttempt(current, result, clock()));
}

// runAlarm starts a send for each held message that is due in this silence
// and has none yet, then makes one attempt for each send whose attempt is
// due, at most SENDS_PER_ALARM at once, and moves the alarm.
export async function runAlarm(storage, clock, send) {
  const now = clock();
  const checkIn = await storage.get("check_in");
  if (!checkIn) {
    return;
  }
  const held = await readHeldMessages(storage, checkIn);
  const sends = await readSends(storage);
  const sentThisSilence = new Set(sends.filter((s) => s.silence_started_at === checkIn.accepted_at).map((s) => s.held_message_id));
  let nextSendId = (await storage.get("next_send_id")) ?? 1;
  const created = [];
  for (const [id, message] of held) {
    const due = checkIn.accepted_at + message.delay_seconds * 1000;
    if (due <= now && !sentThisSilence.has(id)) {
      const record = { send_id: nextSendId, held_message_id: id, silence_started_at: checkIn.accepted_at, due_at: due, attempts: 0, next_attempt_at: now, send_state: "sending", send_outcome: null, http_status_code: null, finished_at: null, recovery_sent_at: null, recovery_outcome: null };
      created.push([sendKey(nextSendId), record]);
      sends.push(record);
      nextSendId += 1;
    }
  }
  if (created.length > 0) {
    // next_send_id goes first, so a write that stops part way never hands
    // out a send ID twice. Every new send is saved before any request goes out.
    await putInOrder(storage, [["next_send_id", nextSendId], ...created]);
  }
  // Sends never attempted go first, then the ones whose attempt has waited
  // longest, so sends that keep getting no answer never crowd out the rest.
  const due = sends
    .filter((s) => s.send_state === "sending" && s.silence_started_at === checkIn.accepted_at && s.next_attempt_at <= now)
    .sort((a, b) => (a.attempts > 0) - (b.attempts > 0) || a.next_attempt_at - b.next_attempt_at)
    .slice(0, SENDS_PER_ALARM);
  await Promise.all(due.map(async (s) => {
    const message = held.get(s.held_message_id);
    if (!message) {
      const record = await storage.get(sendKey(s.send_id));
      await storage.put(sendKey(s.send_id), Object.assign(record, { send_state: "finished", send_outcome: "held_message_replaced", finished_at: now }));
      return;
    }
    await attemptSend(storage, s.send_id, message, now, clock, send);
  }));
  await scheduleAlarm(storage);
}

// probeOnce asks for a health check URL and reports the HTTP status code, or
// null when no answer came in time. A redirect is not followed: a healthy
// /health answers 200 itself, and a redirect to a login page must not pass.
export async function probeOnce(check, fetchFunction) {
  try {
    const response = await fetchFunction(check.url, {
      method: "GET",
      // Asks Cloudflare's cache for a fresh answer, so a cached 200 never hides a failure.
      headers: { "Cache-Control": "no-cache" },
      redirect: "manual",
      signal: AbortSignal.timeout(check.timeout_ms),
    });
    try {
      await response.body?.cancel();
    } catch {
      // Only the status code matters.
    }
    return response.status;
  } catch {
    return null;
  }
}

function emptyHealthState() {
  return { consecutive_failures: 0, alerting: false, alerted_at: null, last_status_code: null, last_checked_at: null, last_alert_outcome: null, last_recovery_outcome: null };
}

// runHealthChecks probes every stored health check once and sends the alert
// request of a check that has failed failures_before_alert times in a row, and
// the recovery request of an alerting check that is healthy again. Each
// transition is saved before its request goes out, so a run that stops part way
// never sends the same alert twice. A request that the service could not take
// (no answer, 429, or 5xx) is tried again at the next run: the check is put
// back before the transition, because a dropped alert is worse than a repeated one.
export async function runHealthChecks(storage, clock, probeFetch, send) {
  const healthChecks = await readHealthChecks(storage, await storage.get("check_in"));
  if (healthChecks.length === 0) {
    return { checked: 0, results: [] };
  }
  let requestsLeft = HEALTH_REQUESTS_PER_RUN;
  const results = await Promise.all(healthChecks.map(async (check) => {
    const statusCode = await probeOnce(check, probeFetch);
    const key = "health_state:" + check.check_id;
    const state = (await storage.get(key)) ?? emptyHealthState();
    state.last_status_code = statusCode;
    state.last_checked_at = clock();
    let request = null;
    let transition = null;
    if (isHealthyStatus(statusCode)) {
      state.consecutive_failures = 0;
      if (state.alerting && (!check.recovery_request || requestsLeft > 0)) {
        state.alerting = false;
        if (check.recovery_request) {
          request = check.recovery_request;
          transition = "recovery";
        }
      }
    } else {
      state.consecutive_failures += 1;
      if (state.consecutive_failures >= check.failures_before_alert && !state.alerting && requestsLeft > 0) {
        state.alerting = true;
        state.alerted_at = state.last_checked_at;
        request = check.alert_request;
        transition = "alert";
      }
    }
    if (request) {
      requestsLeft -= 1;
    }
    await storage.put(key, state);
    let outcome = null;
    if (request) {
      const result = await send(request);
      outcome = outcomeOf(result.http_status_code);
      state[transition === "alert" ? "last_alert_outcome" : "last_recovery_outcome"] = outcome;
      if (retryable(result.http_status_code)) {
        state.alerting = transition === "recovery";
      }
      await storage.put(key, state);
    }
    return {
      check_id: check.check_id,
      healthy: isHealthyStatus(statusCode),
      http_status_code: statusCode,
      consecutive_failures: state.consecutive_failures,
      alerting: state.alerting,
      alert_outcome: transition === "alert" ? outcome : null,
      recovery_outcome: transition === "recovery" ? outcome : null,
    };
  }));
  return { checked: healthChecks.length, results };
}

// pushoverReceiptIn is the receipt in a service's answer to an accepted
// test request, or null when the answer has none or a malformed one. Only
// Pushover's answer to an emergency notification has a receipt.
export function pushoverReceiptIn(answerText) {
  try {
    const receipt = JSON.parse(answerText)?.receipt;
    return typeof receipt === "string" && PUSHOVER_RECEIPT.test(receipt) ? receipt : null;
  } catch {
    return null;
  }
}

// sendTests sends each held message's test request now, for the "send a test
// now" request. Tests are not retried and not logged as sends. The answer to
// each includes the Pushover receipt, when the answer has one.
export async function sendTests(storage, send) {
  const held = await readHeldMessages(storage, await storage.get("check_in"));
  return Promise.all([...held.values()].slice(0, SENDS_PER_ALARM).map(async (message) => {
    const result = await send(message.test_request, true);
    const outcome = outcomeOf(result.http_status_code);
    const receipt = outcome === "delivered" ? pushoverReceiptIn(result.answer_text) : null;
    return { held_message_id: message.held_message_id, send_outcome: outcome, http_status_code: result.http_status_code, ...(receipt ? { pushover_receipt: receipt } : {}) };
  }));
}

function json(status, body) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// handleObjectRequest is the Durable Object's side of a request that the
// Worker already checked.
export async function handleObjectRequest(request, storage, clock, send, probeFetch) {
  const path = new URL(request.url).pathname;
  if (request.method !== "POST") {
    return json(404, { error_code: "not_found", error_message: "The Solo outside alarm answers only POST requests." });
  }
  if (path === "/internal/run-health-checks") {
    return json(200, await runHealthChecks(storage, clock, probeFetch, send));
  }
  if (path === "/send-test") {
    return json(200, { test_sends: await sendTests(storage, send) });
  }
  if (path !== "/check-in") {
    return json(404, { error_code: "not_found", error_message: `The Solo outside alarm has nothing at ${path}.` });
  }
  let payload;
  try {
    payload = checkCheckIn(await request.text());
  } catch (error) {
    if (error instanceof CheckInRefused) {
      return json(400, { error_code: "check_in_refused", field: error.field, error_message: `${error.field || "The check-in"} ${error.message}` });
    }
    throw error;
  }
  return json(200, await acceptCheckIn(storage, payload, clock(), send));
}

function soloOutsideAlarmObject(env) {
  return env.SOLO_OUTSIDE_ALARM.get(env.SOLO_OUTSIDE_ALARM.idFromName(DURABLE_OBJECT_NAME));
}

// handleWorkerRequest checks the method, the path, and the check-in token
// before the request reaches the Durable Object, so other requests cost it
// nothing. The health check run is not reachable from here.
export async function handleWorkerRequest(request, env) {
  const path = new URL(request.url).pathname;
  if (request.method !== "POST" || (path !== "/check-in" && path !== "/send-test")) {
    return json(404, { error_code: "not_found", error_message: "The Solo outside alarm answers only POST /check-in and POST /send-test." });
  }
  const expected = env[CHECK_IN_TOKEN_SECRET];
  if (!expected) {
    return json(503, { error_code: "check_in_token_not_set", error_message: `The Worker's secret ${CHECK_IN_TOKEN_SECRET} is not set; set up the Solo outside alarm again.` });
  }
  if (!sameToken(request.headers.get("Authorization") ?? "", "Bearer " + expected)) {
    return json(401, { error_code: "check_in_token_wrong", error_message: "The check-in token is wrong." });
  }
  return soloOutsideAlarmObject(env).fetch(request);
}

// handleScheduled runs once for each Cron Trigger firing (every 5 minutes) and
// has the Durable Object run the health checks. A failed run throws, so
// Cloudflare's logs show it.
export async function handleScheduled(controller, env, ctx) {
  const run = soloOutsideAlarmObject(env)
    .fetch(RUN_HEALTH_CHECKS_URL, { method: "POST" })
    .then((response) => {
      if (!response.ok) {
        throw new Error(`The health check run answered HTTP ${response.status}.`);
      }
    });
  ctx?.waitUntil(run);
  await run;
}

export class SoloOutsideAlarm extends DurableObject {
  async fetch(request) {
    return handleObjectRequest(request, this.ctx.storage, () => Date.now(), (held, readAnswer) => sendOnce(held, fetch, readAnswer), fetch);
  }

  async alarm() {
    await runAlarm(this.ctx.storage, () => Date.now(), (held) => sendOnce(held, fetch));
  }
}

export default {
  fetch: handleWorkerRequest,
  scheduled: handleScheduled,
};
