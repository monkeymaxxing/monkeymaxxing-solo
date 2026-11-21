# Rotate a secret

**When:** a secret leaked or might have leaked; a person who knew one
left; a company asks for a new key; or after a lost laptop.

**What this does:** a new value replaces the old one in the vault, one
`solo_server` run puts it on `solo01`, and the old value stops working at
the company that issued it.

**Who acts:** you, for secrets that a command makes. The person, for keys
typed at a hidden prompt and for each step at a company's website.

## Before you start

- **One secret at a time,** unless the laptop was lost. After each, check
  that the site answers before the next.
- **Tell the person** which secret changes, why, and whether users notice:
  a new signing secret, such as `SECRET_KEY_BASE`, signs every user out.

## Steps

1. **Make the new value,** by the key's kind:

   | Key | How to make the new value |
   | --- | --- |
   | `<database_name>_database_password`, `<app>_<VARIABLE>` with `generated_by_the_setup` | You: `set-up-solo-services secrets generate --repo-dir . --key KEY --replace`, with the same `--length` as before |
   | `<app>_<VARIABLE>` with `an_outside_service`, `container_registry_read_only_token` | The person makes a new key at the company, then types it: `set-up-solo-services secrets set --repo-dir . --key KEY --replace --prompt "LABEL"` |
   | `pushover_application_token`, `pushover_user_or_group_key` | The person: `set-up-solo-services secrets set-pushover --repo-dir . --replace` |
   | `slack_webhook_url`, `discord_webhook_url`, `telegram_bot_token`, `alert_webhook_bearer_token`, `email_login_password` | The person makes a new one at the company, then types it with `secrets set --replace` |
   | `pgbackrest_r2_access_key_id` and `pgbackrest_r2_secret_access_key`, or an app's `<app>_R2_ACCESS_KEY_ID` and `<app>_R2_SECRET_ACCESS_KEY` | The person deletes the R2 key named `solo-r2-backup`, or `solo-r2-<app>-uploads`, from the account API tokens at Cloudflare, then runs `set-up-solo-services cloudflare` again with the same flags as the setup. The command finds the key gone, makes a new one, and saves both values. |
   | `outside_alarm_check_in_token` | The person deletes the Worker's secret `SOLO_OUTSIDE_ALARM_TOKEN` at Cloudflare (Workers, `solo-outside-alarm`, Settings, Variables and Secrets), then runs `set-up-solo-services cloudflare` again. The command makes a new token and sets it on both sides. |
   | `cloudflare_tunnel_token` | The person refreshes the tunnel's token at Cloudflare (Zero Trust, Networks, Tunnels, `solo01`), copies the new token from the install command Cloudflare shows, and types it: `secrets set --key cloudflare_tunnel_token --replace` |
   | `pgbackrest_repository_cipher_passphrase` | Not in place: every backup is encrypted with it. See [The backup passphrase](#the-backup-passphrase) below. |

   **Check:** `set-up-solo-services secrets list --repo-dir .` still lists
   the key. Commit the vault ("Rotate shop_STRIPE_SECRET_KEY: the old key
   leaked") and push.

2. **Put the new value on `solo01`.** Run `mmx dryrun solo_server`, then
   `mmx run solo_server`. The run changes only the files that hold the key,
   and restarts what reads them.

   **Check:** each app's public health URL answers `200`, and the dryrun
   showed only the files of that key. For an alert key, send a test as
   [Turn on alerts](../initial_setup/05_turn_on_alerts.md), step 3, does.

3. **Revoke the old value** at the company that issued it, such as the old
   GitHub key or the old Stripe key. A database password and a generated
   app secret stop working with the run.

   **Check:** the old value is refused. Record
   `a_secret_rotation_reaches_the_server` in the checklist the first time.

## The vault passphrase

The person runs `mmx secrets passwd` in their own terminal, saves the new
passphrase in the password manager, then runs
`mmx secrets write-passphrase-file` again. Commit the vault. Old copies of
the vault in Git history keep the old passphrase, so rotate each secret the
old passphrase could open when the old passphrase leaked.

## The laptop's SSH and WireGuard keys

Follow "After a lost or stolen laptop" in
[Rebuild solo01 on a new server](rebuild_solo01_on_a_new_server.md#after-a-lost-or-stolen-laptop).

## The backup passphrase

pgBackRest encrypts every backup with
`pgbackrest_repository_cipher_passphrase`, so a new passphrase needs a new
backup history in a new bucket. With the person's yes: delete the R2 key
`solo-r2-backup` at Cloudflare, run `set-up-solo-services cloudflare` with a
new `--backup-bucket`, generate the passphrase with `--replace`, write the
new bucket into `backups.bucket_name`, and run `mmx run solo_server`, which
starts the new history with a full backup. Keep the old bucket until its
backups pass `keep_database_backups_for_days`, then ask the person to
delete it.

## Why it works this way

**The vault first, then one run.** The repository always says which value
runs, and `solo01` gets the new value the same way as every other change.
