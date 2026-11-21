# Turn on alerts

**When:** right after the live screen. This is part 4, "Alerts".

**What this file does:** alerts from MMWatchdog on `solo01` and from the
outside alarm in Cloudflare reach the person's phone. Pushover is the
default. Slack, Discord, Telegram, a webhook, or email work too. You test
both senders, finish the checklist, and write the person's README.

**Who acts:** the person makes the Pushover account (or picks another
channel), types its keys at a hidden prompt, and confirms that the test
alerts arrived. You do the rest.

**Tell the person when this starts:** the Pushover screen in step 1, under
the live screen.

## Before you start

- **Which channel.** Use Pushover unless the person asks for another one.
  Reason: a phone alert that repeats until acknowledged is what wakes a
  person, and Pushover costs about $5 once.
- **Email reaches the person only from MMWatchdog.** The outside alarm sends
  HTTPS requests only, so with email it cannot tell the person that
  `solo01` itself went silent. Say so in one line when the person picks
  email, and recommend any other kind.

## Steps

1. **The keys of the channel.**

   **Ask the person,** for Pushover:

   ```text
   Your phone alerts, with Pushover

   What this is for:  your server and the outside alarm send alerts to your phone.
   Pushover costs about $5 once, after a free 30-day trial.

   Do this now:
     1. Install Pushover on your phone, and sign up at https://pushover.net
     2. Copy "Your User Key" from the top of https://pushover.net
     3. Open https://pushover.net/apps/build, name the application "acme_systems",
        and click "Create Application".  Copy its "API Token/Key".
     4. Run this in your own terminal window, and paste the two keys when asked:

   cd ~/code/acme_systems
   export PATH="$HOME/.local/bin:$PATH" MMX_VAULT_PASSWORD_FILE="$HOME/.config/mmx/vault-passphrase-acme_systems"
   set-up-solo-services secrets set-pushover --repo-dir .

   Then say "done".
   ```

   `secrets set-pushover` checks both keys with Pushover before it saves
   them as `pushover_application_token` and `pushover_user_or_group_key`.

   For another channel, the person types its key with
   `set-up-solo-services secrets set --repo-dir . --key KEY --prompt "LABEL"`,
   and you write its settings into `alert_destination` in
   `mmx_project_settings.yml`:

   | Kind | Vault key the person types | Settings you write |
   | --- | --- | --- |
   | `slack` | `slack_webhook_url` | none |
   | `discord` | `discord_webhook_url` | none |
   | `telegram` | `telegram_bot_token` | `telegram_chat_id`, in quotes |
   | `webhook` | `alert_webhook_bearer_token`, when the webhook takes one | `webhook_url`, `webhook_has_a_bearer_token` |
   | `email` | `email_login_password`, when the server needs a login | `email_smtp_server`, `email_connection_security`, `email_from`, `email_to`, `email_login_user` |

   **Check:** `set-up-solo-services secrets list --repo-dir .` lists the
   channel's keys.

2. **Turn the alerts on.** Set `alert_destination.enabled: true` and
   `alert_destination.kind` in `mmx_project_settings.yml`. Commit ("Turn
   on alerts to Pushover"), push, and run `mmx run solo_server`.

   **Check:** the run ends with no failed node,
   `ssh -F ssh/config root@10.20.1.1 mmwatchdog config --json` shows the
   destination `alerts_to_you`, and
   `ssh -F ssh/config root@10.20.1.1 '/usr/local/sbin/send_outside_alarm_check_in.py --print-payload' | python3 -c 'import json,sys; print(len(json.load(sys.stdin)["health_checks"]))'`
   prints the number of apps (assuming `public_site_checks_run` is `true`
   and the channel is not `email`).

3. **Test both senders.** Tell the person that two test alerts come now,
   one from the server and one from the outside alarm.

   ```sh
   # On the laptop, in the systems repository.
   ssh -F ssh/config root@10.20.1.1 'mmwatchdog send-notice --alert-severity warn --title "Test alert from solo01" --destination alerts_to_you'
   ssh -F ssh/config root@10.20.1.1 /usr/local/sbin/send_outside_alarm_check_in.py --send-test
   ```

   **Ask the person:** "Did two test alerts arrive on your phone, one from
   your server and one from the outside alarm?"

   **Check:** the person saw both. Mark
   `mmwatchdog_runs_and_its_alert_reaches_a_person` `done`, with what the
   person saw in `last_result`.

4. **Test the outside alarm for a silent server.** Offer it in one line:
   it takes about 7 minutes and proves that the alarm fires when `solo01`
   stops. With the person's yes:

   ```sh
   # On the laptop, in the systems repository.
   ssh -F ssh/config root@10.20.1.1 systemctl stop solo_outside_alarm_check_in.timer
   ```

   Wait `outside_alarm.delay_seconds` and about a minute more. The held
   alert arrives. Then start the timer again with
   `systemctl start solo_outside_alarm_check_in.timer`, and the recovery
   message arrives.

   **Check:** the person saw the alert and the recovery. Mark
   `the_outside_alarm_fires_when_solo01_goes_silent` `done`. When the
   person declines, mark it `done` from the `--send-test` result of step 3,
   and say in `last_result` that the silent test did not run. The edge
   health check of each app proves itself the same way: mark
   `the_outside_alarm_checks_each_apps_public_health_url` `done` from step
   2 above when the person declines its own outage test.

5. **Finish the checklist and the README.** Mark
   `secrets_never_appeared_in_the_conversation` and
   `solo01_can_be_rebuilt_from_git_and_r2` `done` after their checks. Every
   `part: setup` item is now `done` or `deviated`. Fill the lines marked
   "the setup fills it" in `README.md`: the site's addresses, the
   repository's link, the app's Actions page, the company's console, the
   alert channel, and the next actions with dates, such as the first weekly
   system review. Commit ("The setup is finished") and push.

   **Check:** `grep -n 'the setup fills it' README.md` lists nothing, and
   every `part: setup` item is `done` or `deviated`.

6. **The finish screen.**

   ```text
   Your setup is finished.

     ✓ 1. Your app
     ✓ 2. Your server and Cloudflare
     ✓ 3. Install and go live
     ✓ 4. Alerts

   Your site is live at https://example.org.  Alerts reach your phone, your backups are locked
   in Cloudflare, and the restore test passed.

   Your one regular duty: once a week, say "Do the weekly system review." It takes about 10 minutes.
   Your settings and what to ask me are in the README of acme_systems at GitHub.
   ```

## If something is different

- **Pushover rejects the keys** (step 1): the command names the problem,
  such as "user identifier is invalid", and saves nothing. Ask the person
  to copy the key again and run the same command.
- **A test alert does not arrive** (step 3): check the phone's
  notification settings for the app with the person, then the vault key
  with `--replace`. Never mark the item `done` before the person saw an
  alert.
- **The person wants alerts later:** leave `alert_destination.enabled:
  false`, and say plainly on every compact map and in the README that the
  site has no alerts. The setup is not finished until alerts work.

## Why it works this way

**Two senders.** MMWatchdog on `solo01` knows the most about the server,
but it cannot say that the server itself froze. The outside alarm runs in
Cloudflare, so it alerts when `solo01` goes silent or the site stops
answering from the internet.

**The site goes live before alerts.** A person can see the site before
signing up anywhere else, and the alert keys never block going live.
