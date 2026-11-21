# Recover lost rows from backups

**When:** rows were deleted or changed by mistake, such as by a bad
release, a wrong query, or a user; or the database is damaged.

**What this does:** pgBackRest keeps every database change in R2, so the
database can come back as it was at any moment the backups cover, which is
about the last 3 weeks (`keep_database_backups_for_days`). There are two
ways:

| Way | When | Effect on the live site |
| --- | --- | --- |
| [Inspect a copy](#inspect-a-copy-and-copy-rows-back) | A few rows or tables are wrong, and everything else must stay as it is now | None: the live database keeps running. You copy the lost rows back with the person's yes. |
| [Restore the whole database to a time](#restore-the-whole-database-to-a-time) | The whole database is damaged, or the person chooses to lose everything after the bad moment | The apps stop, and every change after the chosen time is lost. |

Recommend inspecting a copy first, unless the whole database is damaged.

**Who acts:** you. The person tells you when the mistake happened, and
approves each change to live data.

## Before you start

- **The moment.** Ask the person when the mistake happened, in their local
  time. Convert it to UTC with its time zone, such as
  `2026-11-03 14:01:00+00`, and pick a moment one minute before it.
- **Disk.** A copy needs free disk on `solo01` about as large as the
  database: compare `df -h /var/lib` with
  `sudo -u postgres psql -c "select pg_size_pretty(sum(pg_database_size(datname))) from pg_database"`.
  When the disk is too small, tell the person, and offer a bigger server
  first.

## Inspect a copy, and copy rows back

1. **Restore a copy beside the live database.** On `solo01`, as root,
   restore into a scratch directory, the way the weekly restore test does,
   but to the chosen moment. Then start the copy as a second Postgres with
   `solo01`'s own Postgres 17 programs, which fetch the archived changes up
   to that moment from R2:

   ```sh
   # On solo01, through ssh -F ssh/config root@10.20.1.1. TIME: such as "2026-11-03 14:00:00+00".
   install -d -o postgres -g postgres -m 0700 /var/lib/solo-recovery /var/lib/solo-recovery/data
   runuser -u postgres -- pgbackrest --config=/etc/pgbackrest/pgbackrest.conf --stanza=main \
       --pg1-path=/var/lib/solo-recovery/data --type=time --target="TIME" \
       --target-action=promote --archive-mode=off restore
   printf '%s\n' 'local all all trust' > /var/lib/solo-recovery/data/pg_hba.conf
   : > /var/lib/solo-recovery/data/postgresql.conf
   : > /var/lib/solo-recovery/data/pg_ident.conf
   chown postgres:postgres /var/lib/solo-recovery/data/*.conf
   runuser -u postgres -- /usr/lib/postgresql/17/bin/pg_ctl --pgdata=/var/lib/solo-recovery/data \
       --log=/var/lib/solo-recovery/postgres.log --wait --timeout=3600 \
       --options="-c port=5433 -c listen_addresses= -c unix_socket_directories=/var/lib/solo-recovery -c archive_mode=off -c max_connections=100" start
   ```

   The copy listens on no network address, only on its own socket in
   `/var/lib/solo-recovery`, and its archive is off, so it can never write
   to the live backups. Restoring the copy from R2 takes about as long as
   the weekly restore test.

   **Check:**
   `runuser -u postgres -- psql --host=/var/lib/solo-recovery --port=5433 --dbname=shop -c 'select count(*) from orders'`
   answers with the count before the mistake.

2. **Find the lost rows.** Compare the copy with the live database, such as
   the orders that exist in the copy and not in the live database. Tell the
   person what you found in their words: "47 orders from 3 November, 9:00
   to 14:00 your time, are in the backup and missing from the site."

3. **Ask before copying back.**

   > I can copy the 47 orders back into your live database. Nothing else changes. Copy them now?

   With the person's yes, export the rows from the copy with
   `runuser -u postgres -- psql --host=/var/lib/solo-recovery --port=5433 --dbname=shop -c "\copy (select ...) to stdout with csv"`
   and import them into the live database in one transaction, as the app's
   own login, with `psql` on `solo01`. Check for rows that the app changed
   since, and ask the person about each conflict.

   **Check:** the live database holds the rows, and the app shows them.

4. **Remove the copy.**

   ```sh
   # On solo01, as root.
   runuser -u postgres -- /usr/lib/postgresql/17/bin/pg_ctl --pgdata=/var/lib/solo-recovery/data --mode=fast stop
   rm -rf --one-file-system /var/lib/solo-recovery
   ```

## Restore the whole database to a time

1. **Ask.** Say plainly: the apps stop for the restore, and every change
   after the chosen moment is lost, for every app on `solo01`. The current
   data directory is kept on `solo01`, moved aside, until the person
   decides to delete it.

   > Your whole database goes back to 3 November, 14:00 your time. Everything written after that, in every app, is lost. Your apps stop for about 10 minutes. Go ahead?

2. **Run.**

   ```sh
   # On the laptop, in the systems repository. TIME: such as "2026-11-03 14:00:00+00".
   mmx run restore_database --var confirm_this_replaces_the_current_database=true --var target_time="TIME"
   ```

   Without `target_time`, the database comes back with the newest data
   the archive holds, which is the way to repair a damaged database.

   **Check:** the run ends with no failed node, each check query answered
   `t`, and each app's public health URL answers `200`. Record
   `a_restore_to_a_point_in_time_works` in the checklist.

3. **Remove the old data directory** only when the person says the
   restored database is right:
   `rm -rf /var/lib/postgresql/17/main_replaced_by_restore_*` on `solo01`.

## Why it works this way

**A copy first.** Copying rows back changes only what was lost, and the
users keep everything they did since. A whole restore is simpler, but loses
every later change.
