# Assess a running system

**When:** you meet this system for the first time, such as in a new
session or on a new laptop, or the person asks what to change.

**What this does:** you learn what runs on `solo01` from the repository,
check that the server matches it, and recommend at most three changes, each
with its reason.

**Who acts:** you. The person decides each recommendation.

## Steps

1. **Read the repository.** `AGENTS.md`, `README.md`,
   `mmx_project_settings.yml`, `inventories/servers.yml`, and
   `monkeymaxxing_solo_checklist.yml`. Note the apps, their addresses,
   `solo_version`, `solo_commit`, and every item that is `open`,
   `deviated`, or due again.

2. **Check the laptop's tools and access.** The pinned `mmx`, `doubleshell`,
   and `set-up-solo-services` are in `~/.local/bin`, the vault passphrase
   file exists, and `ssh -F ssh/config root@10.20.1.1 hostname` answers.
   When access fails, say so and stop: never work around it on the server.

3. **Check that the server matches the repository.** `git status` is clean
   and `main` matches GitHub. `mmx validate solo_server` prints `Valid`.
   `mmx dryrun solo_server` shows no change. A change in the dryrun means
   someone changed `solo01` by hand or a commit never ran: tell the person
   before you run anything.

4. **Check the health.** Follow steps 1 to 3 of
   [Do the weekly system review](do_the_weekly_system_review.md).

5. **Recommend.** At most three changes, most important first, each in one
   or two lines with its reason, such as an `open` checklist item, a new
   Solo version, a reason to move to the Monkeymaxxing Stack from
   `what_solo_leaves_out`, or a disk near full. When nothing needs a
   change, say so.

   **Check:** the person chose what to do. Follow the operating procedure
   for each choice, from the table in [`AGENTS.md`](../AGENTS.md#when-this-happens-follow-this-operating-procedure).

## Why it works this way

**Read before acting.** A dryrun that shows changes on a system nobody
changed is the first sign of drift, and running it blind would undo
someone's work.
