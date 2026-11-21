# Answer two questions

**When:** first, when the person says "Set up Monkeymaxxing Solo for my
app." in the app's folder, and has no systems repository yet.

**What this file does:** the person names the domain and confirms which
apps go on the server. You show every other choice as a default, on one
screen, with the cost. The person's "go" approves the whole screen. Then
you install three small tools and start the systems repository on the
laptop.

**Who acts:** the person answers two questions and says "go". You work out
every other value from the laptop, the app, and the domain.

**Tell the person when this starts:** the trust line and the auto mode line
below, then the first question.

## Before you start

- **You do every step you can.** Never ask the person for a value that you
  can read from the laptop, the app, the domain, or a company's API.
- **The screens.** Each step gives the exact text of each screen, in a
  `text` block. Show its lines as they stand, with the person's own names
  and values in place of the example's. Keep each blank line.
- **Short messages.** Between screens, say what you do in one line at a
  time, such as "I am reading your app's code."
- **Command blocks.** Every command block starts with a comment that says
  where it runs.

## First: the trust line and auto mode

Your coding agent's program may ask the person before each new command,
file change, or web page, such as Claude Code's "Do you want to proceed?".
The setup runs about a hundred commands, so ask the person at the very
start to run you in auto mode.

**Tell the person first,** in your first reply, before the first question:

```text
USE ONLY A CODING AGENT YOU TRUST.  It will have full control of your server.

Trust means three things: it does the work well, it means you well, and you trust
the company that runs it, because your work passes through that company.

Run me in auto mode for this setup, so I don't stop to ask before each step.
In Claude Code: press Shift+Tab until it shows "auto mode on".
Reason: the setup runs about a hundred commands, and you approve the steps that matter,
such as changes to your app and anything that costs money, in this chat.

The setup takes under an hour.  I need you for about 15 minutes of it, mostly near the start.

By default, I'll go fast and only stop when I need you. Say "guide me" any time
and I'll slow down and explain each part before I do it. Say "go fast" to speed
up again. Ask "why?" about anything.
```

The time line is the only word about time and parts before "go": the map
of the four parts comes right after "go". When the person's agent is not
Claude Code, say in one line how to turn on that agent's own auto mode, or
that it has none. Never write a permission list into the app's folder.

Then ask the first question in the same reply.

**Check:** you showed the screen.

## Steps

1. **The domain.**

   **Ask the person,** in your first reply:

   ```text
   First question:  which domain is your app on?  (for example  example.org)
   ```

   **Check:** you recorded the domain.

2. **The apps.** Read the app's folder: its README, its framework, its
   `Containerfile` or `Dockerfile`, and its database settings. Find the
   app's name from its repository name, in lowercase letters, digits, and
   underscores, such as `shop`.

   **Ask the person,** in your second reply:

   ```text
   Second question:  I found your app "shop" in this folder.  Is that the app to put live?
   If your server should run more apps, name their folders too.
   ```

   One server runs several apps: each gets its own database, its own
   containers, and its own address.

   **Check:** you recorded each app's name and folder.

3. **The defaults, on one screen.** Work out each value, then show the
   screen.

   - **Your server:** one Debian 13 server with 2 CPUs of the x86 kind
     (amd64), 4 GB of memory, and about 40 GB of disk, at any company that
     sells one. Recommend Hetzner: cheap, simple, with locations in
     Europe, the US, and Singapore. Name OVHcloud, DigitalOcean, and Vultr
     as other choices, and accept any company the person already uses.
     Reason for x86: the pinned programs and the app's image are built for
     amd64, so an Arm server, such as Hetzner's CAX, cannot run them.
     Reason for 4 GB: a release runs the old and the new version side by
     side for a minute.
   - **Your app's address:** the domain itself, such as `example.org`, for
     the first app. A second app gets a name under the domain, such as
     `blog.example.org`. Never choose another name yourself: offer one only
     when the person asks, or when the domain already runs another site.
   - **Password manager:** the one the laptop has, such as 1Password,
     Bitwarden, or KeePassXC. On a Mac, look in `/Applications`. On Linux,
     look for `1password`, `bitwarden`, or `keepassxc` on `PATH`. When you
     find none, write "Which password manager do you use?" on the screen.
   - **Alerts:** Pushover on the person's phone, with its one-time fee.
   - **Cost:** "from about $6 a month" for the server, from the
     recommended company's public price list. Cloudflare's free plan costs
     nothing; R2 storage is free up to 10 GB, then about 1.5 cents per GB a
     month. Pushover is about $5 once. GitHub's free plan holds the
     private systems repository and builds the app.

   **Ask the person,** as the last text of your reply, with no tool call
   after it:

   ```text
   Here is what I will set up.

     Your server:        one Debian 13 server: 2 CPUs, 4 GB memory, about 40 GB disk
                         at Hetzner (or OVHcloud, DigitalOcean, Vultr, or any company you already use)
     Your apps:          shop, at example.org
     Cloudflare:         the free plan, in front of your site, with your backups and an outside alarm
     Alerts:             Pushover on your phone   (or Slack, Discord, Telegram, a webhook, or email)
     Password manager:   1Password

     Cost:               from about $6 a month for the server, plus about $5 once for Pushover.
                         Cloudflare and GitHub are free for this.

   Say "details" to see everything this includes.
   Tell me what to change, or say "go".
   ```

   **"details"** shows the rest, as one reply that ends your turn, then the
   same question:

   ```text
   Everything "go" approves:

     Your server:        you create it yourself at the company you choose; I never get a key to your account there
     Backups:            every database change to Cloudflare's file storage, encrypted, locked for 14 days
                         so nobody who breaks into your server can delete them; free up to 10 GB
     Restore test:       once a week your server proves that the newest backup restores
     Outside alarm:      a small program in your Cloudflare account that alerts you when your server goes silent
     Your keys:          your server's SSH key and your secrets' passphrase stay on this laptop,
                         and the passphrase in your password manager too
     Your settings:      a private repository at GitHub, such as acme_systems
     Tools:              mmx, doubleshell, and set-up-solo-services, three small tools in your home folder

   Tell me what to change, or say "go".
   ```

   "go" approves everything on both screens. It does not buy anything: the
   person creates the server and pays the company directly.

   When the person changes a line, confirm each change in your next reply,
   in one line each.

   **Check:** the person said "go".

4. **The tools.** Install the three programs on the laptop, into
   `~/.local/bin`, and verify their checksums and versions before moving on.

   - **MMX:** the release that `solo_software.mmx` in the installed
     skill's `systems_repository_template/mmx_project_settings.yml` pins,
     as [MMX installation](https://github.com/monkeymaxxing/mmx#install)
     says, with the archive checked against the release's `SHA256SUMS`.
   - **doubleshell:** the release that `solo_software.doubleshell` pins,
     never a newer one. Download `doubleshell-VERSION-PLATFORM.tar.gz` for
     the laptop's platform from the folder of the pin's `download_url`,
     such as `doubleshell-0.1.2-macos-arm64.tar.gz`, and check its SHA-256
     against `laptop_archive_sha256` for that platform. A SHA-256 that
     differs stops the install. Copy the binary to `~/.local/bin`, as
     [doubleshell installation](https://github.com/monkeymaxxing/doubleshell#install)
     says.
   - **set-up-solo-services:** the version of the Solo release whose skill
     you use, `solo_version` in the skill's
     `systems_repository_template/monkeymaxxing_solo_checklist.yml`. When
     `~/.local/bin/set-up-solo-services --version` already prints that
     version, keep it. Otherwise build it with Go 1.24 or newer from Solo's
     repository at the release's tag:

     ```sh
     # On the laptop. VERSION: solo_version, such as 0.1.0.
     git clone --depth 1 --branch vVERSION https://github.com/monkeymaxxing/monkeymaxxing-solo.git ~/.cache/monkeymaxxing-solo/source-vVERSION
     cd ~/.cache/monkeymaxxing-solo/source-vVERSION/set_up_solo_services && go build -o ~/.local/bin/set-up-solo-services .
     ```

     When the laptop has no Go, use the release's archive for the laptop's
     platform from the release page, checked against the release's
     `SHA256SUMS`, or install Go first.

   The laptop also needs Git, OpenSSH, and `ssh-keygen`. On Windows,
   every command runs inside WSL.

   **Keep going without stopping for a reply:** install the tools, start
   the systems repository in step 5, and read the app in
   [Get your app ready](02_get_your_app_ready.md) before you pause. Only
   stop for the person when you show them
   [the full map](00_before_you_start.md#the-map) together with the
   proposed app changes to approve (or when a tool install genuinely needs
   their input).

   **Check:** `~/.local/bin/mmx version` and `~/.local/bin/doubleshell version`
   print the pinned versions, and `~/.local/bin/set-up-solo-services --version`
   prints `solo_version`.

5. **Start the systems repository on the laptop.** `TEMPLATE_DIRECTORY` is
   `systems_repository_template/` beside the installed skill's `SKILL.md`.
   The repository's name is the company's or project's name in lowercase
   words with underscores, followed by `_systems`, such as `acme_systems`.
   Take the name from the app's README title or repository name.

   ```sh
   # On the laptop. Replace TEMPLATE_DIRECTORY with the directory's path:
   mkdir -p ~/code
   cp -R TEMPLATE_DIRECTORY ~/code/acme_systems
   git init -b main ~/code/acme_systems
   ```

   The template leaves each value that only this system's setup can know
   empty, with the comment "the setup fills it". Fill each one when its
   step comes, and replace the whole line, comment included, such as
   `project_name: acme_systems`. So
   `grep -n 'the setup fills it' mmx_project_settings.yml inventories/servers.yml`
   lists exactly the values still to fill.

   Write these values now:

   - `project_name: acme_systems` in `mmx_project_settings.yml`.
   - `nightly_work_starts_at` (UTC), a time when most users are away, and
     `maintenance_window`, a weekday evening, both in the users' region.
     `weekly_work_day` keeps `sunday`.
   - `solo_commit` in `monkeymaxxing_solo_checklist.yml`, from
     `git -C TEMPLATE_DIRECTORY rev-parse HEAD` when the skill came from a
     clone of Solo's repository. Without a clone, write the release's tag,
     such as `v0.1.0`.

   Commit: "Start the systems repository from Monkeymaxxing Solo".

   **Keep going in the same turn:** go straight into
   [Get your app ready](02_get_your_app_ready.md) to read the app's code
   and list its needed changes. Do not stop and wait for a reply while the
   person has nothing to act on. When you pause with the proposed app
   changes, include [the full map](00_before_you_start.md#the-map), one
   line confirming the tools and `~/code/acme_systems`, and the app-changes
   screen to approve.

   **Check:** `~/code/acme_systems` holds the template with the values
   above, and its first commit exists.

## If something is different

- **The person has no domain yet** (step 1): the person registers one at
  Cloudflare when the Cloudflare account exists, at the price Cloudflare
  shows. A domain registered at Cloudflare needs no nameserver change.
- **The laptop shows no password manager** (step 3): ask which one the
  person uses. Only when the person has none, recommend Bitwarden.
- **The laptop lacks Git or OpenSSH** (step 4): name each missing tool and
  install it with the laptop's package manager, after the person's yes.
- **A pin whose SHA-256 is all zeros** (step 4): the skill came from a
  Solo checkout that is not a release. Stop, and tell the person to
  install a released Solo version.
- **The person already has a server** with other software on it: Solo
  needs a new, empty Debian 13 server. Say so in one line, and go on.

## Why it works this way

**Two questions.** Only the domain and the apps need the person. Every
other value has a default that fits a side project, and the person sees
each default once, with the cost.

**Any company for the server.** Solo creates no server through a
company's API, so the person keeps no company key on the laptop, and every
company that sells a Debian 13 server works on day one.

Next, in the same reply: [Get your app ready](02_get_your_app_ready.md).
