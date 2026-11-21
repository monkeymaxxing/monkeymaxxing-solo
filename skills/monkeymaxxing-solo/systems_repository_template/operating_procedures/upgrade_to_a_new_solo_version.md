# Upgrade to a new Solo version

**When:** a new Solo release is out, or the template in the installed skill
changed since `solo_commit` in `monkeymaxxing_solo_checklist.yml`, and the
person asks to upgrade.

**What this does:** you compare the new version's template with the one
this system was copied from, carry each change into this repository while
keeping the person's own values, and apply it with one `solo_server` run.
A new skill version never changes a running system by itself.

**Who acts:** you. The person approves the plan, and any step the release
notes name as a person's step.

## Steps

1. **Read the release notes** of every version between `solo_version` and
   the new one, at Solo's repository on GitHub. Note each step the notes
   name, such as a new setting, a new vault key, or a new Debian release.

2. **Install the new skill version** as Solo's README says, and the new
   `set-up-solo-services`, as the initial setup's
   [Answer two questions](../initial_setup/01_answer_two_questions.md),
   step 4, does.

3. **Compare the templates.** With a clone of Solo's repository, compare
   the old commit with the new tag:
   `git diff SOLO_COMMIT NEW_TAG -- skills/monkeymaxxing-solo/systems_repository_template`.
   For each changed file, carry the change into this repository. Keep the
   person's own values: every line the setup filled, each app, the
   schedules, and the alert settings.

   Also keep everything the person added from a recipe. The template never
   holds these, so a diff never asks you to remove them:

   - the `custom_recipes/` directory;
   - `custom_extensions:` in `monkeymaxxing_solo_checklist.yml`;
   - the files `mmwatchdog_checks/custom_*.yml`, and their names in
     `mmwatchdog_check_files` in `mmx_project_settings.yml` (carry a new
     core name into that list, and keep every `custom_*.yml` name);
   - `custom:` in `mmx_project_settings.yml`;
   - every custom fleet stage and role call between the
     `# --- Custom extensions (start) ---` and `# --- Custom extensions (end) ---`
     lines of `playbooks/solo_server.yml`, and any role of the recipe in
     `roles/`.

   When a template change touches lines beside one of these markers, carry
   it by hand and keep the marker lines and the lines between them exactly
   as they are.

4. **Install new pinned programs on the laptop.** When `solo_software`
   changed, install the new `mmx` and `doubleshell` releases as the
   initial setup's step 4 does.

5. **Show the plan, and ask.** List each change in one line in the person's
   words, with any downtime and any step for the person:

   > Solo 0.2.0 has 3 changes for your system: a newer MMWatchdog, a check for a full disk earlier, and a new setting for the restore test's time. Nothing goes down. Upgrade now?

   **Check:** the person said yes.

6. **Apply.** `mmx validate solo_server`, then write the new `solo_version`
   and `solo_commit` into the checklist. Commit ("Upgrade to Solo 0.2.0")
   and push. Run `mmx dryrun solo_server` and `mmx run solo_server`.

   **Check:** the run ends with no failed node, each app's public health
   URL answers `200`, and `mmwatchdog status` on `solo01` shows no failed
   check.

7. **Tell the person** in one line what changed, and update the README.

## A new Debian release

A Solo version that moves to a new Debian release also moves Postgres to a
new major version. Its release notes give the steps, which build a new
`solo01` with [Rebuild solo01 on a new server](rebuild_solo01_on_a_new_server.md)
and move the data the way the notes say. Never upgrade Debian in place on
`solo01`.

## Why it works this way

**A copy, not a link.** Each system keeps its own copy of the template, so
a new skill version changes nothing until you and the person apply it.
