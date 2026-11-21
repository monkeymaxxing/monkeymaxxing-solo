# Your production system, run by Monkeymaxxing Solo

This repository is your systems repository: every setting and every secret
of your one server, `solo01`, which runs your web apps behind Cloudflare.
Your coding agent makes every change here and applies it with MMX from your
laptop. You never need to log in to the server yourself.

The setup fills the lines marked "the setup fills it" below with your own
names and links.

## What Monkeymaxxing Solo builds

```text
Your users
  |
Cloudflare (free plan): your domain, a cache, an attack filter, a private tunnel to your server,
  |                     file storage (R2) for uploads and locked backups, and the outside alarm
  |  one tunnel, opened from your server, so your server has no open web port
+---------------------------------------------------------------+
| solo01: one Debian 13 server, 2 CPUs, 4 GB memory             |
|   Your apps:  cloudflared, Caddy, and Podman containers        |
|   Database:   Postgres 17, with pgBackRest backups to R2       |
|   Watcher:    MMWatchdog                                       |
+---------------------------------------------------------------+
  Managed from your laptop by MMX, over doubleshell and WireGuard (no public SSH port)
```

- **Your apps** run in containers. A release starts the new version beside
  the old one, checks its health URL, and switches users over with no
  downtime. A rollback is the same step with the previous version.
- **Your database** is Postgres 17, with one database and one login for
  each app. Every change goes to Cloudflare R2 within about a minute, with
  a full backup each week and a smaller one each night, all encrypted.
- **Your backups are locked** for 14 days by R2's Bucket Lock, so nobody
  who breaks into `solo01` can delete them. Once a week, `solo01` restores
  the newest backup into a throwaway Postgres (no network port) and checks
  your data: the restore test.
- **MMWatchdog** watches disks, memory, services, Postgres, backups,
  security updates, and known exploited flaws, and alerts you.
- **The outside alarm,** a small program in your own Cloudflare account,
  alerts you when `solo01` goes silent, and when your site stops
  answering from the internet.
- **Security updates** install every day. `solo01` never reboots by
  itself: your coding agent reboots it in your weekly maintenance window.

## Links

| What | Where |
| --- | --- |
| Your site | the setup fills it: each app's address |
| This repository at GitHub | the setup fills it |
| Your app's tests and builds at GitHub | the setup fills it |
| Your Cloudflare account | <https://dash.cloudflare.com> |
| Your server's provider | the setup fills it: the provider's console |
| Your alerts | the setup fills it: Pushover, or your chosen channel |
| What is done and what is left | [`monkeymaxxing_solo_checklist.yml`](monkeymaxxing_solo_checklist.yml) |

## What to ask your coding agent

Ask in your own words, from your laptop. For example:

- "Deploy the newest version of shop." or "Roll shop back."
- "Add my second app to the server."
- "Someone deleted orders an hour ago. Get them back."
- "The server is dead." or "Move the server to a bigger one."
- "Do the weekly system review."
- "Rotate the Stripe key."

## When something fails

| What fails | What happens | What you do |
| --- | --- | --- |
| A new app version fails its health check | Users stay on the old version. The release stops with the reason. | Nothing. Your agent finds the cause. |
| An app crashes | Its container restarts by itself. MMWatchdog alerts you if it keeps failing. | Ask your agent to look. |
| `solo01` freezes, runs out of memory, or goes offline | The site is down. The outside alarm alerts you within about 5 minutes. | Ask your agent. If `solo01` does not come back, your agent rebuilds it on a new server from this repository and the R2 backups. |
| Your site stops answering from the internet | The outside alarm alerts you after two failed checks, 5 minutes apart. | Ask your agent to look. |
| The disk fills, or backups stop | MMWatchdog alerts you. | Ask your agent to look. |
| Data is deleted by mistake | The backups can bring the database back to any moment of the last 21 days. | Ask your agent, and say when the mistake happened. |
| Your laptop is lost | Your vault passphrase is in your password manager, and this repository is at GitHub. | Ask your agent, on a new laptop, to rebuild `solo01` and change every secret. |

## Your weekly duty

Once a week, you and your coding agent do the weekly system review, about
10 minutes: the week's alerts, waiting updates and reboots, the restore
test's result, and what else is due. Say "Do the weekly system review."

## When to move to the Monkeymaxxing Stack

Solo is one server. Move to the Monkeymaxxing Stack when one server is no
longer enough: when the load outgrows a bigger server, when the site must
survive the loss of a server without a rebuild, when releases need a
staging server, or when more than one person manages the system. The Stack
uses the same app contract, so your apps move with no change. The
checklist's `what_solo_leaves_out` lists each reason.

## Next actions

The setup fills it: what is open, who does it, and when.
