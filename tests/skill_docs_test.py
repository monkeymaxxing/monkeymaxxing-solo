"""Facts about Solo's documentation and coding-agent skill that must always hold.

These tests read the README, docs/, and skills/monkeymaxxing-solo/ as text
(no network, no mmx): every local link and heading anchor resolves; the
skill's copy of the Worker matches the Worker; the skill's front matter is
valid; every vault key that set-up-solo-services writes is documented under
its real name, and no old name survives; the operating procedure table and
the setup files are complete; every command the docs name is one that
exists; and no command or server of the bigger Monkeymaxxing Stack appears
in Solo's setup or procedures.
"""

import pathlib
import re
import sys
import unittest

sys.dont_write_bytecode = True

REPOSITORY = pathlib.Path(__file__).resolve().parent.parent
SKILL = REPOSITORY / "skills" / "monkeymaxxing-solo"
TEMPLATE = SKILL / "systems_repository_template"
HELPER = REPOSITORY / "set_up_solo_services"

# The vault keys the setup helper saves, with the Go constant that names each.
KEYS_THE_HELPER_WRITES = {
    "tunnelTokenKey": "cloudflare_tunnel_token",
    "backupAccessKeyIDKey": "pgbackrest_r2_access_key_id",
    "backupSecretKey": "pgbackrest_r2_secret_access_key",
    "alarmTokenKey": "outside_alarm_check_in_token",
    "pushoverApplicationTokenKey": "pushover_application_token",
    "pushoverUserKeyKey": "pushover_user_or_group_key",
}

# Names these keys had before the playbooks and the helper agreed. Written in
# pieces so this file does not contain them itself.
OLD_KEY_NAMES = [
    "apps_cloudflare" + "_tunnel_token",
    "r2_backup" + "_access_key_id",
    "r2_backup" + "_secret_access_key",
    "solo_outside" + "_alarm_token",
    "pushover_user" + "_key",
]

# Commands, files, and servers of the Monkeymaxxing Stack that Solo does not
# have. None may appear in Solo's setup files or operating procedures.
STACK_ONLY_NAMES = ["desk01", "set-up-outside-services", "run_mmx_on_util01", "mmhotline_settings.yml", "util01", "app001", "db01"]

THE_FOUR_PARTS = ["Your app", "Your server and Cloudflare", "Install and go live", "Alerts"]


def read(path):
    return pathlib.Path(path).read_text(encoding="utf-8")


def markdown_files():
    files = [REPOSITORY / "README.md", REPOSITORY / "AGENTS.md", REPOSITORY / "CONTRIBUTING.md"]
    files += sorted((REPOSITORY / "docs").rglob("*.md"))
    files += sorted(SKILL.rglob("*.md"))
    return files


def without_code(text):
    """The text without fenced code blocks and inline code spans, where a [link](path) is not a link."""
    text = re.sub(r"(?ms)^\s*```.*?^\s*```[^\n]*$", "", text)
    return re.sub(r"`[^`\n]*`", "", text)


def links(text):
    """The targets of each [text](target) link outside code."""
    return re.findall(r"\[[^\]]*\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)", without_code(text))


def github_anchor(heading):
    """The anchor GitHub gives a heading: lowercase, punctuation removed, spaces made hyphens."""
    heading = re.sub(r"\[([^\]]*)\]\([^)]*\)", r"\1", heading)
    heading = heading.strip().lower()
    heading = re.sub(r"[^\w\- ]", "", heading)
    return heading.replace(" ", "-")


def anchors(path):
    """Every heading anchor of a markdown file, with GitHub's -1, -2 for repeated headings."""
    found, seen = set(), {}
    text = re.sub(r"(?ms)^\s*```.*?^\s*```[^\n]*$", "", read(path))
    for match in re.finditer(r"(?m)^#{1,6}\s+(.+?)\s*#*\s*$", text):
        anchor = github_anchor(match.group(1))
        if anchor in seen:
            seen[anchor] += 1
            found.add("%s-%d" % (anchor, seen[anchor]))
        else:
            seen[anchor] = 0
            found.add(anchor)
    return found


def relative(path):
    return str(pathlib.Path(path).relative_to(REPOSITORY))


class LinksTest(unittest.TestCase):
    def test_every_local_link_points_to_a_file_and_heading_that_exist(self):
        checked = 0
        for path in markdown_files():
            for target in links(read(path)):
                if re.match(r"^[a-z][a-z0-9+.-]*:", target):
                    continue
                with self.subTest(file=relative(path), link=target):
                    file_part, _, anchor = target.partition("#")
                    destination = (path.parent / file_part).resolve() if file_part else path
                    self.assertTrue(destination.exists(), relative(path) + " links to a missing " + target)
                    if anchor:
                        self.assertTrue(destination.is_file() and destination.suffix == ".md", target + " has an anchor on a file that is not markdown")
                        self.assertIn(anchor, anchors(destination), relative(path) + " links to a missing heading " + target)
                    checked += 1
        self.assertGreater(checked, 100, "the link check found almost no links: the link pattern is broken")

    def test_the_anchor_rule_matches_githubs(self):
        self.assertEqual(github_anchor("The person's words"), "the-persons-words")
        self.assertEqual(github_anchor("First: the trust line and auto mode"), "first-the-trust-line-and-auto-mode")
        self.assertEqual(github_anchor("When this happens, follow this operating procedure"), "when-this-happens-follow-this-operating-procedure")
        self.assertEqual(github_anchor("Inspect a copy, and copy rows back"), "inspect-a-copy-and-copy-rows-back")


class SkillTest(unittest.TestCase):
    def test_the_skills_worker_is_the_worker(self):
        self.assertEqual(
            (SKILL / "worker" / "solo_outside_alarm_worker.js").read_bytes(),
            (REPOSITORY / "worker" / "solo_outside_alarm_worker.js").read_bytes(),
            "copy worker/solo_outside_alarm_worker.js to skills/monkeymaxxing-solo/worker/ again",
        )

    def test_the_skill_has_valid_front_matter(self):
        text = read(SKILL / "SKILL.md")
        match = re.match(r"\A---\n(.*?)\n---\n", text, re.S)
        self.assertIsNotNone(match, "SKILL.md must start with front matter between --- lines")
        fields = dict(re.findall(r"(?m)^([a-z_]+): (.+)$", match.group(1)))
        self.assertEqual(fields.get("name"), "monkeymaxxing-solo")
        description = fields.get("description", "").strip()
        self.assertGreater(len(description), 100, "the description says when to use the skill")
        self.assertLessEqual(len(description), 1024, "skill descriptions are limited to 1024 characters")
        self.assertIn("monkeymaxxing_solo_checklist.yml", description)

    def test_every_setup_file_is_linked_from_the_skill(self):
        skill = links(read(SKILL / "SKILL.md"))
        setup_files = sorted((TEMPLATE / "initial_setup").glob("*.md"))
        self.assertEqual([path.name[:2] for path in setup_files], ["00", "01", "02", "03", "04", "05"])
        for path in setup_files:
            with self.subTest(setup_file=path.name):
                self.assertTrue(any(target.split("#")[0] == "systems_repository_template/initial_setup/" + path.name for target in skill), path.name + " is not linked from SKILL.md")

    def test_the_skill_names_the_four_parts(self):
        for path in (SKILL / "SKILL.md", TEMPLATE / "initial_setup" / "00_before_you_start.md", TEMPLATE / "talking_with_the_team.md"):
            text = " ".join(read(path).split())
            for part in THE_FOUR_PARTS:
                with self.subTest(file=relative(path), part=part):
                    self.assertTrue(part in text, relative(path) + " does not name the part " + repr(part))

    def test_the_first_screen_has_the_trust_line_and_the_pace_paragraph(self):
        text = read(TEMPLATE / "initial_setup" / "01_answer_two_questions.md")
        self.assertIn("USE ONLY A CODING AGENT YOU TRUST.  It will have full control of your server.", text)
        self.assertIn('Say "guide me" any time', text)

    def test_claude_md_points_to_agents_md(self):
        self.assertEqual(read(TEMPLATE / "CLAUDE.md"), "@AGENTS.md\n")

    def test_the_app_workflow_builds_for_solo01_with_the_full_commit(self):
        text = read(SKILL / "files_for_your_app" / "test_build_and_push_the_app_image.yml")
        for needed in ("--platform linux/amd64", '--tag "$image_name:${{ github.sha }}"', "packages: write", "image: postgres:17"):
            self.assertIn(needed, text)


class VaultKeysTest(unittest.TestCase):
    def test_the_helper_writes_the_keys_the_playbooks_read(self):
        source = "\n".join(read(path) for path in HELPER.glob("*.go") if not path.name.endswith("_test.go"))
        playbooks = "\n".join(read(path) for path in (TEMPLATE / "playbooks").glob("*.yml"))
        for constant, key in KEYS_THE_HELPER_WRITES.items():
            with self.subTest(key=key):
                self.assertRegex(source, r"\b" + constant + r"\s*=\s*\"" + key + "\"")
                self.assertIn(".secrets." + key + " ", playbooks + " ")

    def test_every_key_the_helper_writes_is_documented(self):
        agents = read(TEMPLATE / "AGENTS.md")
        for key in KEYS_THE_HELPER_WRITES.values():
            with self.subTest(key=key):
                self.assertIn("`" + key + "`", agents)
        setup = read(TEMPLATE / "initial_setup" / "03_create_your_server_and_cloudflare.md")
        for key in ("cloudflare_tunnel_token", "pgbackrest_r2_access_key_id", "pgbackrest_r2_secret_access_key", "outside_alarm_check_in_token"):
            self.assertIn("`" + key + "`", setup)
        alerts = read(TEMPLATE / "initial_setup" / "05_turn_on_alerts.md")
        for key in ("pushover_application_token", "pushover_user_or_group_key"):
            self.assertIn("`" + key + "`", alerts)

    def test_every_key_a_playbook_reads_is_in_the_agents_secrets_table(self):
        agents = read(TEMPLATE / "AGENTS.md").split("## Secrets and where each one comes from")[1].split("\n## ")[0]
        for playbook in (TEMPLATE / "playbooks").glob("*.yml"):
            for key in set(re.findall(r"\.secrets\.([A-Za-z0-9_]+)", read(playbook))):
                if key.startswith("shop_"):
                    continue  # the example app's keys follow the <app>_ and <database_name>_ rows
                with self.subTest(playbook=playbook.name, key=key):
                    self.assertIn("`" + key + "`", agents)

    def test_no_old_key_name_is_left_anywhere(self):
        this_file = pathlib.Path(__file__).resolve()
        for path in sorted(REPOSITORY.rglob("*")):
            if not path.is_file() or ".git" in path.parts or "__pycache__" in path.parts or path.resolve() == this_file:
                continue
            if path.suffix in (".asc", ".gpg", ".pyc"):
                continue
            text = path.read_text(encoding="utf-8", errors="replace")
            for name in OLD_KEY_NAMES:
                with self.subTest(file=relative(path), name=name):
                    self.assertIsNone(re.search(r"\b" + name + r"\b", text), relative(path) + " uses the old vault key " + name)


class ProceduresTest(unittest.TestCase):
    def procedure_table(self):
        agents = read(TEMPLATE / "AGENTS.md")
        return agents.split("## When this happens, follow this operating procedure")[1].split("\n## ")[0]

    def test_every_procedure_in_the_table_exists(self):
        targets = {target.split("#")[0] for target in links(self.procedure_table())}
        self.assertGreaterEqual(len(targets), 9)
        for target in targets:
            with self.subTest(procedure=target):
                self.assertTrue(target.startswith("operating_procedures/"), target)
                self.assertTrue((TEMPLATE / target).is_file(), target + " does not exist")

    def test_every_procedure_on_disk_is_in_the_table(self):
        targets = {target.split("#")[0] for target in links(self.procedure_table())}
        for path in sorted((TEMPLATE / "operating_procedures").glob("*.md")):
            with self.subTest(procedure=path.name):
                self.assertIn("operating_procedures/" + path.name, targets, path.name + " is not in AGENTS.md's procedure table")

    def test_no_stack_only_command_or_server_in_the_setup_or_the_procedures(self):
        for directory in ("initial_setup", "operating_procedures"):
            for path in sorted((TEMPLATE / directory).glob("*.md")):
                text = read(path)
                for name in STACK_ONLY_NAMES:
                    with self.subTest(file=relative(path), name=name):
                        self.assertIsNone(re.search(r"(?<![\w-])" + re.escape(name) + r"(?![\w-])", text), relative(path) + " names " + name)


class CommandsTest(unittest.TestCase):
    def documents(self):
        return [path for path in markdown_files() if SKILL in path.parents]

    def test_every_playbook_the_docs_run_exists(self):
        for path in self.documents():
            for name in set(re.findall(r"\bmmx (?:run|dryrun|validate) ([a-z_]+)\b", read(path))):
                if name.upper() == name or name in ("playbook",):
                    continue
                with self.subTest(file=relative(path), playbook=name):
                    self.assertTrue((TEMPLATE / "playbooks" / (name + ".yml")).is_file(), name)

    def test_every_command_line_variable_the_docs_pass_exists(self):
        playbooks = {path.stem: read(path) for path in (TEMPLATE / "playbooks").glob("*.yml")}
        for path in self.documents():
            for playbook, rest in re.findall(r"\bmmx run ([a-z_]+)((?: --var [a-z_]+=\S+)+)", read(path)):
                for variable in re.findall(r"--var ([a-z_]+)=", rest):
                    with self.subTest(file=relative(path), playbook=playbook, variable=variable):
                        self.assertRegex(playbooks[playbook], r"(?m)^  " + variable + r":$")

    def test_every_setup_helper_command_and_flag_exists(self):
        source = "\n".join(read(path) for path in HELPER.glob("*.go") if not path.name.endswith("_test.go"))
        flags = set(re.findall(r"flags\.(?:String|Int|Bool)\(\"([a-z-]+)\"", source)) | {"version", "help"}
        subcommands = {"secrets list", "secrets set", "secrets generate", "secrets set-pushover", "cloudflare"}
        uses = 0
        for path in self.documents():
            for line in read(path).splitlines():
                for command in re.findall(r"set-up-solo-services ((?:secrets [a-z-]+|cloudflare)\b[^`\n]*)", line):
                    words = command.split()
                    name = " ".join(words[:2]) if words[0] == "secrets" else words[0]
                    with self.subTest(file=relative(path), command=command):
                        self.assertIn(name, subcommands)
                        for flag in re.findall(r"(?<![\w-])--([a-z][a-z-]*)", command):
                            self.assertIn(flag, flags, "--" + flag + " is not a flag of set-up-solo-services")
                    uses += 1
        self.assertGreater(uses, 15)


if __name__ == "__main__":
    unittest.main()
