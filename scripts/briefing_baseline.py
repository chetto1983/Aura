#!/usr/bin/env python3
"""The daily briefing's dumb baseline: the same three sources, rendered by rule, no model.

docs/superpowers/specs/2026-10-09-daily-briefing-design.md, open question 1: the baseline is
rendered from the tool results the briefing run itself recorded, so both briefings read the same
inputs. Acceptance 3 shows the operator both, unlabeled, in random order.

Input, one JSON object:
  now     the instant the run read the clock, RFC 3339
  zone    the IANA zone of the run's clock hint
  board   the text `board list` returned
  events  the JSON text `calendar__calendar` get_calendar_events returned, or null when it failed
  emails  the JSON text `calendar__calendar` get_emails (unreadOnly) returned, or null

or the run's own record: a job scheduled with "keep_results": true writes
<AURA_RUN_DIR>/tmp/agent_job-<run id>/results.jsonl, one finished tool call per line, and
--results builds the bundle from it (spec, "Keeping a run's reads").

Usage: briefing_baseline.py BUNDLE.json
       briefing_baseline.py --results results.jsonl --zone Europe/Rome
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from datetime import date, datetime
from zoneinfo import ZoneInfo

CARD = re.compile(r"^- \[(?P<id>[^\]]+)\] (?P<label>.*?)(?: \((?P<meta>[^()]*)\))?$")
DUE = re.compile(r"due (\S+)")
HIDDEN = re.compile(r"^\((\d+) more card\(s\) not shown")
QUIET = "Nothing on the calendar, nothing due, no unread mail."


def instant(text: str) -> datetime:
    return datetime.fromisoformat(text.replace("Z", "+00:00"))


def due_cards(board: str, zone: ZoneInfo, today: date) -> tuple[list[tuple[datetime, str]], int]:
    """Cards due on or before today in the operator's zone, by due date, each once, and the count
    the snapshot did not show."""
    seen: dict[str, tuple[datetime, str]] = {}
    hidden = 0
    for line in board.splitlines():
        if m := HIDDEN.match(line):
            hidden = int(m.group(1))
            continue
        card = CARD.match(line)
        if not card or not card.group("meta"):
            continue
        due = DUE.search(card.group("meta"))
        if due and card.group("id") not in seen:
            at = instant(due.group(1)).astimezone(zone)
            if at.date() <= today:
                seen[card.group("id")] = (at, card.group("label"))
    return sorted(seen.values()), hidden


def card_line(at: datetime, label: str, today: date) -> str:
    late = " (overdue)" if at.date() < today else ""
    return f"- {at:%Y-%m-%d %H:%M} {label}{late}"


def clock(local: str | None) -> str:
    """HH:MM of a local ISO timestamp, as get_calendar_events returns start_local/end_local."""
    return (local or "")[11:16] or "?"


def events_lines(events: dict) -> list[str]:
    rows = sorted(events.get("events") or [], key=lambda e: e.get("start_local") or "")
    return [f"- {clock(e.get('start_local'))}-{clock(e.get('end_local'))} {e.get('subject') or '(no subject)'}" for e in rows]


def sender(value: object) -> str:
    if isinstance(value, dict):
        return str(value.get("name") or value.get("address") or "unknown sender")
    return str(value or "unknown sender")


def mail_lines(emails: dict) -> list[str]:
    rows = emails.get("emails") or []
    if not rows:
        return []
    newest = sorted(rows, key=lambda m: m.get("receivedDateTime") or "", reverse=True)[:5]
    return [f"Unread mail: {len(rows)}"] + [f"- {sender(m.get('from'))}: {m.get('subject') or '(no subject)'}" for m in newest]


def parsed(text: str | None) -> dict | None:
    return None if text is None else json.loads(text)


def render(bundle: dict) -> str:
    zone = ZoneInfo(bundle["zone"])
    today = instant(bundle["now"]).astimezone(zone).date()
    events, emails = parsed(bundle.get("events")), parsed(bundle.get("emails"))
    cards, hidden = due_cards(bundle.get("board") or "", zone, today)

    sections: list[str] = []
    if events is None:
        sections.append("Calendar unavailable.")
    elif lines := events_lines(events):
        sections.append("Events\n" + "\n".join(lines))
    if cards:
        sections.append("Cards due\n" + "\n".join(card_line(at, label, today) for at, label in cards))
    if hidden:
        sections.append(f"{hidden} more cards not shown on the board.")
    if emails is None:
        sections.append("Mail unavailable.")
    elif lines := mail_lines(emails):
        sections.append("\n".join(lines))
    return "\n\n".join(sections) + "\n" if sections else QUIET + "\n"


def kept_source(row: dict) -> str | None:
    """Which bundle field a kept call feeds, or None for a call the baseline does not read."""
    try:
        action = json.loads(row.get("arguments") or "{}").get("action")
    except json.JSONDecodeError:
        return None
    tool = row.get("tool")
    if tool == "board" and action == "list":
        return "board"
    if tool == "calendar__calendar" and action == "get_calendar_events":
        return "events"
    if tool == "calendar__calendar" and action == "get_emails":
        return "emails"
    return None


def kept_text(row: dict) -> str:
    """The whole result: the sidecar when the tool spilled it, else the result itself."""
    if path := row.get("result_sidecar_path"):
        with open(path, encoding="utf-8") as f:
            return f.read()
    return row.get("result") or ""


def bundle_from_results(lines: list[str], zone: str) -> dict:
    """The bundle of a run's last successful read of each source; a source the run did not
    read, or read only with an error, is null. `now` is the run's first finished call."""
    rows = [json.loads(line) for line in lines if line.strip()]
    # Go writes RFC 3339 with 0 to 9 fraction digits, so the strings do not sort as instants.
    ended = [r["ended_at"] for r in rows if r.get("ended_at")]
    bundle: dict = {"zone": zone, "board": None, "events": None, "emails": None,
                    "now": min(ended, key=instant, default=None)}
    for row in rows:
        source = kept_source(row)
        if source and row.get("status") == "ok":
            bundle[source] = kept_text(row)
    return bundle


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.strip().splitlines()[0])
    parser.add_argument("bundle", nargs="?", help="a bundle JSON file")
    parser.add_argument("--results", help="a job's kept results.jsonl")
    parser.add_argument("--zone", help="the operator's IANA zone, with --results")
    args = parser.parse_args(argv[1:])
    if args.results and args.zone:
        with open(args.results, encoding="utf-8") as f:
            bundle = bundle_from_results(f.readlines(), args.zone)
    elif args.bundle and not args.results:
        with open(args.bundle, encoding="utf-8") as f:
            bundle = json.load(f)
    else:
        parser.error("give a bundle, or --results with --zone")
    sys.stdout.write(render(bundle))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
