# Do the weekly system review

**When:** once a week, on a day the person chose, or when the person says
"Do the weekly system review" or asks how the system is.

**What this does:** you and the person look at the week in about 10
minutes: the alerts, the waiting updates and reboots, the restore test's
result, the backups, the disk and memory, the checklist items that are due
again, and whether Solo still fits. You fix what you can, and the person
decides the rest.

**Who acts:** you check everything. The person reads one short report and
answers its questions.

## Steps

1. **Read the server's state.** Read only: change nothing yet.

   ```sh
   # On the laptop, in the systems repository.
   ssh -F ssh/config root@10.20.1.1 'mmwatchdog status; mmwatchdog events --last 168h; cat /var/lib/solo-restore-check/last-result.json; sudo -u postgres pgbackrest --stanza=main info; df -h / /var/lib; free -m; test -f /var/run/reboot-required && cat /var/run/reboot-required.pkgs; apt list --upgradable 2>/dev/null | tail -n +2'
   ```

   Read `mmwatchdog --help` for the installed release first, when a
   command above differs.

   **Check:** you know the week's alerts, the restore test's result and
   date, the newest full backup, the free disk and memory, whether a reboot
   waits, and what updates wait.

2. **Check the outside.** Each app's public health URL answers `200`. The
   outside alarm got a check-in in the last minutes:
   `/usr/local/sbin/send_outside_alarm_check_in.py` ran without error in
   `journalctl -u solo_outside_alarm_check_in.service -n 5` on `solo01`.

3. **Check what is due.** In `monkeymaxxing_solo_checklist.yml`, find each
   item whose `verified_at` is older than its `verify_again_every`, and
   each `open` item. Check each due item again, and record the result.

4. **Check whether Solo still fits.** Compare the week with
   `what_solo_leaves_out` in the checklist: memory near the limit at
   releases, an outage that a rebuild made too long, or a second person
   who manages the server. Name a reason to move to the Monkeymaxxing Stack
   in one line, only when one applies.

5. **Check for a new Solo version.** Compare `solo_version` and
   `solo_commit` with Solo's newest release. When one is out, say so in one
   line.

6. **Report, in at most 15 lines,** in the person's words:

   > Your system this week: no alerts, the restore test passed on Sunday, and the newest full backup is from Sunday 01:00 UTC.
   > A kernel update waits for a reboot. I reboot it in your window, Tuesday at 11 pm your time, unless you say otherwise.
   > Disk: 31% used. Memory at releases: enough room.
   > Nothing for you to do.

   Ask only what the person must decide. Then do what was agreed, such as
   [Reboot after updates](reboot_after_updates.md) in the window, and
   update the README's next actions.

## Why it works this way

**One short duty.** Automatic checks catch most faults at once. The weekly
review catches the slow ones: a disk that fills over months, an update that
waits, and a system that outgrows one server.
