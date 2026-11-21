"""pgBackRest's settings, and the weekly local restore check program.

Part 1, pgbackrest.conf: the template files_used_by_several_roles/pgbackrest.conf
writes /etc/pgbackrest/pgbackrest.conf for both the backups and a restore. The
test checks what makes it work with Cloudflare R2 (path-style addresses,
the region auto, aes-256-cbc encryption, retention by time) and runs the
template's own refusal rules ("fail" guards) over a table of values, so a
retention below 14 days, a lock below 8 days, or a lock longer than the
retention stops a run before anything changes. The guards are evaluated from the
template's text with a small interpreter of the few template functions they
use, not copied into the test.

Part 2, run_local_restore_check: the program that restores the newest backup
into a throwaway Postgres on a private Unix socket (the Postgres program
that Debian installed, no container) and runs each app's check queries. The
test runs it with a stand-in for every command (pgbackrest, pg_ctl, psql, chown),
and checks that it stops that Postgres and removes the scratch directory in every
outcome, writes a result that says what failed, and refuses a check that
proves nothing.
"""

import importlib.machinery
import importlib.util
import json
import os
import pathlib
import re
import sys
import tempfile
import unittest
from unittest import mock

# Loading the restore check program must not write bytecode next to it in the template.
sys.dont_write_bytecode = True

import template_filler

TEMPLATE = template_filler.TEMPLATE_DIRECTORY
PGBACKREST_TEMPLATE = TEMPLATE / "files_used_by_several_roles" / "pgbackrest.conf"
POSTGRES_ROLE_DIRECTORY = TEMPLATE / "roles" / "postgres_and_backups"
RESTORE_CHECK_PROGRAM = POSTGRES_ROLE_DIRECTORY / "files" / "run_local_restore_check"
RESTORE_CHECK_SETTINGS_TEMPLATE = POSTGRES_ROLE_DIRECTORY / "files" / "restore_check_settings.json"


# ---- a small interpreter for the template functions the refusal guards use ----

def tokens_of(text):
    return re.findall(r'\(|\)|"(?:[^"\\]|\\.)*"|[^\s()]+', text)


def parse(tokens):
    token = tokens.pop(0)
    if token == "(":
        expression = []
        while tokens[0] != ")":
            expression.append(parse(tokens))
        tokens.pop(0)
        return expression
    return token


def evaluate(expression, variables):
    if isinstance(expression, str):
        if expression.startswith('"'):
            return json.loads(expression)
        if expression.startswith("$"):
            return variables[expression]
        return int(expression)
    function, arguments = expression[0], [evaluate(argument, variables) for argument in expression[1:]]
    if function == "not":
        return not arguments[0]
    if function == "regexMatch":
        return re.search(arguments[0], arguments[1]) is not None
    if function == "toString":
        return str(arguments[0])
    if function == "int":
        return int(arguments[0])
    if function == "lt":
        return arguments[0] < arguments[1]
    if function == "gt":
        return arguments[0] > arguments[1]
    raise AssertionError("the guard uses a template function this test does not know: " + function)


def guards_of(template_text):
    """The (condition, message) of each `{{- if CONDITION }}{{ fail (printf "MESSAGE" ...) }}{{ end -}}` at the top of the template."""
    found = re.findall(r'\{\{- if (.+?) \}\}\{\{ fail \(printf ("(?:[^"\\]|\\.)*")', template_text)
    return [(parse(tokens_of("(" + condition + ")")), json.loads(message)) for condition, message in found]


def refusals(template_text, keep, lock):
    """The message of the first guard that fails the run, as a list; empty when the values pass.

    A template stops at its first `fail`, so a later guard never reads a value
    an earlier guard refused (a keep of "twenty-one" is refused for not being
    a number before another guard turns it into an int).
    """
    variables = {"$keep": keep, "$lock": lock}
    for condition, message in guards_of(template_text):
        if evaluate(condition, variables):
            return [message]
    return []


class PgbackrestSettingsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.text = PGBACKREST_TEMPLATE.read_text(encoding="utf-8")

    def test_it_talks_to_r2_with_path_style_addresses_and_the_region_auto(self):
        for line in ("repo1-type=s3", "repo1-s3-region=auto", "repo1-s3-uri-style=path"):
            self.assertRegex(self.text, r"(?m)^" + re.escape(line) + r"$", line)
        self.assertNotIn("uri-style=host", self.text)
        self.assertIn("repo1-s3-endpoint={{ .vars.pgbackrest_settings.r2_s3_endpoint }}", self.text)
        self.assertIn("repo1-s3-bucket={{ .vars.pgbackrest_settings.bucket_name }}", self.text)
        self.assertIn("repo1-s3-key={{ .vars.pgbackrest_settings.bucket_key_id }}", self.text)
        self.assertIn("repo1-s3-key-secret={{ .vars.pgbackrest_settings.bucket_secret_key }}", self.text)

    def test_every_file_is_encrypted_before_it_leaves_the_server(self):
        self.assertRegex(self.text, r"(?m)^repo1-cipher-type=aes-256-cbc$")
        self.assertIn("repo1-cipher-pass={{ .vars.pgbackrest_settings.repository_cipher_passphrase }}", self.text)

    def test_full_backups_are_kept_by_time_and_a_backup_holds_the_log_it_needs(self):
        self.assertRegex(self.text, r"(?m)^repo1-retention-full-type=time$")
        self.assertIn("repo1-retention-full={{ int $keep }}", self.text)
        # The weekly restore check starts a restored copy from the backup's own files.
        self.assertRegex(self.text, r"(?m)^archive-copy=y$")

    def test_the_stanza_main_names_the_data_directory_and_the_port(self):
        self.assertRegex(self.text, r"(?m)^\[main\]$")
        self.assertIn("pg1-path={{ .vars.pgbackrest_settings.data_directory }}", self.text)
        self.assertIn("pg1-port=5432", self.text)

    def test_the_guards_were_found(self):
        self.assertEqual(len(guards_of(self.text)), 5)

    def test_the_guards_accept_exactly_the_values_the_rule_allows(self):
        # The rule: keep is a whole number >= 14; lock is a whole number >= 8 and <= keep.
        for keep in (7, 13, 14, 21, 30, 365):
            for lock in (1, 7, 8, 14, 21, 22, 30, 400):
                with self.subTest(keep=keep, lock=lock):
                    allowed = keep >= 14 and lock >= 8 and lock <= keep
                    found = refusals(self.text, keep, lock)
                    self.assertEqual(not found, allowed, "keep " + str(keep) + ", lock " + str(lock) + ": " + str(found))

    def test_the_guards_refuse_values_that_are_not_whole_numbers(self):
        for keep, lock in (("21.5", 14), (21, "14.5"), ("twenty-one", 14), (21, "fourteen"), ("-21", 14), (21, "")):
            with self.subTest(keep=keep, lock=lock):
                self.assertTrue(refusals(self.text, keep, lock), "should refuse keep " + repr(keep) + ", lock " + repr(lock))

    def test_each_refusal_names_the_setting_and_says_nothing_changed(self):
        self.assertIn("backups.keep_database_backups_for_days", refusals(self.text, 13, 8)[0])
        self.assertIn("minimum of 14", refusals(self.text, 13, 8)[0])
        self.assertIn("backups.lock_backups_against_deletion_for_days", refusals(self.text, 21, 7)[0])
        self.assertIn("minimum of 8", refusals(self.text, 21, 7)[0])
        too_long = refusals(self.text, 14, 15)
        self.assertEqual(len(too_long), 1)
        self.assertIn("more than backups.keep_database_backups_for_days", too_long[0])
        for _, message in guards_of(self.text):
            self.assertIn("Nothing changed.", message)

    def test_the_template_defaults_pass_the_guards(self):
        settings = (TEMPLATE / "mmx_project_settings.yml").read_text(encoding="utf-8")
        keep = int(re.search(r"(?m)^\s+keep_database_backups_for_days: (\d+)\b", settings).group(1))
        lock = int(re.search(r"(?m)^\s+lock_backups_against_deletion_for_days: (\d+)\b", settings).group(1))
        self.assertEqual((keep, lock), (21, 14))
        self.assertEqual(refusals(self.text, keep, lock), [])

    def test_backups_and_a_restore_write_the_file_from_the_same_template(self):
        role = (POSTGRES_ROLE_DIRECTORY / "main.yml").read_text(encoding="utf-8")
        restore = (TEMPLATE / "playbooks" / "restore_database.yml").read_text(encoding="utf-8")
        self.assertIn("template: ../../files_used_by_several_roles/pgbackrest.conf", role)
        self.assertIn("template: files_used_by_several_roles/pgbackrest.conf", restore)

    def test_postgres_archives_to_pgbackrest_every_minute_and_listens_only_on_the_wireguard_address(self):
        role = (POSTGRES_ROLE_DIRECTORY / "main.yml").read_text(encoding="utf-8")
        for line in (
            "listen_addresses = '{{ .inputs.private_ip }}'",
            "shared_buffers = 128MB",
            "archive_mode = on",
            "archive_command = 'pgbackrest --config=/etc/pgbackrest/pgbackrest.conf --stanza=main archive-push %p'",
            "archive_timeout = 60",
        ):
            self.assertIn(line, role)


# ---- the weekly local restore check -------------------------------------------

def load_program():
    loader = importlib.machinery.SourceFileLoader("run_local_restore_check", str(RESTORE_CHECK_PROGRAM))
    specification = importlib.util.spec_from_loader("run_local_restore_check", loader)
    module = importlib.util.module_from_spec(specification)
    loader.exec_module(module)
    return module


PROGRAM = load_program()


def sample_settings(directory, **changes):
    settings = {
        "postgresql_major_version": 17,
        "pgbackrest_config_file": "/etc/pgbackrest/pgbackrest.conf",
        "stanza": "main",
        "scratch_directory": str(directory / "scratch"),
        "result_file": str(directory / "result" / "last-result.json"),
        "seconds_to_wait_for_postgres": 30,
        "seconds_to_wait_for_the_restore": 600,
        "apps": {
            "shop": {"database_name": "shop", "queries": [{"name": "the_migrations_came_back", "sql": "select count(*) > 0 from schema_migrations"}]},
            "blog": {"database_name": "blog", "queries": [{"name": "a_post_came_back", "sql": "select count(*) > 0 from posts"}]},
        },
    }
    settings.update(changes)
    return settings


class FakeServer:
    """A stand-in for every command the program runs. It records them and answers as chosen."""

    def __init__(self, settings, restore_exit=0, answers=None, ready_after=0, info_exit=0, info_has_backup=True, start_exit=0):
        self.settings = settings
        self.commands = []
        self.restore_exit = restore_exit
        self.answers = answers or {}
        self.ready_after = ready_after
        self.ready_checks = 0
        self.info_exit = info_exit
        self.info_has_backup = info_has_backup
        self.start_exit = start_exit
        self.running = False
        self.time = 0

    def run(self, arguments, timeout_seconds=None, extra_environment=None):
        self.commands.append(list(arguments))
        program = arguments[0]
        if program == "pgbackrest":
            backups = [{"label": "20261101-010003F"}, {"label": "20261102-010002D_20261101-010003F"}] if self.info_has_backup else []
            return self.info_exit, json.dumps([{"name": "main", "backup": backups}]), "" if self.info_exit == 0 else "info failed"
        if program == "runuser":
            assert arguments[1:4] == ["-u", "postgres", "--"], arguments
            program_name = os.path.basename(arguments[4] if arguments[4] != "env" else arguments[6])
            data = self.settings["scratch_directory"] + "/data"
            if program_name == "pgbackrest":
                if self.restore_exit == 0:
                    os.makedirs(data, exist_ok=True)
                    pathlib.Path(data + "/PG_VERSION").write_text("17\n", encoding="utf-8")
                return self.restore_exit, "", "" if self.restore_exit == 0 else "the repository answered 403"
            if program_name == "pg_ctl":
                if arguments[-1] == "start":
                    if self.start_exit != 0:
                        pathlib.Path(self.settings["scratch_directory"] + "/postgres.log").write_text("FATAL:  could not map shared memory\n", encoding="utf-8")
                        return self.start_exit, "", "pg_ctl: could not start server"
                    self.running = True
                    pathlib.Path(data + "/postmaster.pid").write_text("4242\n", encoding="utf-8")
                    return 0, "", ""
                self.running = False
                pathlib.Path(data + "/postmaster.pid").unlink(missing_ok=True)
                return 0, "", ""
            if program_name == "psql":
                sql = arguments[-1]
                if sql == "select pg_is_in_recovery()":
                    self.ready_checks += 1
                    return (0, "f\n", "") if self.ready_checks > self.ready_after else (1, "", "the database system is starting up")
                database = arguments[arguments.index("--dbname") + 1]
                answer = self.answers.get(database, "t")
                if answer.startswith("error:"):
                    return 1, "", answer[6:]
                return 0, answer + "\n", ""
        if program == "chown":
            return 0, "", ""
        if program == "rm":
            return 0, "", ""
        raise AssertionError("an unexpected command: " + repr(arguments))

    def sleep(self, seconds):
        self.time += seconds

    def clock(self):
        return self.time


class RestoreCheckProgramTest(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix="solo-restore-check-test-")
        self.addCleanup(temporary.cleanup)
        self.directory = pathlib.Path(temporary.name)
        self.settings = sample_settings(self.directory)

    def run_check(self, **fake_options):
        server = FakeServer(self.settings, **fake_options)
        result = PROGRAM.run_the_check(self.settings, run=server.run, sleep=server.sleep, clock=server.clock)
        return result, server

    def written_result(self):
        return json.loads(pathlib.Path(self.settings["result_file"]).read_text(encoding="utf-8"))

    def assert_cleaned_up(self, server):
        self.assertFalse(os.path.exists(self.settings["scratch_directory"]), "the scratch directory is still there")
        self.assertFalse(server.running, "the throwaway Postgres is still running")
        self.assertNoContainerProgram(server)

    def assertNoContainerProgram(self, server):
        self.assertFalse([command for command in server.commands if command[0] in ("podman", "docker")], "the check must not use a container program")

    def stops(self, server):
        return [command for command in server.commands if command[:4] == ["runuser", "-u", "postgres", "--"] and command[-1] == "stop"]

    def test_a_passing_check_restores_runs_every_query_and_cleans_up(self):
        result, server = self.run_check()
        self.assertEqual(result["result"], "passed", result)
        self.assertEqual(result["backup_label"], "20261102-010002D_20261101-010003F")
        self.assertEqual([(check["app"], check["query"], check["passed"]) for check in result["checks"]], [("blog", "a_post_came_back", True), ("shop", "the_migrations_came_back", True)])
        self.assertEqual(self.written_result(), result)
        self.assert_cleaned_up(server)
        self.assertFalse(os.path.exists(self.settings["result_file"] + ".new"))
        self.assertEqual(oct(os.stat(self.settings["result_file"]).st_mode & 0o777), "0o644")

    def test_the_restore_runs_as_postgres_into_the_scratch_directory_and_stops_at_the_backups_own_log(self):
        _, server = self.run_check()
        restore = next(command for command in server.commands if command[0] == "runuser")
        self.assertEqual(restore[:4], ["runuser", "-u", "postgres", "--"])
        self.assertIn("--pg1-path=" + self.settings["scratch_directory"] + "/data", restore)
        self.assertIn("--type=immediate", restore)
        self.assertIn("--archive-mode=off", restore)
        self.assertEqual(restore[-1], "restore")

    def test_the_throwaway_postgres_is_the_debian_one_on_a_private_socket_with_no_network_port(self):
        _, server = self.run_check()
        scratch = self.settings["scratch_directory"]
        start = next(command for command in server.commands if command[-1] == "start")
        self.assertEqual(start[:5], ["runuser", "-u", "postgres", "--", "/usr/lib/postgresql/17/bin/pg_ctl"])
        self.assertEqual(start[start.index("-D") + 1], scratch + "/data")
        self.assertEqual(start[start.index("-l") + 1], scratch + "/postgres.log")
        self.assertIn("-w", start)
        self.assertEqual(start[start.index("-t") + 1], "30")
        options = start[start.index("-o") + 1]
        self.assertIn("-c listen_addresses= ", options + " ")
        self.assertIn("-c unix_socket_directories=" + scratch + "/socket", options)
        self.assertIn("-c archive_mode=off", options)
        self.assertNotIn("'", options, "pg_ctl -o needs no nested quotes")
        self.assertNoContainerProgram(server)

    def test_the_socket_directory_is_made_private_and_given_to_postgres_before_the_start(self):
        socket_modes = []
        original = PROGRAM.restored_cluster_files

        def look_at_the_socket(settings):
            socket = pathlib.Path(settings["scratch_directory"]) / "socket"
            socket_modes.append(oct(socket.stat().st_mode & 0o777))
            return original(settings)

        with mock.patch.object(PROGRAM, "restored_cluster_files", look_at_the_socket):
            _, server = self.run_check()
        self.assertEqual(socket_modes, ["0o700"])
        scratch = self.settings["scratch_directory"]
        self.assertIn(["chown", "postgres:postgres", scratch, scratch + "/socket"], server.commands)
        cluster_files = [scratch + "/data/" + name for name in ("postgresql.conf", "pg_hba.conf", "pg_ident.conf")]
        self.assertIn(["chown", "postgres:postgres"] + cluster_files, server.commands)

    def test_the_cluster_files_a_debian_cluster_keeps_elsewhere_are_written_next_to_the_data(self):
        written = {}
        original = PROGRAM.clean_up

        def keep_a_look_before_cleaning_up(settings, run=PROGRAM.run_command):
            data = pathlib.Path(settings["scratch_directory"]) / "data"
            if data.exists():
                for name in ("postgresql.conf", "pg_hba.conf", "pg_ident.conf"):
                    if (data / name).exists():
                        written[name] = (data / name).read_text(encoding="utf-8")
            original(settings, run)

        with mock.patch.object(PROGRAM, "clean_up", keep_a_look_before_cleaning_up):
            self.run_check()
        self.assertEqual(sorted(written), ["pg_hba.conf", "pg_ident.conf", "postgresql.conf"])
        self.assertEqual(written["pg_hba.conf"], "local all all trust\n")
        for line in ("listen_addresses = ''", "archive_mode = off", "max_connections = 100", "shared_buffers = 64MB", "unix_socket_directories = '" + self.settings["scratch_directory"] + "/socket'"):
            self.assertIn(line + "\n", written["postgresql.conf"])

    def test_queries_run_read_only_against_each_apps_database(self):
        _, server = self.run_check()
        psql_commands = [command for command in server.commands if "/usr/lib/postgresql/17/bin/psql" in command]
        queries = [command for command in psql_commands if command[-1] != "select pg_is_in_recovery()"]
        self.assertEqual({command[command.index("--dbname") + 1] for command in queries}, {"shop", "blog"})
        for command in psql_commands:
            self.assertEqual(command[:7], ["runuser", "-u", "postgres", "--", "env", "PGOPTIONS=-c default_transaction_read_only=on", "/usr/lib/postgresql/17/bin/psql"])
            self.assertEqual(command[command.index("--host") + 1], self.settings["scratch_directory"] + "/socket")
            self.assertEqual(command[command.index("--username") + 1], "postgres")
            self.assertNotIn("--port", command, "the throwaway Postgres has no network port")

    def test_a_query_that_does_not_answer_true_fails_the_check_and_names_it(self):
        result, server = self.run_check(answers={"shop": "f"})
        self.assertEqual(result["result"], "failed")
        self.assertIn("shop/the_migrations_came_back", result["error"])
        self.assertEqual([check["passed"] for check in result["checks"]], [True, False])
        self.assertEqual(self.written_result()["result"], "failed")
        self.assert_cleaned_up(server)

    def test_a_query_that_errors_fails_the_check_with_the_error(self):
        result, server = self.run_check(answers={"blog": 'error:relation "posts" does not exist'})
        self.assertEqual(result["result"], "failed")
        self.assertIn("blog/a_post_came_back", result["error"])
        self.assertEqual(result["checks"][0]["answer"], 'relation "posts" does not exist')
        self.assert_cleaned_up(server)

    def test_a_failed_restore_still_removes_the_scratch_directory(self):
        os.makedirs(self.settings["scratch_directory"] + "/left_over", exist_ok=True)
        result, server = self.run_check(restore_exit=1)
        self.assertEqual(result["result"], "failed")
        self.assertIn("the restore from the bucket failed", result["error"])
        self.assertIn("403", result["error"])
        self.assertEqual(self.written_result()["result"], "failed")
        self.assert_cleaned_up(server)
        self.assertFalse(any(command[-1] == "start" for command in server.commands), "no Postgres starts when the restore failed")

    def test_a_leftover_from_a_killed_check_is_removed_before_the_check_starts(self):
        os.makedirs(self.settings["scratch_directory"] + "/left_over", exist_ok=True)
        result, server = self.run_check()
        self.assertEqual(result["result"], "passed")
        self.assertFalse(os.path.exists(self.settings["scratch_directory"] + "/left_over"))

    def test_a_throwaway_postgres_that_a_killed_check_left_running_is_stopped_before_its_directory_is_removed(self):
        data = pathlib.Path(self.settings["scratch_directory"]) / "data"
        data.mkdir(parents=True)
        (data / "postmaster.pid").write_text("4242\n", encoding="utf-8")
        result, server = self.run_check()
        self.assertEqual(result["result"], "passed", result)
        # The very first command is the stop of the leftover Postgres, and it works on the scratch data only.
        self.assertEqual(server.commands[0], ["runuser", "-u", "postgres", "--", "/usr/lib/postgresql/17/bin/pg_ctl", "-D", str(data), "-m", "immediate", "stop"])
        self.assertEqual(len(self.stops(server)), 2, "once for the leftover and once at the end of this check")
        for command in server.commands:
            self.assertFalse(any("/var/lib/postgresql" in argument for argument in command), "the live cluster is never touched")

    def test_the_throwaway_postgres_is_stopped_before_the_scratch_directory_is_removed_even_when_a_query_fails(self):
        seen = {}
        original = PROGRAM.shutil.rmtree

        def note_whether_postgres_still_ran(path, *arguments, **options):
            seen.setdefault("running_at_removal", []).append(server.running)
            original(path, *arguments, **options)

        server = FakeServer(self.settings, answers={"shop": "f"})
        with mock.patch.object(PROGRAM.shutil, "rmtree", note_whether_postgres_still_ran):
            result = PROGRAM.run_the_check(self.settings, run=server.run, sleep=server.sleep, clock=server.clock)
        self.assertEqual(result["result"], "failed")
        self.assertEqual(seen["running_at_removal"][-1], False)
        self.assertEqual(server.commands[-1][-1], "stop")
        self.assert_cleaned_up(server)

    def test_a_postgres_that_cannot_start_fails_with_the_end_of_its_log_and_still_cleans_up(self):
        result, server = self.run_check(start_exit=1)
        self.assertEqual(result["result"], "failed")
        self.assertIn("could not start the restored Postgres", result["error"])
        self.assertIn("could not map shared memory", result["error"])
        self.assert_cleaned_up(server)

    def test_no_backup_in_the_bucket_fails_with_the_reason(self):
        result, server = self.run_check(info_has_backup=False)
        self.assertEqual(result["result"], "failed")
        self.assertIn("no backup in the bucket yet", result["error"])
        self.assert_cleaned_up(server)

    def test_a_postgres_that_never_finishes_starting_fails_after_the_wait(self):
        result, server = self.run_check(ready_after=10 ** 6)
        self.assertEqual(result["result"], "failed")
        self.assertIn("did not finish starting within 30 seconds", result["error"])
        self.assertGreaterEqual(server.time, 30)
        self.assert_cleaned_up(server)

    def test_a_postgres_that_finishes_starting_late_is_waited_for(self):
        result, server = self.run_check(ready_after=3)
        self.assertEqual(result["result"], "passed", result)
        self.assertEqual(server.time, 15)

    def test_a_check_with_no_queries_proves_nothing_and_fails(self):
        self.settings["apps"] = {"shop": {"database_name": "shop", "queries": []}}
        result, server = self.run_check()
        self.assertEqual(result["result"], "failed")
        self.assertIn("no app has a restore_drill_check_queries entry", result["error"])
        self.assert_cleaned_up(server)

    def test_a_scratch_directory_that_stays_makes_the_check_fail(self):
        with mock.patch.object(PROGRAM.shutil, "rmtree", lambda *arguments, **options: None):
            result, _ = self.run_check()
        self.assertEqual(result["result"], "failed")
        self.assertIn("could not remove the scratch directory", result["error"])

    def test_the_plan_lists_the_commands_and_runs_nothing(self):
        settings_file = self.directory / "settings.json"
        settings_file.write_text(json.dumps(self.settings), encoding="utf-8")
        with mock.patch("builtins.print") as printed, mock.patch.object(PROGRAM, "run_command", side_effect=AssertionError("the plan must run nothing")):
            self.assertEqual(PROGRAM.main(["--settings", str(settings_file), "--print-plan"]), 0)
        plan = json.loads(printed.call_args[0][0])
        self.assertEqual(sorted(plan), ["newest_backup", "remove_the_scratch_directory", "restore", "start_postgres", "stop_postgres"])
        self.assertFalse([name for name, command in plan.items() if "podman" in command or "docker" in command])
        self.assertFalse(os.path.exists(self.settings["result_file"]))

    def test_a_missing_settings_file_exits_with_one_and_names_the_file(self):
        with mock.patch("sys.stderr") as standard_error:
            self.assertEqual(PROGRAM.main(["--settings", str(self.directory / "missing.json")]), 1)
        self.assertIn("missing.json", "".join(call.args[0] for call in standard_error.write.call_args_list))

    def test_main_exits_with_one_when_the_check_fails_and_zero_when_it_passes(self):
        settings_file = self.directory / "settings.json"
        settings_file.write_text(json.dumps(self.settings), encoding="utf-8")
        for answers, expected in (({}, 0), ({"shop": "f"}, 1)):
            with self.subTest(answers=answers):
                server = FakeServer(self.settings, answers=answers)
                with mock.patch.object(PROGRAM, "run_command", server.run), mock.patch.object(PROGRAM.time, "sleep", server.sleep), mock.patch("builtins.print"):
                    self.assertEqual(PROGRAM.main(["--settings", str(settings_file)]), expected)

    def test_the_settings_file_template_has_the_keys_the_program_reads(self):
        template = RESTORE_CHECK_SETTINGS_TEMPLATE.read_text(encoding="utf-8")
        written_by_the_template = set(re.findall(r'"([a-z_0-9]+)"', template))
        # "main" is the stanza's name, the value of the key stanza.
        read_by_the_program = set(self.settings) | {"database_name", "queries", "main"}
        self.assertEqual(written_by_the_template, read_by_the_program)

    def test_the_settings_file_template_names_no_container(self):
        template = RESTORE_CHECK_SETTINGS_TEMPLATE.read_text(encoding="utf-8")
        for word in ("container", "image", "docker"):
            self.assertNotIn(word, template.lower())

    def test_the_role_installs_no_container_program_for_the_restore_check(self):
        role = (POSTGRES_ROLE_DIRECTORY / "main.yml").read_text(encoding="utf-8")
        self.assertNotIn("install_podman_for_the_restore_check", role)
        program = RESTORE_CHECK_PROGRAM.read_text(encoding="utf-8")
        for word in ("podman", "docker", "container_image"):
            self.assertNotIn(word, program)


if __name__ == "__main__":
    unittest.main()
