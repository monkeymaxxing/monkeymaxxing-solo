"""Facts about the single-server systems repository template that must always hold.

These tests read the template's files as text (no mmx, no network) and keep
Solo what its design says: one server named solo01, pinned programs with real
checksums, no parts of the bigger Monkeymaxxing Stack, no placeholder
addresses in live settings, every file a playbook or role names present, and
nothing from a private lab in a public repository.
"""

import os
import pathlib
import re
import subprocess
import sys
import unittest

# Importing a program of the template must not write bytecode next to it.
sys.dont_write_bytecode = True

import template_filler

TEMPLATE = template_filler.TEMPLATE_DIRECTORY
REPOSITORY = template_filler.REPOSITORY_ROOT
ZERO_SHA256 = "0" * 64

# Parts of the Monkeymaxxing Stack that Solo leaves out. None may appear in a
# live (non-comment) line of the template: a leftover would be a setting that
# nothing reads, or a server that does not exist.
LEFT_OUT = [
    "pg_auto_failover", "pgautofailover", "pg_autoctl", "pgbouncer", "mmhotline", "systems_desk", "desk01", "util01",
    "staging01", "restore01", "backblaze", "b2_s3", "b2_region", "jobs_server", "load_balancer", "app_servers",
    "database_servers", "triage", "monkeymaxxing-stack", "stack_software", "lock_everyone_out", "everyone_locked_out",
]

# Words that belong to a private lab, not to a public product. Written in
# pieces so this file does not contain them itself.
PRIVATE_LAB_WORDS = ["/opt/" + "lab", "lab" + "_setup", "hil" + "o", "/Users/" + "dev", "agent-" + "notify", "ma" + "ui/"]


def template_files():
    return sorted(path for path in TEMPLATE.rglob("*") if path.is_file() and "__pycache__" not in path.parts)


def read(path):
    return pathlib.Path(path).read_text(encoding="utf-8")


def live_lines(path):
    """The lines of a file that are not comments or blank: what a server or MMX would act on."""
    lines = []
    in_docstring = False
    for line in read(path).splitlines():
        stripped = line.strip()
        if stripped.count('"""') == 1:
            in_docstring = not in_docstring
            continue
        if in_docstring or stripped == "" or stripped.startswith("#") or stripped.startswith("{{- /*") or stripped.startswith("*/"):
            continue
        if str(path).endswith((".yml", ".json")):
            line = re.sub(r"\s+#.*$", "", line)
        lines.append(line)
    return lines


def read_block(text, name):
    """The nested mapping under `name:` of a YAML file, for simple `key: value` blocks, as strings."""
    lines = text.splitlines()
    start = next(index for index, line in enumerate(lines) if re.match(r"^\s*" + re.escape(name) + r":\s*(#.*)?$", line))
    indent = len(lines[start]) - len(lines[start].lstrip())
    root, stack = {}, [(indent, {})]
    stack = [(indent, root)]
    for line in lines[start + 1:]:
        if line.strip() == "" or line.strip().startswith("#"):
            continue
        current = len(line) - len(line.lstrip())
        if current <= indent:
            break
        while stack and current <= stack[-1][0]:
            stack.pop()
        match = re.match(r"^\s*([A-Za-z0-9_-]+):\s*(.*?)\s*(#.*)?$", line)
        if not match:
            continue
        key, value = match.group(1), match.group(2)
        parent = stack[-1][1]
        if value == "":
            parent[key] = {}
            stack.append((current, parent[key]))
        else:
            parent[key] = value.strip('"')
    return root


class SoftwarePinsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.settings = read(TEMPLATE / "mmx_project_settings.yml")
        cls.software = read_block(cls.settings, "solo_software")

    def test_the_programs_are_the_ones_the_stack_v0_3_0_was_tested_with(self):
        self.assertEqual(sorted(self.software), ["doubleshell", "mmwatchdog", "mmx"])
        self.assertEqual({name: pin["version"] for name, pin in self.software.items()}, {"mmx": "0.2.0", "doubleshell": "0.1.2", "mmwatchdog": "0.1.3"})

    def test_every_pin_has_a_real_checksum_and_an_address_that_names_its_version(self):
        for name, pin in self.software.items():
            with self.subTest(program=name):
                self.assertRegex(pin["sha256"], r"^[0-9a-f]{64}$")
                self.assertNotEqual(pin["sha256"], ZERO_SHA256)
                self.assertTrue(pin["download_url"].startswith("https://github.com/monkeymaxxing/" + name + "/releases/download/v" + pin["version"] + "/"), pin["download_url"])
                self.assertTrue(pin["download_url"].endswith("-" + pin["version"] + "-linux-amd64.tar.gz") or pin["download_url"].endswith(name + "-" + pin["version"] + "-linux-amd64.tar.gz"), pin["download_url"])

    def test_doubleshell_has_a_checksum_for_each_laptop_platform(self):
        platforms = self.software["doubleshell"]["laptop_archive_sha256"]
        self.assertEqual(sorted(platforms), ["linux-amd64", "linux-arm64", "macos-amd64", "macos-arm64"])
        for platform, checksum in platforms.items():
            with self.subTest(platform=platform):
                self.assertRegex(checksum, r"^[0-9a-f]{64}$")
                self.assertNotEqual(checksum, ZERO_SHA256)
        self.assertEqual(platforms["linux-amd64"], self.software["doubleshell"]["sha256"])
        self.assertEqual(len(set(platforms.values())), 4, "each platform's archive has its own checksum")

    def test_solo_server_refuses_a_pin_of_zeros_before_it_changes_anything(self):
        playbook = read(TEMPLATE / "playbooks" / "solo_server.yml")
        self.assertIn("no_pin_has_a_sha256_of_zeros", playbook)
        self.assertIn("refuse_values_the_setup_has_not_filled_in", playbook)
        self.assertIn("0" * 64, playbook)
        # The two refusals are the first fleet stages.
        stages = re.findall(r"(?m)^  - id: (\w+)$", playbook)
        self.assertEqual(stages[:2], ["refuse_programs_pinned_without_a_release", "refuse_values_the_setup_has_not_filled_in"])


class SettingsAndInventoryTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.settings = read(TEMPLATE / "mmx_project_settings.yml")
        cls.inventory = read(TEMPLATE / "inventories" / "servers.yml")

    def test_project_level_settings(self):
        self.assertRegex(self.settings, r"(?m)^project_name:\s+#")
        for line in ("default_inventory: servers", "vault_file: secrets_vault.age", "run_history_retention: 30d"):
            self.assertRegex(self.settings, r"(?m)^" + re.escape(line) + r"\b")

    def test_defaults_of_the_schedules_and_the_database(self):
        for line in (
            'nightly_work_starts_at: "01:00"', "weekly_work_day: sunday", "come_back_wait_after_a_reboot: 3m", "postgresql_major_version: 17",
            "keep_database_backups_for_days: 21", "lock_backups_against_deletion_for_days: 14", "delay_seconds: 300",
            "health_check_failures_before_alert: 2", "pushover_sound: cosmic", "pushover_priority: 1", "public_site_checks_run: false",
            "production_releases_wait_for_a_persons_approval: true", "first_release_waits_for_a_persons_approval: false",
        ):
            self.assertRegex(self.settings, r"(?m)^\s+" + re.escape(line) + r"\s*(#.*)?$", line)
        self.assertEqual(read_block(self.settings, "maintenance_window"), {"weekday": "tuesday", "starts_at": "21:00", "length_minutes": "60"})
        self.assertEqual(read_block(self.settings, "alert_destination")["kind"], "pushover")
        self.assertEqual(read_block(self.settings, "alert_destination")["enabled"], "false")

    def test_values_only_the_setup_knows_ship_empty(self):
        for key in ("cloudflare_account_id", "container_registry_read_only_username", "r2_s3_endpoint", "bucket_name", "worker_url"):
            self.assertRegex(self.settings, r"(?m)^\s+" + key + r":\s+#\s*the setup fills it", key)
        self.assertRegex(self.settings, r"(?m)^\s+root_ssh_public_keys: \[\]")
        for key in ("public_ipv4_address", "wireguard_public_key", "laptop_wireguard_public_key"):
            self.assertRegex(self.inventory, r"(?m)^\s+" + key + r":\s+#\s*the setup fills it", key)

    def test_the_example_app_has_the_stacks_app_contract(self):
        app = read_block(self.settings, "shop")
        for key in ("image", "database_name", "web_port_a", "web_port_b", "health_url_path", "asset_directory", "migration_command", "jobs_command", "production_uploads_r2_bucket", "env", "secrets_from_vault", "restore_drill_check_queries"):
            self.assertIn(key, app)

    def test_one_server_named_solo01_in_one_group(self):
        self.assertEqual(re.findall(r"(?m)^  (\w+):$", self.inventory.split("nodes:")[1].split("groups:")[0]), ["solo01"])
        self.assertRegex(self.inventory, r"(?m)^  solo_server:\n    nodes: \[solo01\]")
        for line in ("address: 10.20.1.1", "connection: ssh_only", "ssh_user: root", "ssh_known_hosts: ssh/known_hosts", "ssh_config: ssh/config", "private_ip: 10.20.1.1", "laptop_private_ip: 10.20.10.1", "wireguard_network: 10.20.0.0/16", "wireguard_listen_port: 51820"):
            self.assertIn(line, self.inventory)

    def test_image_tags_and_host_names_are_flat_with_no_staging(self):
        tags = read_block(self.inventory, "deployed_image_tags")
        hosts = read_block(self.inventory, "public_host_names")
        self.assertEqual(sorted(tags), ["shop"])
        self.assertEqual(sorted(hosts), ["shop"])
        self.assertRegex(tags["shop"], r"^[0-9a-f]{40}$")

    def test_ssh_goes_through_doubleshell_to_the_wireguard_address(self):
        config = read(TEMPLATE / "ssh" / "config")
        self.assertRegex(config, r"(?m)^\s+ProxyCommand .*doubleshell --wireguard-config \S*solo01\.conf proxy-ssh %h %p$")
        self.assertIn("Host solo01 10.20.1.1", config)
        self.assertIn("StrictHostKeyChecking yes", config)
        self.assertIn("UserKnownHostsFile ssh/known_hosts", config)


class WhatTheTemplateHoldsTest(unittest.TestCase):
    def test_exactly_the_four_playbooks_and_four_roles(self):
        self.assertEqual(sorted(path.name for path in (TEMPLATE / "playbooks").iterdir()), ["deploy.yml", "reboot_after_updates.yml", "restore_database.yml", "solo_server.yml"])
        self.assertEqual(sorted(path.name for path in (TEMPLATE / "roles").iterdir()), ["apps_and_tunnel", "base_server", "monitoring_and_outside_alarm", "postgres_and_backups"])
        for role in (TEMPLATE / "roles").iterdir():
            self.assertTrue((role / "main.yml").is_file(), str(role))
        self.assertEqual(sorted(path.name for path in (TEMPLATE / "mmwatchdog_checks").iterdir()), ["apps_and_tunnel.yml", "every_server.yml", "postgres_and_backups.yml"])

    def test_no_part_of_the_stack_that_solo_leaves_out_is_in_a_live_line(self):
        for path in template_files():
            if path.name == "monkeymaxxing_solo_checklist.yml" or path.suffix == ".asc" or path.suffix == ".gpg":
                continue
            for line in live_lines(path):
                for word in LEFT_OUT:
                    with self.subTest(file=str(path.relative_to(TEMPLATE)), word=word):
                        self.assertNotIn(word, line.lower(), str(path.relative_to(TEMPLATE)) + ": " + line.strip())

    def test_the_checklist_mentions_what_solo_leaves_out_only_in_its_informational_section(self):
        text = read(TEMPLATE / "monkeymaxxing_solo_checklist.yml")
        before_the_section, section = text.split("what_solo_leaves_out:")
        self.assertIn("What Solo leaves out (when to upgrade to the Monkeymaxxing Stack)", section)
        self.assertRegex(text, r'(?m)^solo_version: "0\.1\.0"$')
        self.assertRegex(text, r'(?m)^solo_commit: ""$')
        for word in ("pg_auto_failover", "pgbouncer", "systems_desk", "desk01", "util01", "staging01", "backblaze", "b2_s3"):
            live = "\n".join(line for line in before_the_section.splitlines() if not line.strip().startswith("#"))
            self.assertNotIn(word, live.lower(), word)

    def test_no_placeholder_address_is_left_in_a_live_file(self):
        # The settings and the inventory ship placeholder values under example.com; the filled copy replaces each.
        for path in template_files():
            relative = str(path.relative_to(TEMPLATE))
            if relative in ("mmx_project_settings.yml", "inventories/servers.yml") or path.suffix in (".asc", ".gpg"):
                continue
            for line in live_lines(path):
                self.assertNotIn("example.com", line, relative + ": " + line.strip())
        with template_filler.FilledTemplate() as filled:
            for relative in ("mmx_project_settings.yml", "inventories/servers.yml"):
                for line in live_lines(filled / relative):
                    self.assertNotIn("example.com", line, relative + ": " + line.strip())

    def test_the_firewall_admits_only_the_two_wireguard_ports_and_ssh_on_wireguard(self):
        role = read(TEMPLATE / "roles" / "base_server" / "main.yml")
        block = role.split("incoming_rules:")[1].split("incoming_rules_not_listed: remove")[0]
        rules = [line.strip() for line in block.splitlines() if line.strip().startswith("- {action:")]
        self.assertEqual(rules, [
            "- {action: allow, protocol: udp, port: 443}",
            '- {action: allow, protocol: udp, port: "{{ .inputs.wireguard_listen_port }}"}',
            '- {action: allow, protocol: tcp, port: 22, incoming_interface: wg0, from: "{{ .inputs.laptop_private_ip }}"}',
        ])
        self.assertIn("default_incoming: deny", role)
        self.assertIn("incoming_rules_not_listed: remove", role)

    def test_sshd_listens_only_on_the_wireguard_address_with_key_only_root_login(self):
        role = read(TEMPLATE / "roles" / "base_server" / "main.yml")
        for line in ("ListenAddress {{ .inputs.private_ip }}", "PermitRootLogin prohibit-password", "PasswordAuthentication no", "KbdInteractiveAuthentication no"):
            self.assertIn(line, role)
        self.assertEqual(role.count("ListenAddress"), 1)

    def test_nothing_runs_a_web_port_open_to_the_internet(self):
        role = read(TEMPLATE / "roles" / "apps_and_tunnel" / "files" / "Caddyfile")
        self.assertIn("default_bind 127.0.0.1", role)
        self.assertIn("auto_https off", role)
        incoming = read(TEMPLATE / "roles" / "base_server" / "main.yml").split("incoming_rules:")[1].split("incoming_rules_not_listed: remove")[0]
        self.assertIsNone(re.search(r"protocol: tcp, port: (80|443)\b|port: (80|443), protocol: tcp", incoming), "a web port is open")

    def test_every_script_is_executable_and_has_valid_syntax(self):
        scripts = [
            path for path in template_files()
            if path.suffix == "" and path.parent.name == "files" and read(path).startswith("#!")
        ] + [TEMPLATE / "roles" / "monitoring_and_outside_alarm" / "files" / "send_outside_alarm_check_in.py"]
        self.assertGreaterEqual(len(scripts), 7)
        for path in scripts:
            with self.subTest(script=path.name):
                self.assertTrue(os.access(path, os.X_OK), "not executable: " + str(path))
                first_line = read(path).splitlines()[0]
                if "python" in first_line:
                    compile(read(path), str(path), "exec")
                else:
                    finished = subprocess.run(["bash", "-n", str(path)], capture_output=True, text=True)
                    self.assertEqual(finished.returncode, 0, finished.stderr)

    def test_the_checklist_has_unique_ids_and_the_fields_each_item_needs(self):
        text = read(TEMPLATE / "monkeymaxxing_solo_checklist.yml")
        ids = re.findall(r"(?m)^\s+- id: (\w+)$", text)
        self.assertEqual(len(ids), len(set(ids)), "an id repeats")
        self.assertGreaterEqual(len(ids), 25)
        items = text.split("\n      - id: ") if False else re.split(r"(?m)^\s+- id: ", text)[1:]
        for item in items:
            name = item.splitlines()[0]
            if name in ("more_than_one_server", "automatic_database_failover", "staging_server", "separate_systems_desk", "separate_backup_company", "on_call_alerting_and_the_triage_agent", "provider_server_scripts"):
                continue
            for field in ("title:", "state:", "part:", "protects_against:", "verification:"):
                self.assertIn(field, item, name + " has no " + field)
            self.assertRegex(item, r"part: (setup|later_when_it_happens)")


class ReferencesTest(unittest.TestCase):
    def test_every_role_a_playbook_calls_exists(self):
        for playbook in (TEMPLATE / "playbooks").iterdir():
            for role in re.findall(r"(?m)^\s+- role: (\w+)$", read(playbook)):
                with self.subTest(playbook=playbook.name, role=role):
                    self.assertTrue((TEMPLATE / "roles" / role / "main.yml").is_file())

    def test_every_file_a_playbook_or_role_names_exists(self):
        for path in list((TEMPLATE / "playbooks").iterdir()) + [role / "main.yml" for role in (TEMPLATE / "roles").iterdir()]:
            base = path.parent if path.parent.name != "playbooks" else TEMPLATE
            for kind, name in re.findall(r"(?m)^\s+(source|template): (\S+)$", read(path)):
                with self.subTest(file=str(path.relative_to(TEMPLATE)), name=name):
                    candidates = [(base / name), (TEMPLATE / name)]
                    self.assertTrue(any(candidate.resolve().is_file() for candidate in candidates), kind + " " + name + " does not exist")
            for name in re.findall(r"signing_key_source: (\S+)", read(path)):
                self.assertTrue((base / name).is_file(), name)

    def test_no_file_of_a_role_is_left_unused(self):
        everything = "\n".join(read(path) for path in template_files() if path.suffix in (".yml", ".json", "") or path.name in ("Caddyfile",))
        for role in (TEMPLATE / "roles").iterdir():
            for file in (role / "files").iterdir():
                with self.subTest(file=str(file.relative_to(TEMPLATE))):
                    self.assertIn(file.name, everything.replace(read(file), "") if file.suffix not in (".asc", ".gpg") else everything)
        for file in (TEMPLATE / "files_used_by_several_roles").iterdir():
            with self.subTest(file=file.name):
                self.assertIn("files_used_by_several_roles/" + file.name, everything)

    def test_every_playbook_and_check_group_the_template_names_exists(self):
        text = "\n".join(read(path) for path in template_files() if path.suffix in (".yml", ".py", ""))
        for name in set(re.findall(r"mmx (?:run|dryrun|validate) ([a-z_]+)\b", text)):
            with self.subTest(playbook=name):
                self.assertTrue((TEMPLATE / "playbooks" / (name + ".yml")).is_file(), name)
        for name in set(re.findall(r"mmwatchdog_checks/([a-z_]+\.yml)", text)):
            with self.subTest(check_group_file=name):
                self.assertTrue((TEMPLATE / "mmwatchdog_checks" / name).is_file(), name)

    def test_every_vault_key_the_playbooks_read_is_one_the_filler_knows(self):
        known = set(template_filler.TEST_VAULT_KEYS) | {key for keys in template_filler.ALERT_VAULT_KEYS.values() for key in keys}
        used = set()
        for playbook in (TEMPLATE / "playbooks").iterdir():
            used |= set(re.findall(r"\.secrets\.([A-Za-z0-9_]+)", read(playbook)))
        self.assertEqual(used, known)


class PublicRepositoryTest(unittest.TestCase):
    def test_nothing_from_a_private_lab_is_in_the_skill_or_the_tests(self):
        for directory in (REPOSITORY / "skills", REPOSITORY / "tests"):
            for path in sorted(directory.rglob("*")):
                if not path.is_file() or path.suffix in (".pyc", ".asc", ".gpg") or "__pycache__" in path.parts:
                    continue
                text = read(path)
                for word in PRIVATE_LAB_WORDS:
                    self.assertNotIn(word, text, str(path.relative_to(REPOSITORY)) + " contains " + repr(word))

    def test_the_tests_run_without_any_test_switch_in_the_product(self):
        # The product talks to the real service hostnames: its addresses are fixed in the program.
        script = read(TEMPLATE / "roles" / "monitoring_and_outside_alarm" / "files" / "send_outside_alarm_check_in.py")
        self.assertIn("https://api.pushover.net/1/messages.json", script)
        self.assertIn("https://api.telegram.org/bot", script)
        for switch in ("--test-endpoint", "--insecure", "--ca-file", "verify=False", "ssl._create_unverified_context", "CERT_NONE"):
            self.assertNotIn(switch, script)


if __name__ == "__main__":
    unittest.main()
