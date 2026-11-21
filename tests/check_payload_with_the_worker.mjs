// Reads a check-in payload (JSON text) from standard input and runs it through
// checkCheckIn(), the Worker's own validation of POST /check-in. Prints one
// line of JSON: {"ok": true, "held_messages": N, "health_checks": M}, or
// {"ok": false, "field": "...", "message": "..."} when the Worker refuses it.
// Used by outside_alarm_checkin_test.py:
//   node tests/check_payload_with_the_worker.mjs PATH_TO_solo_outside_alarm_worker.js < payload.json
import { registerHooks } from "node:module";
import { pathToFileURL } from "node:url";

// The Worker imports the DurableObject base class from Cloudflare's runtime;
// here a stand-in with the same constructor takes its place, as in the
// Worker's own tests.
registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === "cloudflare:workers") {
      return { url: "data:text/javascript,export class DurableObject { constructor(ctx, env) { this.ctx = ctx; this.env = env; } }", shortCircuit: true };
    }
    return nextResolve(specifier, context);
  },
});

const worker = await import(pathToFileURL(process.argv[2]).href);
let text = "";
for await (const chunk of process.stdin) {
  text += chunk;
}
try {
  const checked = worker.checkCheckIn(text);
  console.log(JSON.stringify({ ok: true, held_messages: checked.held_messages.length, health_checks: checked.health_checks.length }));
} catch (error) {
  if (error instanceof worker.CheckInRefused) {
    console.log(JSON.stringify({ ok: false, field: error.field, message: error.message }));
  } else {
    throw error;
  }
}
