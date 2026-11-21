# Get your app ready

**When:** right after the systems repository exists on the laptop. This is
part 1, "Your app".

**What this file does:** each app gets ready to run on `solo01` and, later,
on the Monkeymaxxing Stack, with no change: a `Containerfile`, Postgres, R2
for uploads, a health URL, the `PORT` and `WEB_CONCURRENCY` settings, and
separate migrate, web, and jobs commands. GitHub builds the app's first
image. The app's settings go into the systems repository.

**Who acts:** you read the app, plan the changes, and make them. The person
approves the changes to the app before you push them.

**Tell the person when this starts:** nothing new: the map's Now line
already said that you read the app.

## Before you start

- **Ask before changing the app.** Before you change or push anything in
  the app's repository, show what changes and why, in the person's words,
  and wait for yes. One yes covers the whole list you showed.
- **The guides.** The framework guides and the platform migration guides
  are listed in the Monkeymaxxing Solo skill's `SKILL.md`, under "Guides
  for your app". Read the one for the app's framework, and the one for the
  app's current host when it has one, before you plan the changes.
- **Several apps.** Do each step for each app the person named.

## Steps

1. **Check the app against the app contract.** The app contract is the
   seven items under `apps` in `monkeymaxxing_solo_checklist.yml`. Copy the
   template's `shop` items for each app, with the app's name at the start
   of each `id` and in each `title`, and remove the `shop` example. Then
   check the app's code against each item:

   | Item | What the app needs |
   | --- | --- |
   | Data only in Postgres and object storage | Uploads go to an R2 bucket through the S3 API, and sessions live in cookies or Postgres. Nothing writes lasting data to the local disk, a SQLite file, or a volume. `solo01` runs the app with a read-only disk, so a write fails at once. |
   | One `Containerfile` | One image builds from a `Containerfile` at the top of the app's repository. |
   | `PORT` and a health URL | The web server listens on `PORT`, and a path such as `/health` answers HTTP 200 while the app and its database work, with no login. |
   | `WEB_CONCURRENCY` | The web server sizes its worker processes from `WEB_CONCURRENCY`. Puma, Gunicorn, and Uvicorn do this by themselves. |
   | Migrations that keep the running version working | One command runs the migrations. Each migration adds before it removes, and waits at most 5 seconds for a table lock. |
   | Background jobs from one command | One command starts the job worker, which stops within 30 seconds of the stop signal. An app with no jobs has none. |
   | `DATABASE_URL` | The app reads its database from `DATABASE_URL`, a `postgresql://` URL. |

   **Check:** you know, for each item, whether the app meets it now or what
   change it needs.

2. **Plan the changes, and ask.** List each change in one line, in the
   person's words, such as "Add the file that says how your app is
   packaged for the server" or "Store uploaded pictures in Cloudflare's file
   storage, not on the disk". Include the automatic tests at GitHub: copy
   `files_for_your_app/test_build_and_push_the_app_image.yml` from the
   Monkeymaxxing Solo skill into the app's repository as
   `.github/workflows/test_build_and_push_the_app_image.yml`, with its two
   "REPLACE" steps filled for the app's language and tests.

   **Ask the person:**

   ```text
   Your app needs these changes to run on your server:

     1. Add the file that says how your app is packaged for the server.
     2. Add the automatic tests at GitHub, which also package each new version.
     3. Add a health page at /health, which your server checks before each release.
     4. After your site is live, one empty commit to main, which proves that a new
        version goes live with no downtime.  It changes no code.

   I show the full change if you say "show".  Say "yes" and I make them and push them to main.
   ```

   When the app already meets every item, say so in one line, and list only
   the automatic tests at GitHub, when the app has none, and the empty
   commit.

   **Check:** the person said yes.

3. **Make the changes, test them, and push.** Run the app's own tests on
   the laptop when the laptop can, and build the image once locally when
   Podman or Docker is installed, with `--platform linux/amd64`. Then commit
   and push to `main`. Say plainly which tests ran, and which did not and
   why.

   **Check:** the push is on `main` at GitHub.

4. **Wait for the first image.** The workflow at GitHub tests the app,
   builds the image for `linux/amd64`, and pushes it to GitHub's registry
   as `ghcr.io/OWNER/REPOSITORY:COMMIT`, where `COMMIT` is the full
   40-character commit ID. With `gh` signed in, watch it with
   `gh run watch`. Without `gh`, give the person the link to the
   repository's Actions page and wait for "done".

   **Check:** the workflow's last step, "Check that the registry lists the
   tag", passed for the pushed commit.

5. **Write each app into the systems repository.** Replace the template's
   example app `shop` with the person's apps:

   - In `mmx_project_settings.yml`, one block under `vars.apps` for each
     app: `image` (the image without a tag, such as
     `ghcr.io/acme/shop`), `database_name`, two free `web_port_a` and
     `web_port_b` (8001 and 8002 for the first app, 8003 and 8004 for the
     second), `health_url_path`, `asset_directory` (or `""`),
     `migration_command`, `jobs_command` (or `""`),
     `production_uploads_r2_bucket` (a new bucket name such as
     `acme-shop-uploads`, or `""` for an app that keeps no uploads), `env`
     with the app's plain settings, `secrets_from_vault` with each secret
     the app reads, and `restore_drill_check_queries` with at least one
     query that proves the app's data came back.
   - In each `secrets_from_vault` entry, `where_the_value_comes_from` is
     `generated_by_the_setup` for a random value the app only needs to
     keep, such as `SECRET_KEY_BASE`, and for `R2_ACCESS_KEY_ID` and
     `R2_SECRET_ACCESS_KEY` of an app with uploads. It is
     `an_outside_service` for a key from another company, such as Stripe.
   - In `inventories/servers.yml`, each app's tag in `deployed_image_tags`
     (the full 40-character commit from step 4) and its address in
     `public_host_names`.
   - In `playbooks/solo_server.yml`, replace the `shop` lines of
     `database_passwords` and `app_secrets` with one line for each vault key
     each app reads, as
     [Add another app](../operating_procedures/add_another_app.md#write-the-app-into-the-systems-repository)
     shows.
   - `container_registry_read_only_username` in `mmx_project_settings.yml`:
     the GitHub login of the person who makes the read-only key in the next
     part.

   Commit: "Add shop to the systems repository".

   **Check:** `mmx validate solo_server` stops only on a value whose
   comment says "the setup fills it" and that the next part fills, such as
   `cloudflare_account_id`. Any other error is a mistake to fix now.

6. **The end of "Your app".** Show the compact map, as
   [Before you start](00_before_you_start.md#the-compact-map) shows, with
   the result line "Your app is ready: my changes are on main, and GitHub
   packaged your app.", then the first screen of the next part.

## If something is different

- **The app uses SQLite, or keeps uploads on the disk:** this is a bigger
  change. Show the plan first, and say that "Your app" can take days. The
  migration guide for the app's host says how to move the data.
- **The app's code is not at GitHub:** use the Git host's own CI and
  registry, following the six steps at the top of
  `test_build_and_push_the_app_image.yml`. The registry's read-only login
  goes into `container_registry_read_only_username` and the vault the same
  way.
- **The person wants no CI:** build the image on the laptop with Podman or
  Docker, `--platform linux/amd64`, `--build-arg RELEASE=COMMIT`, and the
  tag `IMAGE:COMMIT`, where `COMMIT` is the full commit of a clean
  checkout. The person logs in to the registry in their own terminal, and
  you push the one tag.
- **The app has no health URL:** add one that checks the database with a
  quick query, such as `select 1`.

## Why it works this way

**The same app contract as the Monkeymaxxing Stack.** An app that meets
these seven items moves to the Stack with no change, so outgrowing one
server never means rewriting the app.

**One tag is one image.** The full commit ID as the tag means a tag always
names the same image, so a rollback returns exactly the previous version.

Next: [Create your server and Cloudflare](03_create_your_server_and_cloudflare.md).
