# Testing Monkeymaxxing Solo

Run the cheapest checks that prove correctness before running slower full-server proofs.

## Unit and static checks (run locally in seconds)

1. **Cloudflare Worker tests:**
   ```sh
   node --test worker/solo_outside_alarm_worker_test.mjs
   ```
2. **Setup helper Go tests:**
   ```sh
   cd set_up_solo_services && go vet ./... && go test ./...
   ```
3. **Template and invariant tests:**
   ```sh
   python3 -m unittest discover -s tests -p '*_test.py'
   ```
   and `mmx validate` against a rendered copy of the systems repository template.

## End-to-end proofs

Full setup, deploy, backup/Bucket-Lock refusal, weekly local restore check, outside-alarm firing, and Solo-to-Stack upgrade proofs run on isolated Debian 13 KVM virtual machines across supported coding agents (Claude Code, Codex, Gemini) plus periodic live-account smoke tests.
