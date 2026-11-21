# Acceptance contracts for Monkeymaxxing Solo

Every change to Monkeymaxxing Solo must preserve these required outcomes.

## Required workflows and outcomes

1. **Setup from a fresh Debian 13 VPS:** Given a web app meeting the Stack app contract, a manually created Debian 13 VPS IP, a free Cloudflare account, a Git host, and Pushover (or another supported alert destination), a coding agent using `skills/monkeymaxxing-solo` configures `solo01` and gets the app live behind Cloudflare Tunnel with zero open web ports and closed port 22 (`doubleshell` over WireGuard).
2. **Zero-downtime deploy and rollback:** Deploying a new container image tag starts the new container beside the old one, verifies `/health`, switches Caddy without dropping connections, and supports rolling back to the previous image tag.
3. **Multi-app support:** One `solo01` server supports one or more apps in `apps.<app>`, each with its own Postgres database, containers, and Cloudflare route.
4. **Offsite locked backups and proven recovery:** `pgBackRest` archives WAL continuously and takes nightly backups to Cloudflare R2 encrypted with `aes-256-cbc` under a 14-day R2 Bucket Lock. The weekly local restore check restores the latest backup into a throwaway Postgres on a private Unix socket (no network port, no download), verifies each app's check query, and cleans up.
5. **Local and outside monitoring:** Standalone `MMWatchdog` monitors disks, memory, systemd units, Postgres, backups, security updates, and CISA actively exploited CVEs. The Cloudflare Worker outside alarm fires held alerts when check-ins stop or when an app's `/health` fails consecutive edge probes, and notifies on recovery.
6. **Secrets stay out of AI chat:** Secrets are entered through a hidden local prompt directly into the encrypted MMX vault (`secrets_vault.age`) and never printed to stdout or pasted into coding-agent conversations.
7. **Rebuildable from Git and R2:** Every change to `solo01` is a commit in the private systems repository; a lost server can be replaced with a fresh Debian 13 VPS rebuilt from Git and restored from R2 backups.

## Explicit exclusions (handled by the Monkeymaxxing Stack)

- No separate 2FA systems desk (`desk01`); keys live on the developer's laptop.
- No multi-server pools, automatic Postgres failover (`pg_auto_failover`), staging server, or VPS provider API server-creation scripts.
- No separate third backup company (Backblaze B2 Compliance Lock) or nightly copy of R2 uploads.
- No `MMHotline`, AI triage agent, on-call schedules, team access controls, or emergency lock-out button.
