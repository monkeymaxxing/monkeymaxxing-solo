"""`mmx validate` on a filled copy of the single-server systems repository template.

MMX checks every playbook, role, template reference and vault key against
its own rules before it changes a server, so a template that passes here has
no misspelled module, input or reference. The test builds a filled temporary
copy (template_filler.py: values under the reserved .test domain, made-up
keys, a made-up vault) and runs `mmx validate` on all four playbooks:
solo_server, deploy, restore_database and reboot_after_updates. It also checks
that each alert destination kind reads only its own vault keys, and that the
unfilled template is refused.

The mmx program comes from the environment variable MMX_BIN, else from PATH.
Official extra modules (pgbackrest_restore and others) must be beside it, as
they are in an MMX release: <the directory of mmx>/official_extra_modules.
A test run without mmx fails with that message: MMX is the tool this
repository is for, so a run that cannot use it proves nothing.
"""

import json
import os
import pathlib
import subprocess
import tempfile
import unittest

import template_filler

PLAYBOOKS = ["solo_server", "deploy", "restore_database", "reboot_after_updates"]

BASE_KEYS_OF_SOLO_SERVER = {
    "container_registry_read_only_token",
    "cloudflare_tunnel_token",
    "outside_alarm_check_in_token",
    "pgbackrest_r2_access_key_id",
    "pgbackrest_r2_secret_access_key",
    "pgbackrest_repository_cipher_passphrase",
    "shop_database_password",
    "shop_SECRET_KEY_BASE",
    "shop_R2_ACCESS_KEY_ID",
    "shop_R2_SECRET_ACCESS_KEY",
}

# For each alert destination kind: the edits that turn it on in
# mmx_project_settings.yml, and the vault keys that only it reads.
ALERT_KIND_EDITS = {
    "pushover": [],
    "slack": [("kind: pushover", "kind: slack")],
    "discord": [("kind: pushover", "kind: discord")],
    "telegram": [("kind: pushover", "kind: telegram"), ('telegram_chat_id: ""', 'telegram_chat_id: "-1001234567890"')],
    "webhook": [("kind: pushover", "kind: webhook"), ('webhook_url: ""', 'webhook_url: "https://hooks.acme.test/alerts"'), ("webhook_has_a_bearer_token: false", "webhook_has_a_bearer_token: true")],
    "email": [
        ("kind: pushover", "kind: email"),
        ('email_smtp_server: ""', 'email_smtp_server: "smtp.acme.test:587"'),
        ('email_from: ""', 'email_from: "alerts@acme.test"'),
        ("email_to: []", 'email_to: ["person@acme.test"]'),
        ('email_login_user: ""', 'email_login_user: "alerts"'),
    ],
}


class TemplateValidatesWithMmx(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.mmx = template_filler.find_mmx()
        if not cls.mmx:
            raise AssertionError(
                "mmx was not found. Set MMX_BIN to the mmx program, or put mmx on PATH. "
                "Build it from the MMX repository with: go build -o /tmp/mmx-solo-test ./cmd/mmx "
                "(then MMX_BIN=/tmp/mmx-solo-test python3 -m unittest discover -s tests -p '*_test.py')."
            )
        release_directory = os.environ.get("MMX_RUNNER_DIR") or os.path.dirname(os.path.realpath(cls.mmx))
        if not os.path.isdir(os.path.join(release_directory, "official_extra_modules")):
            raise AssertionError(
                "The official extra modules are not beside mmx: " + release_directory + "/official_extra_modules is missing. "
                "An MMX release archive has them there; for a program you built, copy the MMX repository's "
                "official_extra_modules directory next to it, or set MMX_RUNNER_DIR to a directory that holds both."
            )
        cls.directory = tempfile.TemporaryDirectory(prefix="solo-validate-test-")
        cls.addClassCleanup(cls.directory.cleanup)
        cls.environment = dict(os.environ, MMX_VAULT_PASSWORD=template_filler.TEST_VAULT_PASSPHRASE)

    def run_mmx(self, project, *arguments):
        return subprocess.run([self.mmx, *arguments], cwd=project, env=self.environment, capture_output=True, text=True)

    def filled_copy(self, name, settings_edits=(), extra_vault_keys=()):
        project = template_filler.fill_the_template(pathlib.Path(self.directory.name) / name / "systems")
        if settings_edits:
            settings_file = project / "mmx_project_settings.yml"
            settings = settings_file.read_text(encoding="utf-8")
            for old, new in settings_edits:
                settings = template_filler.replace_once(settings, old, new)
            settings_file.write_text(settings, encoding="utf-8")
        template_filler.make_the_test_vault(project, self.mmx, list(template_filler.TEST_VAULT_KEYS) + list(extra_vault_keys))
        return project

    def test_every_playbook_validates(self):
        project = self.filled_copy("all_playbooks")
        for playbook in PLAYBOOKS:
            with self.subTest(playbook=playbook):
                finished = self.run_mmx(project, "validate", playbook)
                self.assertEqual(finished.returncode, 0, finished.stdout + finished.stderr)
                self.assertIn("Valid", finished.stdout)

    def test_the_unfilled_template_is_refused_and_names_a_value_the_setup_fills(self):
        project = pathlib.Path(self.directory.name) / "unfilled" / "systems"
        template_filler.shutil.copytree(template_filler.TEMPLATE_DIRECTORY, project)
        finished = self.run_mmx(project, "validate", "solo_server")
        self.assertNotEqual(finished.returncode, 0, "an unfilled template must not validate")
        self.assertIn("project_name", finished.stdout + finished.stderr)

    def secrets_usage(self, project):
        finished = self.run_mmx(project, "secrets", "usage", "--json", "--non-interactive")
        self.assertEqual(finished.returncode, 0, finished.stdout + finished.stderr)
        return json.loads(finished.stdout)["keys"]

    def test_alerts_off_reads_no_alert_key(self):
        project = self.filled_copy("alerts_off", extra_vault_keys=[key for keys in template_filler.ALERT_VAULT_KEYS.values() for key in keys])
        finished = self.run_mmx(project, "validate", "solo_server")
        self.assertEqual(finished.returncode, 0, finished.stdout + finished.stderr)
        needed = {entry["key"] for entry in self.secrets_usage(project) if not entry.get("only_read_by_fleet_stages_left_out_before_the_run")}
        self.assertEqual(needed, BASE_KEYS_OF_SOLO_SERVER | {"container_registry_read_only_token"})

    def test_each_alert_destination_kind_reads_only_its_own_keys(self):
        every_alert_key = [key for keys in template_filler.ALERT_VAULT_KEYS.values() for key in keys]
        for kind, edits in ALERT_KIND_EDITS.items():
            with self.subTest(kind=kind):
                project = self.filled_copy("alerts_" + kind, [("    enabled: false", "    enabled: true")] + edits, every_alert_key)
                for playbook in ("solo_server", "deploy"):
                    finished = self.run_mmx(project, "validate", playbook)
                    self.assertEqual(finished.returncode, 0, playbook + ": " + finished.stdout + finished.stderr)
                needed = {entry["key"] for entry in self.secrets_usage(project) if not entry.get("only_read_by_fleet_stages_left_out_before_the_run")}
                self.assertEqual(needed, BASE_KEYS_OF_SOLO_SERVER | set(template_filler.ALERT_VAULT_KEYS[kind]), kind + " reads the wrong vault keys")

    def test_every_secret_the_playbooks_read_is_in_the_documented_vault_keys(self):
        project = self.filled_copy("documented_keys", [("    enabled: false", "    enabled: true")] + ALERT_KIND_EDITS["pushover"], template_filler.ALERT_VAULT_KEYS["pushover"])
        for entry in self.secrets_usage(project):
            if entry.get("only_read_by_fleet_stages_left_out_before_the_run"):
                continue
            self.assertIsNotNone(entry["source"], entry["key"] + " is read by a playbook but is not in the test vault")


if __name__ == "__main__":
    unittest.main()
