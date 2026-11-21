# Design of Monkeymaxxing Solo

Monkeymaxxing Solo is a single-server production system built and operated by a coding agent for solo founders and side projects. It uses the same core tools, app contract, and mental model as the [Monkeymaxxing Stack](https://github.com/monkeymaxxing/monkeymaxxing-stack), compressed onto one plain Debian server (`solo01`) and two infrastructure accounts (any VPS provider + Cloudflare Free).

Every convention below records its reason and honest trade-off.

## Layout

```text
monkeymaxxing-solo/
  README.md                               overview, comparison with the Stack, how to install the skill
  AGENTS.md                               contributor guidance
  docs/
    design.md                             this document
    acceptance.md                         required workflows, outcomes, and exclusions
    testing.md                            testing levels and commands
  worker/
    solo_outside_alarm_worker.js          single-file, zero-dependency Cloudflare Worker outside alarm
    solo_outside_alarm_worker_test.mjs    local unit tests under `node --test`
  set_up_solo_services/                   small Go CLI (stdlib only) for hidden-prompt secrets + Cloudflare/Pushover setup
  skills/monkeymaxxing-solo/
    SKILL.md                              coding-agent entry point
    systems_repository_template/          copied whole into a user's private systems repository
  tests/                                  template, inventory, firewall, and invariant tests
```

## Architecture on one server

```text
Users
  |
Cloudflare (Free): DNS, cache, attack filter, tunnel,
  |                R2 uploads, R2 locked backups, Worker outside alarm
  |  one tunnel
+--------------------------------------------------------+
| solo01 (one Debian 13 VPS, ~2 vCPU / 4 GB RAM)         |
|   App layer:  cloudflared, Caddy, Podman containers    |
|   Database:   Plain Postgres 17 + pgBackRest           |
|   Watcher:    MMWatchdog (standalone)                  |
+--------------------------------------------------------+
  Managed from laptop by MMX over doubleshell (WireGuard)
```

One `solo01` server can host one app or several (`apps.<app>` in `mmx_project_settings.yml`), each with its own Postgres database, container services, and zero-downtime health-checked deploys.

## Why these choices

- **One plain Debian 13 VPS, created manually at any provider.** Skipping VPS provider API scripts eliminates provider API tokens and works on every Linux VPS host on day one.
- **Same app contract as the Monkeymaxxing Stack.** Apps use a `Containerfile`, Postgres, Cloudflare R2 for uploaded files, `/health`, `PORT`, `WEB_CONCURRENCY`, and separate migrate/web/jobs commands. Graduating to the Stack requires no app rewrite.
- **Cloudflare Tunnel + Caddy + Podman/Quadlet.** No open web ports on `solo01`. Cloudflare filters attack traffic and caches static/eligible responses. Caddy handles zero-downtime blue/green container swaps on deploy.
- **Plain Postgres 17 (no `pg_auto_failover`).** On a single server there is nothing to fail over to. `shared_buffers` stays modest (128 MB on 4 GB servers, matching the Stack's `staging01`) so two releases of an app fit in memory during deploys.
- **pgBackRest continuous WAL archiving + nightly backups to Cloudflare R2 with 14-day Bucket Lock.** Eliminates a third storage account (Backblaze B2) while keeping backups off the VPS provider. Two prefix Bucket Lock rules lock every WAL segment (`pgbackrest/archive/main/1`) and every backup folder (`pgbackrest/backup/main/2`) against deletion and overwrite for `lock_backups_against_deletion_for_days` days, while leaving pgBackRest's mutable index files (`archive.info`, `backup.info`, and their `.copy` files) writable so nightly backups and expiration keep working; `resume=n` avoids overwriting `backup.manifest.copy` mid-backup. A scoped R2 token on `solo01` can write objects but cannot alter Bucket Lock rules, blocking deletion of locked backup data from a compromised VPS. Trade-off: backups share the Cloudflare account with the front door, the small index files are not locked, and unlike B2 Compliance Lock, the Cloudflare account owner can remove a Bucket Lock rule.
- **Weekly local restore check.** Once a week, a timer on `solo01` restores the latest R2 backup into a scratch directory, starts a throwaway Postgres on it with the Debian Postgres program that is already installed (`/usr/lib/postgresql/<major>/bin`), runs each app's check query, then stops that Postgres and removes the directory. The throwaway Postgres listens only on a private Unix socket inside the scratch directory: no network port, no contact with the live Postgres, and no archiving (`archive_mode = off`). Proves the backup is intact and the encryption passphrase works without needing a VPS provider API key or extra server cost. Reason for not using a `postgres` container image: a pull from Docker Hub can be refused for a busy shared server address (HTTP 429), the image costs about 450 MB of disk on a small server, it fails where `solo01` cannot reach `docker.io`, and its Postgres and glibc can differ from the live Postgres. The system's own Postgres program has the same version and collations as the live database.
- **`doubleshell` over WireGuard (UDP 443 -> 51820), port 22 closed.** Same SSH transport as the Stack, independent of Cloudflare availability, and nothing to redo when upgrading to the Stack.
- **Standalone `MMWatchdog` + Cloudflare Worker outside alarm.** `MMWatchdog` on `solo01` monitors disks, memory, systemd units, Postgres, backups, security updates, and CISA Known Exploited Vulnerabilities (OSV.dev). The single-file Cloudflare Worker (`worker/solo_outside_alarm_worker.js`) uses one SQLite-backed Durable Object (available on Workers Free) for both:
  1. **Dead man's switch (`POST /check-in`, `POST /send-test`):** `solo01` checks in every minute with ready-made HTTPS `held_messages` (defaulting to Pushover; Slack, Discord, Telegram, and webhooks work identically) plus optional `health_checks` URLs. When check-ins stop past `delay_seconds` (default 300s, minimum 180s), the Durable Object alarm fires the held requests with retries.
  2. **Active edge `/health` check (`scheduled()` Cron Trigger, every 5 minutes):** Requests each configured `https://<host>/health` URL from Cloudflare's edge. After 2 consecutive failed probes (to avoid transient reload blips), fires the held alert; sends one recovery request when `/health` succeeds again.
- **MMX from the laptop + local hidden-prompt secret helper.** Every server change is a Git commit in the user's private systems repository, applied by MMX over `doubleshell`. Secrets are pasted into a hidden local prompt and written straight into the encrypted `secrets_vault.age`, keeping secrets out of coding-agent conversations. Trade-off: without a separate systems desk, keys and the vault passphrase live on the developer's laptop.
- **Real hostnames, OS CA store, zero test switches.** Every command connects to real service hostnames (`api.cloudflare.com`, `*.r2.cloudflarestorage.com`, `*.workers.dev`, `api.pushover.net`) using standard OS TLS verification so testing environments can intercept via DNS + CA without product test flags.
