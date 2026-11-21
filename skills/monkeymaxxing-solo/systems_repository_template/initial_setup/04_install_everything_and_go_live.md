# Install everything and go live

**When:** right after "Your server and Cloudflare". This is part 3,
"Install and go live".

**What this file does:** one MMX run installs everything on `solo01` and
closes its public SSH port. The run takes the first backup, proves the
backup restores, and starts the apps behind the Cloudflare tunnel. Then you
prove that the site answers through Cloudflare and that `solo01` has no
open port but WireGuard, turn on the outside alarm's checks of the public
site, and show the person the live screen.

**Who acts:** you. The person has nothing to do, and the laptop must stay
awake: MMX runs from it.

**Tell the person when this starts:** the Now and Next lines of the
compact map already said it.

## Before you start

- Run every `mmx` command as
  [`AGENTS.md`, "Running MMX from the laptop"](../AGENTS.md#running-mmx-from-the-laptop)
  says, in the systems repository, after committing and pushing.
- `grep -n 'the setup fills it' mmx_project_settings.yml inventories/servers.yml`
  lists nothing.

## Steps

1. **Validate and preview.**

   ```sh
   # On the laptop, in the systems repository.
   mmx validate solo_server
   mmx dryrun solo_server
   ```

   The first dryrun copies MMX's program to `solo01`. It can report
   `not_checked` for work that depends on earlier work, such as a file in a
   directory that a package creates. That is expected on a new server.

   **Check:** `mmx validate` prints `Valid`, and the dryrun's summary shows
   no failed node.

2. **Install everything.** The person already approved the initial install
   with "go", so do not pause to ask for approval after `mmx dryrun solo_server`.
   Go straight into `mmx run solo_server` and run it to completion (about
   10 to 20 minutes; do not pause and wait for a reply from the person
   while it runs).

   ```sh
   # On the laptop, in the systems repository.
   mmx run solo_server
   ```

   The run, in order:

   1. Refuses programs pinned without a release, values the setup has not
      filled, and settings outside their limits, before any change.
   2. Sets up the base server: the name `solo01`, security updates every
      day, the firewall, WireGuard with MMX's own settings and the UDP 443
      redirect, and SSH on `10.20.1.1` only. From here, the public SSH port
      is closed.
   3. Installs Postgres 17, one database and login for each app, and
      pgBackRest, then takes the first full backup into the locked R2
      bucket.
   4. Starts MMWatchdog and the check-in with the outside alarm, before the
      apps, so a failed app still leaves MMWatchdog watching.
   5. Installs Caddy and `cloudflared`, runs each app's migrations, and
      starts each app's web and job containers.
   6. Runs the restore test: the newest backup restores into a throwaway
      Postgres on a private Unix socket, each app's check queries answer
      true, and that Postgres and its files are removed.

   Give the person one line about every 10 minutes, in their words, such
   as "Your database is installed and its first backup is in Cloudflare."

   **Check:** the run ends with no failed node. When it fails, read
   `mmx results RUN_ID`, fix the cause in the repository, commit, push, and
   run again: every step is safe to run twice.

3. **Prove the server is closed.** From the laptop, outside WireGuard, try
   the public address:

   ```sh
   # On the laptop. ADDRESS: solo01's public IPv4 address.
   for port in 22 80 443; do nc -z -w 5 ADDRESS $port && echo "open: $port" || echo "closed: $port"; done
   ssh -F ssh/config root@10.20.1.1 'ufw status verbose; sshd -T | grep -E "^(listenaddress|permitrootlogin)"; ss -ltn'
   ```

   **Check:** each TCP port prints `closed`. `ufw` shows "deny (incoming)"
   with only UDP 443, UDP 51820, and TCP 22 on `wg0` from `10.20.10.1`.
   `sshd -T` shows only `listenaddress 10.20.1.1:22` and
   `permitrootlogin without-password`. `ss -ltn` shows Postgres only on
   `10.20.1.1:5432`. Mark `ssh_reaches_solo01_only_through_wireguard`,
   `the_firewall_admits_only_wireguard`, and
   `postgres_17_listens_only_on_the_wireguard_address` `done`, with what
   you saw in `last_result`.

4. **Prove the site answers through Cloudflare.** Only now look up the
   app's address from the laptop: an earlier lookup can leave the laptop
   with "no such address" for minutes.

   ```sh
   # On the laptop, for each app. HOST: its public host name. HEALTH_PATH: its health_url_path (starts with /).
   curl -sS -o /dev/null -w '%{http_code}\n' "https://${HOST}${HEALTH_PATH}"
   ```

   **Check:** each prints `200`. Mark
   `the_site_is_reachable_only_through_the_cloudflare_tunnel` `done`.

   Finish steps 6, 7, and 8 below before pausing for the person, then show
   the live screen (step 5) together with the Alerts screen from
   [Turn on alerts](05_turn_on_alerts.md) in one reply so the person sees
   their live site and has the next action right below it.

5. **The live screen.** Show it right above the Alerts screen when you
   pause after step 8:

   ```text
   Your site is live:  https://example.org

   Open it now.

     ✓ 1. Your app
     ✓ 2. Your server and Cloudflare
     ✓ 3. Install and go live
     → 4. Alerts                         next, about 5 minutes
     Your site has no phone alerts yet: that is the next part.

   Your server has no open web port and no public SSH port.  Your database is backed up
   to Cloudflare, locked for 14 days, and the restore test passed.
   ```

   Under it, at most four lines of what is still open, in the person's
   words, then the Alerts screen.

6. **Turn on the outside alarm's checks of the public site.** Set
   `public_site_checks_run: true` in `mmx_project_settings.yml`. Commit
   ("The site answers through Cloudflare: turn on the public site checks"),
   push, and run `mmx run solo_server`.

   The check-in script starts sending the public site checks once alerts
   are turned on in [Turn on alerts](05_turn_on_alerts.md) (step 2 there
   verifies that every app appears in `health_checks`).

7. **Record what the run proved.** For each item below, check its
   `verification` in `monkeymaxxing_solo_checklist.yml` and mark it `done`
   with `verified_at` and `last_result`:
   `security_updates_install_daily_and_no_server_reboots_by_itself`,
   `each_app_has_its_own_database_and_login`,
   `backups_reach_the_r2_bucket_encrypted`,
   `the_weekly_local_restore_check_passes`,
   `apps_run_in_containers_with_a_confined_profile`, and each app's seven
   items. For `the_bucket_lock_stops_deleting_backups`, run this check on
   `solo01` with `solo01`'s own R2 key (it reads the key from
   `/etc/pgbackrest/pgbackrest.conf` without printing it and passes it to
   `curl` over stdin):

   ```sh
   # On solo01, as root (via ssh -F ssh/config root@10.20.1.1):
   bash -euo pipefail <<'CHECK_BUCKET_LOCK'
   label=$(sudo -u postgres pgbackrest --stanza=main info --output=json | jq -r '.[0].backup[-1].label')
   endpoint=$(awk -F= '/^repo1-s3-endpoint=/{print $2}' /etc/pgbackrest/pgbackrest.conf)
   bucket=$(awk -F= '/^repo1-s3-bucket=/{print $2}' /etc/pgbackrest/pgbackrest.conf)
   key_id=$(awk -F= '/^repo1-s3-key=/{print $2}' /etc/pgbackrest/pgbackrest.conf)
   secret=$(awk -F= '/^repo1-s3-key-secret=/{print $2}' /etc/pgbackrest/pgbackrest.conf)
   url="https://${endpoint}/${bucket}/pgbackrest/backup/main/${label}/backup.manifest"
   del_code=$(printf 'user = "%s:%s"\n' "$key_id" "$secret" | curl -sS -o /dev/null -w '%{http_code}' -K - --aws-sigv4 "aws:amz:auto:s3" -X DELETE "$url")
   put_code=$(printf 'user = "%s:%s"\n' "$key_id" "$secret" | curl -sS -o /dev/null -w '%{http_code}' -K - --aws-sigv4 "aws:amz:auto:s3" -X PUT --data-binary 'overwrite-test' "$url")
   echo "delete_http_status=${del_code} overwrite_http_status=${put_code}"
   CHECK_BUCKET_LOCK
   ```

   **Check:** both `delete_http_status` and `overwrite_http_status` are
   `409` or `403` (R2 refuses both deleting and overwriting the locked
   backup object). Commit and push.

8. **The first release.** Prove that a release switches with no downtime.
   The person approved an empty commit to the app for this in "Your app".
   Push it, wait for its image, write its tag into `deployed_image_tags`,
   commit, push, and run `mmx run deploy` while a loop asks the public
   health URL every half second. While
   `first_release_waits_for_a_persons_approval` is `false`, this release
   needs no further yes.

   **Check:** the deploy ended with no failed node, and the loop had no
   failed request. Mark `a_deploy_switches_without_dropping_requests`
   `done`, and tell the person in one line.

## If something is different

- **The run stops on a value or a setting** (step 2): its message names
  the setting and says "Nothing changed". Fix the value, commit, push, and
  run again.
- **The run cannot reach `solo01` after the base server step** (step 2):
  SSH now works only through WireGuard. Run
  `~/.local/bin/doubleshell --wireguard-config ~/.config/doubleshell/solo01.conf list-machines`
  and check the handshake. When WireGuard does not answer, use the
  company's web console for `solo01` with the person, and say so in one
  line.
- **The domain is still "Pending" at Cloudflare** (step 4): the site
  cannot answer yet. Tell the person, keep the live screen until it
  answers, and check again every 10 minutes.
- **An app does not start** (step 2): users see nothing yet, so nothing is
  harmed. Read the app's log with
  `ssh -F ssh/config root@10.20.1.1 journalctl -u 'shop-web-*' -n 50`,
  fix the app or its settings, and run again.
- **The restore test fails** (step 2): read
  `/var/lib/solo-restore-check/last-result.json` on `solo01`. A check
  query that answers false on a new, empty app is the usual cause: change
  the query to one the app's first migration makes true.

## Why it works this way

**One run sets up everything.** The same playbook builds a new server and
keeps a running one the same, so a rebuilt `solo01` gets exactly what this
one has.

**Monitoring before the apps.** A failed app start must never leave the
server unwatched.

**A restore test before going live.** A backup is only proven by a restore,
so the site goes live with backups that are known to work.

Next: [Turn on alerts](05_turn_on_alerts.md).
