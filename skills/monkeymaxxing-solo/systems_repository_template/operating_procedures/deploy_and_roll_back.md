# Deploy and roll back

**When:** the person wants a new version of an app live, or the live
version is bad and must go back to the previous one.

**What this does:** `playbooks/deploy.yml` starts the new version beside
the old one, checks its health URL, switches Caddy to it without dropping a
connection, and stops the old version. A rollback is the same deploy with
the previous tag. The previous image stays on `solo01` for at least 7 days.

**Who acts:** you. The person approves each release while
`production_releases_wait_for_a_persons_approval` is `true`.

## A release

1. **Find the tested version.** The tag is the full 40-character commit of
   the app's `main` whose workflow at GitHub passed, including its last
   step, "Check that the registry lists the tag". With `gh`:
   `gh run list --repo OWNER/APP --branch main --limit 5`.

   **Check:** the workflow of that commit passed.

2. **Ask, when the setting says so.** While
   `production_releases_wait_for_a_persons_approval` is `true`:

   > The new version of shop is tested: 3 changes since the live version, "Add the order export" and 2 others. One database change adds a column. Release it now?

   Name every database migration in the release: a migration cannot be
   undone by a rollback.

   **Check:** the person said yes, or the setting is `false`.

3. **Write the tag.** Change the app's line in `deployed_image_tags` in
   `inventories/servers.yml`. Run `mmx validate deploy`. Commit ("Release
   shop 3f2a1c9: the order export") and push.

4. **Preview and run.**

   ```sh
   # On the laptop, in the systems repository.
   mmx dryrun deploy
   mmx run deploy
   ```

   The deploy checks memory for a second copy of the live version, pulls
   the image, runs the migrations in a one-off container with a 5-second
   lock limit, starts the new version on the free port, waits for its
   health URL, switches Caddy, stops the old version, restarts the job
   container, and checks each app through its public address.

   **Check:** the run ends with no failed node, and
   `curl -sS -o /dev/null -w '%{http_code}\n' https://HOST/HEALTH` prints
   `200`.

5. **Tell the person** in one line: "shop's new version is live, with the
   order export." When the deploy failed before the switch, say that users
   stay on the old version, what failed, and what you do next.

## A rollback

1. **Find the previous tag** in the history of `inventories/servers.yml`:
   `git log -p -- inventories/servers.yml`. Use the last tag that worked.
2. **Check the migrations.** When the bad release ran a migration that
   removed or renamed something, the previous version can fail on the new
   database. Tell the person, and ask before going on. Fixing forward with
   a new release can be safer.
3. **Write, commit, push, and run** as steps 3 and 4 of "A release" say,
   with a message such as "Roll shop back to 1a2b3c4: the order export
   fails". A rollback of a bad release needs no approval: it returns to a
   version the person already approved.
4. **Record** the first rollback in the checklist:
   `a_rollback_returns_to_the_previous_release`.

## If something is different

- **The memory check stops the deploy:** `solo01` lacks memory for two
  copies of the app for the minute of the switch. The message gives the
  numbers. Offer a smaller `WEB_CONCURRENCY` in the app's `env`, or a
  bigger server with [Rebuild solo01 on a new server](rebuild_solo01_on_a_new_server.md).
  For the rollback of a version that leaks memory, and only with the
  person's yes: `mmx run deploy --var skip_the_memory_check=true`.
- **A migration waits for a lock and fails:** nothing switched. Users stay
  on the old version. Rewrite the migration so it waits for no long lock,
  and release again.
- **The new version fails its health check:** it is removed, and users stay
  on the old version. Read its log with
  `ssh -F ssh/config root@10.20.1.1 journalctl -u 'shop-web-*' -n 100`.

## Why it works this way

**Start beside, then switch.** Users never see a version that has not
answered its health URL, and the switch drops no connection.

**A rollback is a release.** One path for both means the rollback is tested
every time a release runs.
