# Rebuild solo01 on a new server

**When:** `solo01` is dead or cannot be repaired; the apps need a bigger
server; the person moves to another company; the laptop was lost or
stolen; or a new Solo version moves `solo01` to a new Debian release.

**What this does:** a new, empty Debian 13 server becomes `solo01`, with
the same settings from this repository and the database from the R2
backups. The order matters: `restore_database` runs first, then
`solo_server`. Reason: `solo_server` on an empty server would create a new,
empty backup history and then refuse to match the one in the bucket.

**Who acts:** the person creates the new server and, at the end, deletes
the old one. You do the rest. The site is down from the moment the old
server stops until the new one answers, about 30 minutes plus the time to
restore the database.

## Before you start

- **Approval.** A new server costs money, and the switch takes the site
  down. Tell the person both, with the reason for the rebuild, and wait for
  yes. A dead server needs no approval for the downtime: the site is down
  already.
- **The laptop's keys stay the same,** except after a lost laptop: the same
  `~/.ssh/solo01`, the same WireGuard key, and the same addresses,
  `10.20.1.1` for `solo01` and `10.20.10.1` for the laptop.
- **Tell the person to expect an alert** from the outside alarm while no
  server checks in. The recovery message follows when the new server checks
  in.

## Steps

1. **Stop the old server, when it still runs.** Two servers must never run
   at once: both would serve users through the same Cloudflare tunnel, and
   both would write to the same backup history. On the old `solo01`:

   ```sh
   # On the laptop, in the systems repository.
   ssh -F ssh/config root@10.20.1.1 'systemctl stop "*-web-*.service" "*-jobs.service"; sudo -u postgres pgbackrest --stanza=main backup --type=diff; systemctl stop cloudflared postgresql solo_outside_alarm_check_in.timer'
   ```

   The apps stop first, so no write is lost. The last backup and the
   archive then hold every change. Then ask the person to power off the
   old server at the company's console. Do not delete it yet.

   **Check:** the backup ended without error, and the old server is off.

2. **The new server.** The person creates it as in the initial setup's
   [Create your server and Cloudflare](../initial_setup/03_create_your_server_and_cloudflare.md),
   step 2, with the same `~/.ssh/solo01.pub` key, at the size or company
   the person chose. Write the new address into `public_ipv4_address` in
   `inventories/servers.yml`.

3. **The private connection.** Run step 3 of the same setup file on the new
   server. Then replace the old values: `wireguard_public_key` in
   `inventories/servers.yml`, the `10.20.1.1` line in `ssh/known_hosts`,
   and `PublicKey` and `Endpoint` in `~/.config/doubleshell/solo01.conf`.
   Commit ("Rebuild solo01 on a new server: the old one is dead") and push.

   **Check:** `ssh -F ssh/config root@10.20.1.1 hostname` answers from the
   new server.

4. **Restore the database first.**

   ```sh
   # On the laptop, in the systems repository.
   mmx run restore_database --var confirm_this_replaces_the_current_database=true
   ```

   The run installs Postgres 17 and pgBackRest, writes pgBackRest's
   settings, restores the newest data from R2, runs each app's check
   queries on the restored database, and turns archiving on again.

   **Check:** the run ends with no failed node, and each check query
   answered `t`.

5. **Then install everything.**

   ```sh
   # On the laptop, in the systems repository.
   mmx run solo_server
   ```

   The run closes the public SSH port, finds the backup history in the
   bucket, starts MMWatchdog and the check-in, the tunnel, and the apps at
   their tags in `deployed_image_tags`, and runs the restore test until it
   passes.

   **Check:** each app's public health URL answers `200`, and the outside
   alarm's recovery message arrived. Check the closed ports as the initial
   setup's
   [Install everything and go live](../initial_setup/04_install_everything_and_go_live.md),
   step 3, does.

6. **Delete the old server.** Ask the person to delete the old server at
   the company, once the new one has worked for a day. Record
   `solo01_can_be_rebuilt_from_git_and_r2` in the checklist with the date
   and the time the site was down. Update the README's link to the
   company's console when the company changed.

## A bigger server, or another company

Follow the steps above with the new size or the new company. Choose x86
(amd64), Debian 13, and at least the old size. A bigger server needs no
setting change: each app's web server reads the new CPU count from
`WEB_CONCURRENCY` at its next start.

## A new Debian release

A new Debian release brings a new Postgres major version, and pgBackRest
restores only into the same major version. A new Solo version that moves
to the new Debian release says how the database moves, in its
[Upgrade to a new Solo version](upgrade_to_a_new_solo_version.md) notes.
Never create a server with another Debian release than the one
`solo_version` names.

## After a lost or stolen laptop

The old laptop held the SSH key, the WireGuard key, and the vault
passphrase file. Treat each as known to someone else.

1. Ask the person to power off the old `solo01` at the company's console at
   once.
2. On the new laptop: install the tools as the initial setup's
   [Answer two questions](../initial_setup/01_answer_two_questions.md),
   step 4, says; clone the systems repository from GitHub; and ask the
   person to run `mmx secrets write-passphrase-file` with the passphrase
   from the password manager.
3. Make new laptop keys as step 1 of
   [Create your server and Cloudflare](../initial_setup/03_create_your_server_and_cloudflare.md)
   says, and replace `root_ssh_public_keys` and
   `laptop_wireguard_public_key`.
4. Rebuild with the steps above, skipping step 1.
5. Change every secret in the vault with
   [Rotate a secret](rotate_a_secret.md), and change the vault passphrase
   with `mmx secrets passwd`, run by the person.

## Why it works this way

**Everything is in Git and R2.** The repository holds every setting, the
vault holds every secret, and R2 holds the database. A new server needs
nothing from the old one.

**Never two servers at once.** One tunnel and one backup history must have
exactly one writer.
