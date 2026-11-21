# Monkeymaxxing Solo

**One production server for your web app, built and run by your coding
agent. About $6 a month, free and open source, and ready to grow into the
[Monkeymaxxing Stack](https://github.com/monkeymaxxing/monkeymaxxing-stack)
with no app rewrite.**

Monkeymaxxing Solo is a skill for your coding agent, such as Claude Code,
Codex, or Gemini. You tell the agent "Set up Monkeymaxxing Solo for my
app." The agent gets your app ready, connects to one plain Debian 13 server
that you create at any company, puts Cloudflare in front of it, and runs
it for you afterwards: releases, rollbacks, recovery, and a short weekly
review. The setup takes under an hour, and about 15 minutes of it is you.

Solo is made for solo founders and side projects. It uses the same tools
and the same app contract as the Monkeymaxxing Stack, compressed onto one
server and two accounts: any server company, and Cloudflare's free plan.

> [!NOTE]
> **In development.** Solo has no release yet. The design is in
> [docs/design.md](docs/design.md), and the required outcomes are in
> [docs/acceptance.md](docs/acceptance.md).

## What Solo builds

```text
Your users
  |
Cloudflare (free plan): DNS, cache, attack filter, tunnel,
  |                     R2 for uploads and locked backups, the outside alarm Worker
  |  one tunnel, opened from solo01, so solo01 has no open web port
+--------------------------------------------------------+
| solo01: one Debian 13 server, 2 vCPU, 4 GB memory      |
|   Apps:      cloudflared, Caddy, Podman containers     |
|   Database:  Postgres 17 + pgBackRest                  |
|   Watcher:   MMWatchdog                                |
+--------------------------------------------------------+
  Managed from your laptop by MMX, over doubleshell and WireGuard
```

- **Your apps,** one or several, each with its own Postgres database,
  containers, and address. Releases start the new version beside the old
  one, check its health URL, and switch with no downtime. A rollback is
  the same step with the previous version.
- **The front door:** a Cloudflare Tunnel, so `solo01` opens no web port.
  Cloudflare caches what it can and filters attack traffic.
- **SSH:** [doubleshell](https://github.com/monkeymaxxing/doubleshell)
  carries SSH through WireGuard on UDP 443. Port 22 is closed to the
  internet.
- **Backups:** pgBackRest sends every database change to Cloudflare R2,
  encrypted, with a full backup each week and a smaller one each night. An
  R2 Bucket Lock keeps each backup for 14 days, so a break-in on `solo01`
  cannot delete them. Each week, `solo01` restores the newest backup into a
  throwaway Postgres, with no network port, and checks each app's data.
- **Monitoring:** [MMWatchdog](https://github.com/monkeymaxxing/mmwatchdog)
  on `solo01` watches disks, memory, services, Postgres, backups, security
  updates, and known exploited flaws (CISA's list, through OSV.dev). A
  single-file Cloudflare Worker in your own account, the outside alarm,
  alerts you when `solo01` stops checking in and when your site stops
  answering from the internet. Alerts go to Pushover by default, or to
  Slack, Discord, Telegram, a webhook, or email.
- **Configuration as code:** [MMX](https://github.com/monkeymaxxing/mmx) runs
  from your laptop. Every change is a commit in your private systems
  repository. You type each secret at a hidden prompt in your own terminal,
  straight into an encrypted vault, so no secret enters the agent's chat.

## What it costs

| What | Cost |
| --- | --- |
| One Debian 13 server, 2 vCPU (x86), 4 GB memory, about 40 GB disk, at any company | from about $6 a month |
| Cloudflare: DNS, tunnel, cache, Workers, and R2 | free; R2 storage above 10 GB costs about 1.5 cents per GB a month |
| GitHub: the private systems repository and the app's image builds | free |
| Pushover, for phone alerts | about $5 once |
| Monkeymaxxing Solo, MMX, MMWatchdog, and doubleshell | free and open source |

## Solo and the Monkeymaxxing Stack

Both use the same app contract: a `Containerfile`, Postgres, R2 for
uploads, a health URL, `PORT`, `WEB_CONCURRENCY`, and separate migrate,
web, and jobs commands. An app that runs on Solo runs on the Stack with no
change.

| | Monkeymaxxing Solo | Monkeymaxxing Stack |
| --- | --- | --- |
| Servers | One, `solo01` | App, database, and utility servers, growing to many |
| When a server dies | The site is down until your agent rebuilds `solo01` from Git and R2 | Another server takes over; a database replica takes over automatically |
| Database | Plain Postgres 17 | Postgres with automatic failover |
| Backups | R2 with a 14-day Bucket Lock; the Cloudflare account owner can remove the lock | A separate company (Backblaze B2) with a compliance lock nobody can remove |
| Restore test | Weekly, in a throwaway Postgres on `solo01` | Weekly, on a temporary server |
| Where the keys live | Your laptop, and the vault passphrase in your password manager | A separate systems desk with two-factor login |
| Server creation | You create one server by hand at any company | Scripts create servers at the provider |
| Alerts | MMWatchdog and the outside alarm, to your phone or channel | On-call schedules, escalation, and an AI triage agent |
| Accounts | Any server company, Cloudflare, GitHub, Pushover | Server provider, Cloudflare, Backblaze, GitHub, Pushover |
| Cost | from about $6 a month | from about €24 a month |

Move to the Stack when one server is no longer enough: when the load
outgrows a bigger server, when the site must survive the loss of a server
without a rebuild, when releases need a staging server, or when more than
one person manages the system.

## Give it to your coding agent

A skill is a directory of instructions for a coding agent. Solo's skill is
[`skills/monkeymaxxing-solo`](skills/monkeymaxxing-solo). Clone this
repository once, then copy the skill directory into the place where your
agent reads skills:

```sh
git clone https://github.com/monkeymaxxing/monkeymaxxing-solo.git ~/monkeymaxxing-solo

# Claude Code reads your skills from ~/.claude/skills
mkdir -p ~/.claude/skills
cp -R ~/monkeymaxxing-solo/skills/monkeymaxxing-solo ~/.claude/skills/

# Codex reads your skills from ~/.agents/skills
mkdir -p ~/.agents/skills
cp -R ~/monkeymaxxing-solo/skills/monkeymaxxing-solo ~/.agents/skills/

# Gemini CLI reads your skills from ~/.gemini/skills
mkdir -p ~/.gemini/skills
cp -R ~/monkeymaxxing-solo/skills/monkeymaxxing-solo ~/.gemini/skills/
```

Copy the directory; never link it. Reason: the skill's files must be
inside your agent's skills folder. Once Solo has releases, check out the
newest release tag before you copy, because between releases `main` names
no released programs, and the setup refuses to install from it.

Then start a new session of your agent in your app's folder and say:

- **To start:** "Set up Monkeymaxxing Solo for my app." Your agent asks
  for your domain and confirms your app, then shows what it sets up and
  what it costs.
- **Later:** "Deploy the newest version of shop", "Roll shop back", "Add
  my second app", "Someone deleted orders an hour ago. Get them back.",
  "Move the server to a bigger one", or "Do the weekly system review."

**Use only a coding agent you trust.** It gets full control of your
server. Your agent never asks you to paste a secret into the chat: you
type each secret at a hidden prompt in your own terminal.

## What is in this repository

| Path | What it is |
| --- | --- |
| [`skills/monkeymaxxing-solo/SKILL.md`](skills/monkeymaxxing-solo/SKILL.md) | The coding agent's entry point |
| [`skills/monkeymaxxing-solo/systems_repository_template/`](skills/monkeymaxxing-solo/systems_repository_template) | The systems repository each person gets: settings, playbooks, roles, checks, the setup steps, and the operating procedures |
| [`skills/monkeymaxxing-solo/worker/`](skills/monkeymaxxing-solo/worker) | A copy of the outside alarm Worker, beside the skill |
| [`skills/monkeymaxxing-solo/files_for_your_app/`](skills/monkeymaxxing-solo/files_for_your_app) | The GitHub Actions workflow that tests, builds, and pushes the app's image |
| [`worker/`](worker) | The outside alarm: one zero-dependency Cloudflare Worker, and its tests |
| [`set_up_solo_services/`](set_up_solo_services) | `set-up-solo-services`, the Go program (standard library only) that saves secrets at a hidden prompt and sets up Cloudflare |
| [`tests/`](tests) | Template, invariant, and documentation tests |

## Documentation

- [docs/design.md](docs/design.md): the architecture, and the reason for
  each decision.
- [docs/acceptance.md](docs/acceptance.md): the required outcomes, and what
  Solo leaves to the Monkeymaxxing Stack.
- [docs/testing.md](docs/testing.md): the test levels.
- [CONTRIBUTING.md](CONTRIBUTING.md): bug reports, ideas, and pull
  requests.
- [Monkeymaxxing Recipes](https://github.com/monkeymaxxing/monkeymaxxing-recipes):
  the shared cookbook for Solo and the Stack. It holds the
  [app framework guides](https://github.com/monkeymaxxing/monkeymaxxing-recipes/tree/main/frameworks),
  the [platform migration guides](https://github.com/monkeymaxxing/monkeymaxxing-recipes/tree/main/migrations),
  and add-on recipes for extra services and MMWatchdog check packs.

## Running the tests

```sh
# The outside alarm Worker
node --test worker/solo_outside_alarm_worker_test.mjs

# The setup helper
(cd set_up_solo_services && go vet ./... && go test -race ./...)

# The template, its invariants, and the documentation
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -p '*_test.py'
```

## License

[Apache License 2.0](LICENSE).
