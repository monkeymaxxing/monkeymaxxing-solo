# AGENTS.md — monkeymaxxing-solo

This repository holds **Monkeymaxxing Solo**: the coding-agent skill, single-server systems repository template, Cloudflare Worker outside alarm, and local setup helper for running one or more web apps on a single Debian server behind Cloudflare.

## Owning documents

- [README.md](README.md): what Monkeymaxxing Solo is, what it leaves out compared to the Monkeymaxxing Stack, and how to give the skill to a coding agent.
- [docs/design.md](docs/design.md): every convention and architectural choice, with its reason. If code or a template disagrees with `docs/design.md`, either the file is wrong or `docs/design.md` changes first.
- [docs/acceptance.md](docs/acceptance.md): required workflows, outcomes, exclusions, and completion criteria.
- [docs/testing.md](docs/testing.md): test commands and verification levels.

## Core conventions

- **Zero decision fatigue:** Solo makes every sensible default upfront. Do not add optional modes or provider-specific server creation scripts.
- **Same app contract as the Monkeymaxxing Stack:** `Containerfile`, Postgres, Cloudflare R2 for uploads, `/health`, `PORT`, `WEB_CONCURRENCY`, separate migrate/web/jobs commands. Graduating to the Stack must require zero app rewrites.
- **Zero test switches in product code:** Every command talks to real service hostnames (`api.cloudflare.com`, `*.r2.cloudflarestorage.com`, `*.workers.dev`, `api.pushover.net`) using the OS system CA store. Never add test-endpoint flags or custom cert pinning.
- **Plain English and explicit names:** Every setting, file, script, and error message must be immediately understandable to a developer new to Solo.
- **Minimal dependencies:** The Cloudflare Worker is a single zero-dependency ES module (`node --test` locally). The Go setup helper uses only the Go standard library.
