# Reboot after updates

**When:** MMWatchdog's security update check, or the weekly system review,
says that `solo01` waits for a reboot, such as after a new Linux kernel.

**What this does:** `playbooks/reboot_after_updates.yml` reboots `solo01`
inside the weekly maintenance window, pauses MMWatchdog's alerts for the
reboot, and waits until Postgres, Caddy, the tunnel, each app, and
MMWatchdog work again. `solo01` never reboots by itself. The site is down
for under a minute, because `solo01` is the only server.

**Who acts:** you. The person approves a reboot outside the window.

## Steps

1. **Check whether a reboot waits.**
   `ssh -F ssh/config root@10.20.1.1 'test -f /var/run/reboot-required && cat /var/run/reboot-required.pkgs'`
   lists the packages that asked for it.

2. **Run it inside the window.** `maintenance_window` in
   `mmx_project_settings.yml` is in UTC. Tell the person the time in their
   local time, such as "Tuesday at 11 pm your time".

   ```sh
   # On the laptop, in the systems repository, inside the window.
   mmx run reboot_after_updates
   ```

   Outside the window the run changes nothing and says when the window
   opens. With no reboot waiting, it ends at once.

   **Check:** the run ends with no failed node, and
   `/var/run/reboot-required` is gone. Each app's public health URL
   answers `200`. Record `planned_reboots_run_inside_the_maintenance_window`
   in the checklist.

3. **Tell the person** in one line: "Your server rebooted for the kernel
   update at 21:02 UTC and was back in 48 seconds."

## A reboot that cannot wait

When an update fixes a flaw that attackers exploit in the running kernel,
ask the person:

> A kernel flaw that attackers use is fixed by an update that needs a reboot. Your site is down for about a minute. Reboot now, outside your maintenance window?

With yes: `mmx run reboot_after_updates --var reboot_outside_the_maintenance_window=true`.

## If something is different

- **`solo01` does not come back within `come_back_wait_after_a_reboot`:**
  the run fails, and MMWatchdog's alert pause ends by itself. Check with
  `~/.local/bin/doubleshell --wireguard-config ~/.config/doubleshell/solo01.conf list-machines`,
  then the company's web console with the person.
- **An app does not answer after the reboot:** the run fails and names the
  app. Read its log with `journalctl -u 'APP-web-*' -n 100` on `solo01`.

## Why it works this way

**A chosen time.** One server means every reboot is a short outage, so it
happens when the person's users are away, and never by surprise.
