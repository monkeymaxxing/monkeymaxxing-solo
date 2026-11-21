# Add another app

**When:** the person wants a second (or third) app on `solo01`.

**What this does:** the new app gets its own Postgres database and login,
its own containers and web ports, its own address through the Cloudflare
tunnel, and its own uploads bucket, beside the apps that already run.

**Who acts:** you. The person approves the changes to the new app, makes a
new Cloudflare key, runs one command, and types any key the app needs from
another company.

## Before you start

- **Memory.** Each app's web containers run twice for the minute of a
  release. Read the memory in use with
  `ssh -F ssh/config root@10.20.1.1 free -m`. When the apps together would
  leave less than about 1 GB free, tell the person, and offer a bigger
  server with [Rebuild solo01 on a new server](rebuild_solo01_on_a_new_server.md)
  first.
- **Names.** The app's name is lowercase letters, digits, and underscores,
  such as `blog`. Its address is under the domain, such as
  `blog.example.org`.

## Steps

1. **Get the app ready.** Follow steps 1 to 4 of the initial setup's
   [Get your app ready](../initial_setup/02_get_your_app_ready.md): the
   seven app contract items, the person's yes for the changes, the
   workflow at GitHub, and the first image with its full commit tag. Copy
   the seven checklist items under `apps` for the new app.

2. **Make sure the read-only key can read the new image.** When the new
   app's image belongs to another GitHub owner than the first app, the
   person whose login is `container_registry_read_only_username` needs read
   access to it.

## Write the app into the systems repository

3. **Settings and inventory.** Add the app's block under `vars.apps` in
   `mmx_project_settings.yml`, with web ports no other app uses, such as
   8003 and 8004. Add its tag to `deployed_image_tags` and its address to
   `public_host_names` in `inventories/servers.yml`.

4. **The role calls in `playbooks/solo_server.yml`.** Add one line for the
   app's database to `database_passwords`, keyed by `database_name`, and one
   line to `app_secrets` for each vault key the app reads. For an app
   `blog` with the database `blog` and `SECRET_KEY_BASE`:

   ```yaml
             database_passwords:
               shop: "{{ .secrets.shop_database_password }}"
               blog: "{{ .secrets.blog_database_password }}"
   ```

   ```yaml
             app_secrets:
               shop_database_password: "{{ .secrets.shop_database_password }}"
               # ...the lines of shop...
               blog_database_password: "{{ .secrets.blog_database_password }}"
               blog_SECRET_KEY_BASE: "{{ .secrets.blog_SECRET_KEY_BASE }}"
               blog_R2_ACCESS_KEY_ID: "{{ .secrets.blog_R2_ACCESS_KEY_ID }}"
               blog_R2_SECRET_ACCESS_KEY: "{{ .secrets.blog_R2_SECRET_ACCESS_KEY }}"
   ```

   Commit ("Add blog").

5. **Cloudflare.** The new address and the new uploads bucket need one more
   run of `set-up-solo-services cloudflare`, with every app in `--apps`,
   old and new. A second run finds what the first one made and adds only
   what is new. The person makes a new Cloudflare key with the same
   permissions as in the initial setup's
   [Create your server and Cloudflare](../initial_setup/03_create_your_server_and_cloudflare.md),
   step 6, and runs the command in their own terminal. Use the same
   `--backup-bucket` as before.

   **Check:** `set-up-solo-services secrets list --repo-dir .` lists
   `blog_R2_ACCESS_KEY_ID` and `blog_R2_SECRET_ACCESS_KEY` for an app with
   uploads.

6. **The app's other secrets.** Generate `blog_database_password` and each
   `generated_by_the_setup` key with `set-up-solo-services secrets generate`.
   The person types each `an_outside_service` key with
   `set-up-solo-services secrets set`, as the initial setup's steps 7 and 8
   say.

   **Check:** `mmx secrets usage` shows every key of the new app as
   `vault secrets_vault.age`, and `mmx validate solo_server` prints
   `Valid`.

7. **Run.** Commit and push. Then:

   ```sh
   # On the laptop, in the systems repository.
   mmx dryrun solo_server
   mmx run solo_server
   ```

   The run creates the database and its login, writes the app's settings,
   runs its migrations, starts its containers, adds it to Caddy, the
   tunnel, MMWatchdog, and the outside alarm's checks, and leaves the other
   apps running.

   **Check:** `curl -sS -o /dev/null -w '%{http_code}\n' https://blog.example.org/health`
   prints `200`. The next restore test runs the new app's check queries.
   Mark the new app's checklist items `done` after their checks.

8. **Tell the person** in one line, with the new address, and update the
   README's links.

## Why it works this way

**One database and login for each app.** One app can never read or break
another app's data, and each app moves to the Monkeymaxxing Stack on its
own.
