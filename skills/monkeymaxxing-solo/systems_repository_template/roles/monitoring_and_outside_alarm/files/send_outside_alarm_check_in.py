#!/usr/bin/env python3
"""solo01's check-in with the Cloudflare Worker outside alarm.

Managed by MMX from roles/monitoring_and_outside_alarm/files/send_outside_alarm_check_in.py
in the systems repository. solo_outside_alarm_check_in.timer runs it every
minute and at boot:

    send_outside_alarm_check_in.py                  # check in
    send_outside_alarm_check_in.py --print-payload  # print the check-in's JSON, send nothing
    send_outside_alarm_check_in.py --send-test      # ask the Worker to send each held message's test now
    send_outside_alarm_check_in.py --settings FILE  # use another settings file

Each check-in tells the Worker "solo01 is alive" and hands it ready-made
HTTPS requests (the "held messages"): the alert to send when the check-ins
stop for delay_seconds, and the matching recovery message. The Worker, which
runs outside solo01, sends them when solo01 is frozen, out of memory or
offline. The check-in also lists the app health URLs the Worker asks from
Cloudflare's network once public_site_checks_run is true, each with its
own alert and recovery request.

The alert destination (Pushover, Slack, Discord, Telegram or a webhook) and
its secret files come from the settings file. Secrets are read from files
that only root can read, and never printed: --print-payload prints the
payload that the Worker receives, which does hold the destination's secrets
inside its requests, so it is for tests with sample secrets.

The payload follows the check-in format of worker/solo_outside_alarm_worker.js
in the Monkeymaxxing Solo repository: held_messages and health_checks.
Only the Python standard library is used.
"""

import argparse
import json
import re
import sys
import urllib.error
import urllib.parse
import urllib.request

DEFAULT_SETTINGS_FILE = "/etc/solo/outside_alarm_check_in_settings.json"
# The Worker's smallest delay: solo01 checks in every minute and a reboot
# leaves at most about 2.5 minutes without a check-in.
MINIMUM_DELAY_SECONDS = 180
ANSWER_TIME_LIMIT_SECONDS = 30
# One request carries one held message of a few hundred bytes; the Worker
# refuses a check-in above 1 MiB, so a settings mistake is caught here.
MAXIMUM_PAYLOAD_BYTES = 1024 * 1024
KINDS_THE_WORKER_CAN_SEND = ("pushover", "slack", "discord", "telegram", "webhook")


class CheckInRefused(Exception):
    """The settings or a secret file cannot make a valid check-in; the message says what to fix."""


def read_secret(directory, name):
    return read_secret_file(directory.rstrip("/") + "/" + name)


def read_secret_file(path):
    try:
        with open(path, encoding="utf-8") as secret_file:
            value = secret_file.read().strip()
    except OSError as error:
        raise CheckInRefused("cannot read the secret file " + path + ": " + str(error) + ". Run solo_server again so MMX writes it.")
    if value == "":
        raise CheckInRefused("the secret file " + path + " is empty")
    return value


def request(method, url, headers, body):
    return {"method": method, "url": url, "headers": headers, "body": body}


def pushover_request(destination, secrets_directory, title, message, sound, priority):
    fields = {
        "token": read_secret(secrets_directory, "pushover-application-token"),
        "user": read_secret(secrets_directory, "pushover-user-or-group-key"),
        "title": title,
        "message": message,
        "sound": sound,
        "priority": str(priority),
    }
    if priority == 2:
        # Pushover's emergency priority repeats until somebody acknowledges it.
        fields["retry"] = "60"
        fields["expire"] = "3600"
    return request(
        "POST",
        "https://api.pushover.net/1/messages.json",
        {"Content-Type": "application/x-www-form-urlencoded"},
        urllib.parse.urlencode(fields),
    )


def json_request(url, fields, extra_headers=None):
    headers = {"Content-Type": "application/json"}
    headers.update(extra_headers or {})
    return request("POST", url, headers, json.dumps(fields))


def make_request(settings, title, message, urgent):
    """The ready-made HTTPS request that tells a person title and message, for the settings' alert destination."""
    destination = settings["alert_destination"]
    secrets_directory = settings["secrets_directory"]
    kind = destination["kind"]
    text = title + ": " + message
    if kind == "pushover":
        # A test or a recovery is never urgent: it makes no emergency repeat.
        priority = destination["pushover_priority"] if urgent else 0
        return pushover_request(destination, secrets_directory, title, message, destination["pushover_sound"], priority)
    if kind == "slack":
        return json_request(read_secret(secrets_directory, "slack-webhook-url"), {"text": text})
    if kind == "discord":
        return json_request(read_secret(secrets_directory, "discord-webhook-url"), {"content": text})
    if kind == "telegram":
        token = read_secret(secrets_directory, "telegram-bot-token")
        return json_request("https://api.telegram.org/bot" + token + "/sendMessage", {"chat_id": destination["telegram_chat_id"], "text": text})
    if kind == "webhook":
        headers = {}
        if destination["webhook_has_a_bearer_token"]:
            headers["Authorization"] = "Bearer " + read_secret(secrets_directory, "alert-webhook-bearer-token")
        return json_request(destination["webhook_url"], {"source": settings["server_name"] + " outside alarm", "title": title, "message": message, "urgent": urgent}, headers)
    raise CheckInRefused("the alert destination kind " + repr(kind) + " cannot be sent by the outside alarm. Use one of " + ", ".join(KINDS_THE_WORKER_CAN_SEND) + ".")


def lowercase_id(text, suffix):
    """A Worker id: 1 to 64 lowercase letters, digits and hyphens."""
    cleaned = re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-")
    return (cleaned[: 63 - len(suffix)].strip("-") + "-" + suffix) if cleaned else suffix


def validate_settings(settings):
    if settings["outside_alarm_delay_seconds"] < MINIMUM_DELAY_SECONDS:
        raise CheckInRefused("outside_alarm.delay_seconds is " + str(settings["outside_alarm_delay_seconds"]) + ". Use " + str(MINIMUM_DELAY_SECONDS) + " or more: solo01 checks in every minute and a reboot leaves at most about 2.5 minutes without a check-in.")
    if not 1 <= settings["health_check_failures_before_alert"] <= 10:
        raise CheckInRefused("outside_alarm.health_check_failures_before_alert is " + str(settings["health_check_failures_before_alert"]) + ". Use a whole number from 1 to 10.")


def build_payload(settings):
    """The JSON object that POST /check-in takes: held_messages and health_checks."""
    validate_settings(settings)
    server = settings["server_name"]
    destination = settings["alert_destination"]
    held_messages = []
    health_checks = []
    if destination["enabled"] and destination["kind"] in KINDS_THE_WORKER_CAN_SEND:
        held_messages.append({
            "held_message_id": lowercase_id(server + "-" + destination["kind"], "silent"),
            "delay_seconds": settings["outside_alarm_delay_seconds"],
            "request": make_request(settings, server + " stopped checking in", server + " has not checked in for " + str(settings["outside_alarm_delay_seconds"] // 60) + " minutes or more. It may be frozen, out of memory, or offline.", True),
            "test_request": make_request(settings, server + " outside alarm test", "This is a test of the outside alarm. Nothing is wrong.", False),
            "recovery_request": make_request(settings, server + " is checking in again", server + " is back. The outside alarm saw it check in.", False),
        })
        if settings["public_site_checks_run"]:
            for app_name, app in sorted(settings["apps"].items()):
                url = "https://" + app["host_name"] + app["health_url_path"]
                health_checks.append({
                    "check_id": lowercase_id(app_name, "health"),
                    "url": url,
                    "timeout_ms": 10000,
                    "failures_before_alert": settings["health_check_failures_before_alert"],
                    "alert_request": make_request(settings, app["host_name"] + " is down", "The health page " + url + " failed " + str(settings["health_check_failures_before_alert"]) + " checks in a row, asked from outside the server.", True),
                    "recovery_request": make_request(settings, app["host_name"] + " is back", "The health page " + url + " answers again.", False),
                })
    payload = {"held_messages": held_messages}
    if health_checks:
        payload["health_checks"] = health_checks
    body = json.dumps(payload)
    if len(body.encode("utf-8")) > MAXIMUM_PAYLOAD_BYTES:
        raise CheckInRefused("the check-in is larger than the Worker's limit of " + str(MAXIMUM_PAYLOAD_BYTES) + " bytes")
    return payload


def post_to_the_worker(settings, path, body_text):
    token = read_secret_file(settings["check_in_token_file"])
    http_request = urllib.request.Request(
        settings["worker_url"].rstrip("/") + path,
        data=body_text.encode("utf-8"),
        method="POST",
        headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(http_request, timeout=ANSWER_TIME_LIMIT_SECONDS) as answer:
            return answer.status, answer.read().decode("utf-8", "replace")
    except urllib.error.HTTPError as error:
        return error.code, error.read().decode("utf-8", "replace")
    except (urllib.error.URLError, TimeoutError, OSError) as error:
        raise CheckInRefused("could not reach the Worker at " + settings["worker_url"] + ": " + str(error))


def main(arguments=None):
    parser = argparse.ArgumentParser(description="solo01's check-in with the Cloudflare Worker outside alarm.")
    parser.add_argument("--settings", default=DEFAULT_SETTINGS_FILE, help="the settings file, JSON (default: " + DEFAULT_SETTINGS_FILE + ")")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--print-payload", action="store_true", help="print the check-in's JSON and send nothing")
    mode.add_argument("--send-test", action="store_true", help="ask the Worker to send each held message's test request now")
    given = parser.parse_args(arguments)
    try:
        with open(given.settings, encoding="utf-8") as settings_file:
            settings = json.load(settings_file)
        if given.print_payload:
            print(json.dumps(build_payload(settings), indent=2, sort_keys=True))
            return 0
        if settings["alert_destination"]["enabled"] and settings["alert_destination"]["kind"] not in KINDS_THE_WORKER_CAN_SEND:
            print("send_outside_alarm_check_in: the alert destination kind " + settings["alert_destination"]["kind"] + " reaches you from MMWatchdog on solo01 only. The outside alarm sends HTTPS requests, so it holds no message and only records that solo01 is alive.", file=sys.stderr)
        if given.send_test:
            # Refresh the Worker's held messages first so --send-test always
            # tests the current alert settings even right after a config change.
            check_in_status, check_in_answer = post_to_the_worker(settings, "/check-in", json.dumps(build_payload(settings)))
            if check_in_status != 200:
                print(check_in_answer)
                print("send_outside_alarm_check_in: the Worker answered HTTP " + str(check_in_status) + " to /check-in", file=sys.stderr)
                return 1
            status, answer = post_to_the_worker(settings, "/send-test", "")
        else:
            status, answer = post_to_the_worker(settings, "/check-in", json.dumps(build_payload(settings)))
    except (OSError, ValueError, KeyError, CheckInRefused) as error:
        print("send_outside_alarm_check_in: " + str(error), file=sys.stderr)
        return 1
    print(answer)
    if status != 200:
        print("send_outside_alarm_check_in: the Worker answered HTTP " + str(status), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
