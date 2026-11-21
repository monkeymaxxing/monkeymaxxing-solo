"""Shared helper of the template tests: a filled, temporary copy of the systems repository template.

The template ships every value that only the setup can know (a name, an
address, a key) empty, so a copy of the template never looks real. The tests
need a copy whose values are filled in, to run `mmx validate` on it and to
render its templates. This module makes that copy in a temporary directory,
with test values only: names under the reserved `.test` domain, addresses
from the documentation range 203.0.113.0/24, and keys that belong to nobody.
"""

import os
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile

REPOSITORY_ROOT = pathlib.Path(__file__).resolve().parent.parent
TEMPLATE_DIRECTORY = REPOSITORY_ROOT / "skills" / "monkeymaxxing-solo" / "systems_repository_template"
WORKER_FILE = REPOSITORY_ROOT / "worker" / "solo_outside_alarm_worker.js"

# 40 lowercase hexadecimal characters: a Git commit, which is what a deployed image tag is.
TEST_IMAGE_TAG = "0123456789abcdef0123456789abcdef01234567"
# 43 characters of base64 ending in "=": the shape of a WireGuard public key. They belong to nobody.
TEST_SERVER_WIREGUARD_KEY = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
TEST_LAPTOP_WIREGUARD_KEY = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB="
TEST_SSH_PUBLIC_KEY = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA test laptop"
TEST_VAULT_PASSPHRASE = "a test passphrase that protects nothing real"

# The vault keys that solo_server, deploy and restore_database read for the
# template's example app "shop" and the outside alarm, while alerts are off
# (alert_destination.enabled is false in the template).
TEST_VAULT_KEYS = [
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
]

# The vault keys of each alert destination kind, which solo_server reads only
# while alert_destination.enabled is true and kind names it.
ALERT_VAULT_KEYS = {
    "pushover": ["pushover_application_token", "pushover_user_or_group_key"],
    "slack": ["slack_webhook_url"],
    "discord": ["discord_webhook_url"],
    "telegram": ["telegram_bot_token"],
    "webhook": ["alert_webhook_bearer_token"],
    "email": ["email_login_password"],
}


def find_mmx():
    """The mmx program: the MMX_BIN environment variable, else the one on PATH, else None."""
    from_the_environment = os.environ.get("MMX_BIN")
    if from_the_environment:
        return from_the_environment
    return shutil.which("mmx")


def set_value(text, key, value):
    """Gives `key:` a value on its line, keeping the line's indentation. Fails when the line is missing."""
    pattern = re.compile(r"^(?P<indent>[ ]*)" + re.escape(key) + r":(?P<rest>[ ]*(#.*)?)$", re.MULTILINE)
    new_text, count = pattern.subn(lambda match: match.group("indent") + key + ": " + value, text, count=1)
    if count != 1:
        raise AssertionError("the template has no empty line for " + key)
    return new_text


def replace_once(text, old, new):
    if old not in text:
        raise AssertionError("the template no longer has: " + old)
    return text.replace(old, new, 1)


def fill_the_template(destination):
    """Copies the template into destination and fills every value the setup fills. Returns destination."""
    destination = pathlib.Path(destination)
    shutil.copytree(TEMPLATE_DIRECTORY, destination, symlinks=True)
    settings_file = destination / "mmx_project_settings.yml"
    settings = settings_file.read_text(encoding="utf-8")
    settings = set_value(settings, "project_name", "acme_systems")
    settings = set_value(settings, "cloudflare_account_id", "0123456789abcdef0123456789abcdef")
    settings = set_value(settings, "container_registry_read_only_username", "acme-registry-reader")
    settings = set_value(settings, "r2_s3_endpoint", "0123456789abcdef0123456789abcdef.r2.cloudflarestorage.com")
    settings = set_value(settings, "bucket_name", "acme-systems-backups")
    settings = set_value(settings, "worker_url", "https://solo-outside-alarm.acme-account.workers.dev")
    settings = replace_once(settings, "root_ssh_public_keys: []", 'root_ssh_public_keys: ["' + TEST_SSH_PUBLIC_KEY + '"]')
    settings = replace_once(settings, "https://shop.example.com", "https://shop.acme.test")
    settings_file.write_text(settings, encoding="utf-8")
    inventory_file = destination / "inventories" / "servers.yml"
    inventory = inventory_file.read_text(encoding="utf-8")
    inventory = set_value(inventory, "public_ipv4_address", "203.0.113.10")
    inventory = set_value(inventory, "wireguard_public_key", '"' + TEST_SERVER_WIREGUARD_KEY + '"')
    inventory = set_value(inventory, "laptop_wireguard_public_key", '"' + TEST_LAPTOP_WIREGUARD_KEY + '"')
    inventory = replace_once(inventory, "shop.example.com", "shop.acme.test")
    inventory = replace_once(inventory, "3f2a1c97d4e85b60a1c2d3e4f5061728394a5b6c", TEST_IMAGE_TAG)
    inventory_file.write_text(inventory, encoding="utf-8")
    return destination


def make_the_test_vault(project_directory, mmx, keys=TEST_VAULT_KEYS):
    """Creates secrets_vault.age in the project, with the test passphrase and a made-up value for each key.

    One `mmx secrets edit` writes the whole vault, with a small editor
    program that fills the file, so the vault is unlocked once (each unlock
    takes about a second).
    """
    project_directory = pathlib.Path(project_directory)
    editor = project_directory.parent / "write_the_test_secrets.py"
    lines = "".join(key + ": test-value-of-" + key + "\n" for key in keys)
    editor.write_text("import sys\nopen(sys.argv[1], 'w').write(" + repr(lines) + ")\n", encoding="utf-8")
    environment = dict(os.environ, MMX_VAULT_PASSWORD=TEST_VAULT_PASSPHRASE, EDITOR=sys.executable + " " + str(editor))
    subprocess.run([mmx, "secrets", "edit"], cwd=project_directory, env=environment, check=True, capture_output=True, text=True)


class FilledTemplate:
    """A context manager: a filled temporary copy of the template, removed on exit."""

    def __enter__(self):
        self.temporary_directory = tempfile.TemporaryDirectory(prefix="solo-template-test-")
        self.path = fill_the_template(pathlib.Path(self.temporary_directory.name) / "systems")
        return self.path

    def __exit__(self, *details):
        self.temporary_directory.cleanup()
        return False
