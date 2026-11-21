"""The outside alarm's check-in script against the Worker's own validation.

send_outside_alarm_check_in.py builds the JSON that solo01 POSTs to the
Cloudflare Worker's /check-in every minute. The Worker refuses a check-in that
breaks its rules (worker/solo_outside_alarm_worker.js, checkCheckIn), and a
refused check-in means no alert would ever fire. So the test runs the script
with sample settings and sample secrets for each alert destination kind
(Pushover, Slack, Discord, Telegram, webhook), with and without health
checks, and passes every payload through the Worker's real checkCheckIn()
with node. It also sends one check-in to a local stand-in for the Worker, to
check the address, the Authorization header and the exit codes.

Needs node 22.15 or newer (the same as worker/solo_outside_alarm_worker_test.mjs).
"""

import http.server
import json
import pathlib
import re
import shutil
import subprocess
import sys
import tempfile
import threading
import unittest

import template_filler

SCRIPT = template_filler.TEMPLATE_DIRECTORY / "roles" / "monitoring_and_outside_alarm" / "files" / "send_outside_alarm_check_in.py"
CHECKER = pathlib.Path(__file__).resolve().parent / "check_payload_with_the_worker.mjs"
SETTINGS_TEMPLATE = template_filler.TEMPLATE_DIRECTORY / "roles" / "monitoring_and_outside_alarm" / "files" / "outside_alarm_check_in_settings.json"

SECRET_FILES = {
    "pushover-application-token": "sample-pushover-application-token\n",
    "pushover-user-or-group-key": "sample-pushover-user-key\n",
    "slack-webhook-url": "https://hooks.slack.com/services/T000/B000/sample\n",
    "discord-webhook-url": "https://discord.com/api/webhooks/123456789012345678/sample\n",
    "telegram-bot-token": "123456789:sample-bot-token\n",
    "alert-webhook-bearer-token": "sample-bearer-token\n",
}


def sample_settings(directory, kind, enabled=True, public_site_checks_run=True, **changes):
    secrets_directory = directory / "mmwatchdog_secrets"
    secrets_directory.mkdir(exist_ok=True)
    for name, content in SECRET_FILES.items():
        (secrets_directory / name).write_text(content, encoding="utf-8")
    token_file = directory / "check-in-token"
    token_file.write_text("sample-check-in-token\n", encoding="utf-8")
    settings = {
        "server_name": "solo01",
        "worker_url": "https://solo-outside-alarm.acme-account.workers.dev",
        "outside_alarm_delay_seconds": 300,
        "health_check_failures_before_alert": 2,
        "public_site_checks_run": public_site_checks_run,
        "secrets_directory": str(secrets_directory),
        "check_in_token_file": str(token_file),
        "alert_destination": {
            "enabled": enabled,
            "kind": kind,
            "pushover_sound": "cosmic",
            "pushover_priority": 1,
            "telegram_chat_id": "-1001234567890",
            "webhook_url": "https://hooks.acme.test/alerts",
            "webhook_has_a_bearer_token": True,
        },
        "apps": {
            "shop": {"host_name": "shop.acme.test", "health_url_path": "/health"},
            "my_second_app": {"host_name": "second.acme.test", "health_url_path": "/up"},
        },
    }
    settings.update(changes)
    path = directory / "outside_alarm_check_in_settings.json"
    path.write_text(json.dumps(settings), encoding="utf-8")
    return path


def run_script(settings_path, *arguments):
    return subprocess.run([sys.executable, "-I", str(SCRIPT), "--settings", str(settings_path), *arguments], capture_output=True, text=True)


def check_with_the_worker(payload_text):
    finished = subprocess.run(["node", str(CHECKER), str(template_filler.WORKER_FILE)], input=payload_text, capture_output=True, text=True)
    if finished.returncode != 0:
        raise AssertionError("node failed: " + finished.stdout + finished.stderr)
    return json.loads(finished.stdout)


class FakeWorker(http.server.BaseHTTPRequestHandler):
    received = []
    answer_status = 200

    def do_POST(self):
        body = self.rfile.read(int(self.headers.get("Content-Length", "0"))).decode("utf-8")
        FakeWorker.received.append({"path": self.path, "authorization": self.headers.get("Authorization"), "body": body})
        answer = json.dumps({"stored": True}).encode("utf-8")
        self.send_response(FakeWorker.answer_status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(answer)))
        self.end_headers()
        self.wfile.write(answer)

    def log_message(self, *arguments):
        pass


class OutsideAlarmCheckInTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not shutil.which("node"):
            raise AssertionError("node was not found on PATH. The test passes each payload through the Worker's own JavaScript (node 22.15 or newer).")
        cls.directory = tempfile.TemporaryDirectory(prefix="solo-check-in-test-")
        cls.addClassCleanup(cls.directory.cleanup)

    def new_directory(self, name):
        path = pathlib.Path(self.directory.name) / name
        path.mkdir()
        return path

    def test_every_alert_kind_makes_a_payload_that_the_worker_accepts(self):
        for kind in ("pushover", "slack", "discord", "telegram", "webhook"):
            with self.subTest(kind=kind):
                settings = sample_settings(self.new_directory(kind), kind)
                finished = run_script(settings, "--print-payload")
                self.assertEqual(finished.returncode, 0, finished.stderr)
                verdict = check_with_the_worker(finished.stdout)
                self.assertEqual(verdict, {"ok": True, "held_messages": 1, "health_checks": 2}, finished.stdout)
                payload = json.loads(finished.stdout)
                message = payload["held_messages"][0]
                self.assertEqual(message["held_message_id"], "solo01-" + kind + "-silent")
                self.assertEqual(message["delay_seconds"], 300)
                # An alert, a test and a recovery, each one HTTPS request the Worker sends as it is.
                for request_name in ("request", "test_request", "recovery_request"):
                    self.assertTrue(message[request_name]["url"].startswith("https://"), kind + " " + request_name)
                self.assertEqual({check["check_id"] for check in payload["health_checks"]}, {"shop-health", "my-second-app-health"})
                for check in payload["health_checks"]:
                    self.assertEqual(check["failures_before_alert"], 2)
                    self.assertIn("recovery_request", check)

    def test_each_kind_sends_to_its_own_service(self):
        expected_urls = {
            "pushover": "https://api.pushover.net/1/messages.json",
            "slack": "https://hooks.slack.com/services/T000/B000/sample",
            "discord": "https://discord.com/api/webhooks/123456789012345678/sample",
            "telegram": "https://api.telegram.org/bot123456789:sample-bot-token/sendMessage",
            "webhook": "https://hooks.acme.test/alerts",
        }
        for kind, url in expected_urls.items():
            with self.subTest(kind=kind):
                finished = run_script(sample_settings(self.new_directory("url_" + kind), kind), "--print-payload")
                payload = json.loads(finished.stdout)
                self.assertEqual(payload["held_messages"][0]["request"]["url"], url)
                self.assertEqual(payload["held_messages"][0]["request"]["method"], "POST")

    def test_a_webhook_with_a_bearer_token_sends_it_in_the_authorization_header(self):
        finished = run_script(sample_settings(self.new_directory("bearer"), "webhook"), "--print-payload")
        headers = json.loads(finished.stdout)["held_messages"][0]["request"]["headers"]
        self.assertEqual(headers["Authorization"], "Bearer sample-bearer-token")
        without = sample_settings(self.new_directory("no_bearer"), "webhook")
        settings = json.loads(without.read_text(encoding="utf-8"))
        settings["alert_destination"]["webhook_has_a_bearer_token"] = False
        without.write_text(json.dumps(settings), encoding="utf-8")
        headers = json.loads(run_script(without, "--print-payload").stdout)["held_messages"][0]["request"]["headers"]
        self.assertNotIn("Authorization", headers)

    def test_pushover_emergency_priority_repeats_and_a_test_does_not(self):
        directory = self.new_directory("emergency")
        settings = sample_settings(directory, "pushover")
        data = json.loads(settings.read_text(encoding="utf-8"))
        data["alert_destination"]["pushover_priority"] = 2
        settings.write_text(json.dumps(data), encoding="utf-8")
        finished = run_script(settings, "--print-payload")
        self.assertEqual(check_with_the_worker(finished.stdout)["ok"], True)
        message = json.loads(finished.stdout)["held_messages"][0]
        self.assertIn("priority=2", message["request"]["body"])
        self.assertIn("retry=60", message["request"]["body"])
        self.assertIn("priority=0", message["test_request"]["body"])
        self.assertIn("priority=0", message["recovery_request"]["body"])
        self.assertNotIn("retry=", message["test_request"]["body"])

    def test_health_checks_start_only_when_the_public_site_checks_run(self):
        finished = run_script(sample_settings(self.new_directory("before_go_live"), "pushover", public_site_checks_run=False), "--print-payload")
        payload = json.loads(finished.stdout)
        self.assertNotIn("health_checks", payload)
        self.assertEqual(check_with_the_worker(finished.stdout), {"ok": True, "held_messages": 1, "health_checks": 0})

    def test_alerts_off_checks_in_with_no_held_message(self):
        finished = run_script(sample_settings(self.new_directory("alerts_off"), "pushover", enabled=False), "--print-payload")
        self.assertEqual(json.loads(finished.stdout), {"held_messages": []})
        self.assertEqual(check_with_the_worker(finished.stdout), {"ok": True, "held_messages": 0, "health_checks": 0})

    def test_email_holds_no_message_because_the_worker_sends_only_https_requests(self):
        finished = run_script(sample_settings(self.new_directory("email"), "email"), "--print-payload")
        self.assertEqual(json.loads(finished.stdout), {"held_messages": []})

    def test_a_delay_the_worker_would_refuse_is_refused_here_with_the_reason(self):
        finished = run_script(sample_settings(self.new_directory("short_delay"), "pushover", outside_alarm_delay_seconds=120), "--print-payload")
        self.assertEqual(finished.returncode, 1)
        self.assertIn("outside_alarm.delay_seconds is 120", finished.stderr)
        self.assertIn("180", finished.stderr)

    def test_a_missing_secret_file_is_named(self):
        directory = self.new_directory("missing_secret")
        settings = sample_settings(directory, "slack")
        (directory / "mmwatchdog_secrets" / "slack-webhook-url").unlink()
        finished = run_script(settings, "--print-payload")
        self.assertEqual(finished.returncode, 1)
        self.assertIn("slack-webhook-url", finished.stderr)

    def test_the_worker_does_refuse_a_payload_that_breaks_its_rules(self):
        # The test above is only as good as the checker: a payload with a delay of 60 seconds must be refused.
        finished = run_script(sample_settings(self.new_directory("bad_payload"), "slack"), "--print-payload")
        payload = json.loads(finished.stdout)
        payload["held_messages"][0]["delay_seconds"] = 60
        verdict = check_with_the_worker(json.dumps(payload))
        self.assertFalse(verdict["ok"])
        self.assertIn("delay_seconds", verdict["field"])

    def test_the_check_in_is_posted_with_the_bearer_token(self):
        server = http.server.HTTPServer(("127.0.0.1", 0), FakeWorker)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        worker_url = "http://127.0.0.1:" + str(server.server_address[1]) + "/"
        settings = sample_settings(self.new_directory("post"), "pushover", worker_url=worker_url)
        FakeWorker.received.clear()
        FakeWorker.answer_status = 200
        finished = run_script(settings)
        self.assertEqual(finished.returncode, 0, finished.stderr)
        self.assertEqual(len(FakeWorker.received), 1)
        received = FakeWorker.received[0]
        self.assertEqual(received["path"], "/check-in")
        self.assertEqual(received["authorization"], "Bearer sample-check-in-token")
        self.assertEqual(check_with_the_worker(received["body"]), {"ok": True, "held_messages": 1, "health_checks": 2})
        # --send-test refreshes /check-in first so the Worker holds the latest
        # alert settings, then posts /send-test.
        FakeWorker.received.clear()
        self.assertEqual(run_script(settings, "--send-test").returncode, 0)
        self.assertEqual([item["path"] for item in FakeWorker.received], ["/check-in", "/send-test"])
        self.assertEqual(FakeWorker.received[0]["authorization"], "Bearer sample-check-in-token")
        self.assertEqual(FakeWorker.received[1]["authorization"], "Bearer sample-check-in-token")
        # A Worker that refuses the check-in fails the service, so MMWatchdog sees it.
        FakeWorker.answer_status = 401
        refused = run_script(settings)
        self.assertEqual(refused.returncode, 1)
        self.assertIn("HTTP 401", refused.stderr)

    def test_an_unreachable_worker_fails_with_the_address(self):
        settings = sample_settings(self.new_directory("unreachable"), "pushover", worker_url="http://127.0.0.1:9")
        finished = run_script(settings)
        self.assertEqual(finished.returncode, 1)
        self.assertIn("http://127.0.0.1:9", finished.stderr)

    def test_the_settings_file_template_has_the_keys_the_script_reads(self):
        template = SETTINGS_TEMPLATE.read_text(encoding="utf-8")
        written_by_the_template = set(re.findall(r'"([a-z_]+)"', template))
        sample = json.loads(sample_settings(self.new_directory("keys"), "pushover").read_text(encoding="utf-8"))
        read_by_the_script = set(sample) | set(sample["alert_destination"]) | {"host_name", "health_url_path"}
        self.assertEqual(written_by_the_template, read_by_the_script)


if __name__ == "__main__":
    unittest.main()
