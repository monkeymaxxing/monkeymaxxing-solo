# Before you start: the four parts of the setup, and how long they take

**When:** right after the person's "go", for the full map; at the end of
each part, for the compact map; and whenever the person asks where the
setup stands, or comes back to it, such as in a new session, after the
laptop slept, or the next day.

**What this file does:** the person always knows what is going to happen,
where the setup is now, what has happened, and what is left, and, for each
part, what you do and what the person does. The person sees the setup as
four parts, with the same names in the chat and in the systems
repository's `README.md`. The setup files are your own structure, and the
person never sees their names or numbers.

**Who acts:** you show the map. The person reads it and asks questions.
Nothing changes on a server or an account here.

## Before you start

- **The order.** Follow the setup files in order: each needs only the
  earlier ones.
- **Only what the steps say.** Do what each step says, and nothing more:
  no checks, scans, or experiments of your own. When something seems to
  need more, say so in one line, as a suggestion.
- **The messages to the person.** Each step says what to tell or ask the
  person, in quote blocks or `text` screens. Say each message when its step
  comes, in your own words, with the person's own names and links.
- **Commits.** Commit each step's changes to the systems repository as you
  go, with a message that says what changed and why, as
  [`AGENTS.md`, "Making a change"](../AGENTS.md#making-a-change) says.
- **Command blocks.** Every command block in the setup files starts with a
  comment that says where it runs.

## The map

Show the full map once, right after the person's "go", as its own screen.
The parts' times add up to the total: 5, 10, 20, and 5 minutes make about
40 minutes, so say "under an hour". Say "about", never an exact time.

**Show the person,** after "go":

```text
Here is the whole setup, in four parts.  Under an hour in all, and about 15 minutes of it is you.

  1. Your app                        about 5 minutes
     I get your app ready to run on your server.  You approve my changes.
  2. Your server and Cloudflare      about 10 minutes
     You create one server at a company you choose and paste its address,
     choose one passphrase, make one Cloudflare key, and run one command I give you.
     I connect your laptop to your server over a private connection.
  3. Install and go live             about 20 minutes
     I install everything on your server, back up your database, prove the backup
     restores, and put your app live.  Nothing for you.
  4. Alerts                          about 5 minutes
     You set up Pushover on your phone (or Slack, Discord, Telegram, a webhook,
     or email), and I send you a test alert.

By default, I'll go fast and only stop when I need you. Say "guide me" any time
and I'll slow down and explain each part before I do it. Say "go fast" to speed
up again. Ask "why?" about anything.

Now: I am installing three small tools and reading your app, about 5 minutes.
Next for you: approve my changes to your app, in about 5 minutes.  I show them here.
```

The facts on the screen:

- **Your app's changes.** When the app needs bigger changes first, such as
  a move from SQLite to Postgres, part 1's line reads "longer: your app
  needs bigger changes, which I show you first", and the first line says
  that part 1 can take days.
- **A domain new to Cloudflare.** When `dig NS DOMAIN +short` does not name
  Cloudflare's nameservers, add under part 2: "You also change one setting
  at your domain's registrar. Going live waits for that move, minutes to
  about a day."
- **The pace paragraph** stays as it stands. The skill's "The person's
  pace" holds the rules for "guide me", "go fast", and "why?".
- **The Now and Next lines.** Start the tools' install in the background
  before you end your reply with the map, so its end wakes you.

## The compact map

At the end of each part, show the compact map: done parts ticked, the
current part marked with an arrow, and the rest listed. Its first line is
the finished part's result. Until alerts are on, keep the last line "Your
site has no phone alerts yet: that is part 4." For example, at the end of
"Your app":

```text
Your app is ready: my changes are on main, and GitHub is building your app.

  ✓ 1. Your app
  → 2. Your server and Cloudflare     now, about 10 minutes
    3. Install and go live
    4. Alerts
    Your site has no phone alerts yet: that is part 4.
```

The first screen of the next part follows in the same reply, as its last
text. Between the ends of parts, give only the Now and Next lines, with no
map.

## Where are we

When the person asks where the setup stands, or comes back to it, your
first reply is the compact map, with the next step in the Now and Next
lines. Work out where the setup is from the files, never from the person.
Look in the order below and stop at the first row that matches:

| What you find | Where the setup is |
| --- | --- |
| No systems repository on the laptop, such as `~/code/acme_systems` | Before part 1: start [Answer two questions](01_answer_two_questions.md) |
| `project_name` in `mmx_project_settings.yml` is empty | Before part 1: [Answer two questions](01_answer_two_questions.md), step 5 |
| `deployed_image_tags` in `inventories/servers.yml` still holds the template's example tag, `3f2a1c97d4e85b60a1c2d3e4f5061728394a5b6c` | Part 1, "Your app": [Get your app ready](02_get_your_app_ready.md) |
| `public_ipv4_address` or `wireguard_public_key` in `inventories/servers.yml` is empty, or `ssh -F ssh/config root@10.20.1.1 hostname` fails | Part 2, "Your server and Cloudflare": [Create your server and Cloudflare](03_create_your_server_and_cloudflare.md), from the server steps |
| `cloudflare_account_id` or `outside_alarm.worker_url` in `mmx_project_settings.yml` is empty, or `mmx secrets usage` names a missing key | Part 2, "Your server and Cloudflare": [Create your server and Cloudflare](03_create_your_server_and_cloudflare.md), from the Cloudflare and secret steps |
| `the_site_is_reachable_only_through_the_cloudflare_tunnel` in `monkeymaxxing_solo_checklist.yml` is not `done`, or `public_site_checks_run` is `false` | Part 3, "Install and go live": [Install everything and go live](04_install_everything_and_go_live.md) |
| `alert_destination.enabled` is `false`, or `the_outside_alarm_fires_when_solo01_goes_silent` is not `done` | Part 4, "Alerts": [Turn on alerts](05_turn_on_alerts.md) |
| Every `part: setup` item is `done` or `deviated` | The setup is finished. Follow the systems repository's `AGENTS.md` for the person's request. |

## How long it takes

The map gives the times. When the person asks for more, say:

> The setup takes under an hour in all. I need you for about 15 minutes of it: mostly creating your server and your Cloudflare key, near the start.
>
> Two waits can be longer, and you have nothing to do during them:
>
> - If your domain is new to Cloudflare, its move takes from minutes to
>   about a day, and nothing speeds it up. I keep working meanwhile. Your
>   site goes live once the move is finished.
> - "Install and go live" takes about 20 minutes: installing everything,
>   the first backup, and a restore test, a check that your backup really
>   restores. Keep your laptop awake: I work from it.
>
> If your app needs bigger changes first, such as moving from SQLite to
> Postgres or its uploads to Cloudflare's file storage, "Your app" can
> take days.
>
> After the four parts, your site is live, alerts reach your phone, and
> your backups are proven to restore. Then you have one regular duty: once
> a week, you and I do the weekly system review, about 10 minutes.

Next: [Answer two questions](01_answer_two_questions.md).
