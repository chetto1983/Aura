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

Usage: briefing_baseline.py BUNDLE.json
"""

from __future__ import annotations

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


def main(argv: list[str]) -> int:
    if len(argv) != 2:
        print(__doc__.strip().splitlines()[-1], file=sys.stderr)
        return 2
    with open(argv[1], encoding="utf-8") as f:
        sys.stdout.write(render(json.load(f)))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
