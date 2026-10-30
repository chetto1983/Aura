import json
import unittest

import briefing_baseline

BOARD = """## Due within 24 hours, overdue first (now 2026-11-02T06:30:00Z)
- [c1] Pay the invoice (in todo, high priority, due 2026-10-30T09:00:00Z)
- [c2] Call the bank (in doing, due 2026-11-02T16:00:00Z)
## To do (todo): 3 card(s)
- [c3] Plan the offsite (due 2026-11-05T09:00:00Z)
- [c4] Read the contract
## Doing (doing): 1 card(s)
## Done (done): 1 card(s)
- [c5] Ship the release (due 2026-10-28T09:00:00Z)
(2 more card(s) not shown; use search or list one column)
"""

EVENTS = {"events": [
    {"eventId": "e2", "subject": "Lunch with Anna", "start_local": "2026-11-02T13:00:00", "end_local": "2026-11-02T14:00:00"},
    {"eventId": "e1", "subject": "Standup", "start_local": "2026-11-02T09:00:00", "end_local": "2026-11-02T09:15:00"},
]}

EMAILS = {"emails": [
    {"id": f"m{i}", "subject": f"Subject {i}", "from": f"sender{i}@example.test",
     "receivedDateTime": f"2026-11-02T0{i}:00:00Z"} for i in range(7)
]}


def bundle(**over):
    b = {"now": "2026-11-02T06:30:00Z", "zone": "Europe/Rome", "board": BOARD,
         "events": json.dumps(EVENTS), "emails": json.dumps(EMAILS)}
    b.update(over)
    return b


class BriefingBaselineTests(unittest.TestCase):
    def test_events_are_in_time_order(self) -> None:
        out = briefing_baseline.render(bundle())
        self.assertLess(out.index("09:00-09:15 Standup"), out.index("13:00-14:00 Lunch with Anna"))

    def test_cards_due_on_or_before_today_by_due_date_in_any_column(self) -> None:
        out = briefing_baseline.render(bundle())
        self.assertLess(out.index("Ship the release"), out.index("Pay the invoice"))
        self.assertLess(out.index("Pay the invoice"), out.index("Call the bank"))
        self.assertNotIn("Plan the offsite", out)
        self.assertNotIn("Read the contract", out)
        self.assertNotIn("[c", out)

    def test_today_is_the_operators_day_not_utcs(self) -> None:
        # 23:30 UTC on the 1st is already the 2nd in Rome: a card due at 16:00 UTC on the 2nd is today.
        out = briefing_baseline.render(bundle(now="2026-11-01T23:30:00Z"))
        self.assertIn("Call the bank", out)
        out = briefing_baseline.render(bundle(now="2026-11-01T22:30:00Z", zone="UTC"))
        self.assertNotIn("Call the bank", out)

    def test_unread_count_and_the_five_newest_senders(self) -> None:
        out = briefing_baseline.render(bundle())
        self.assertIn("Unread mail: 7", out)
        for i in (6, 5, 4, 3, 2):
            self.assertIn(f"sender{i}@example.test", out)
        for i in (1, 0):
            self.assertNotIn(f"sender{i}@example.test", out)

    def test_the_snapshot_cap_is_named_not_guessed(self) -> None:
        self.assertIn("2 more cards not shown", briefing_baseline.render(bundle()))

    def test_a_missing_source_is_named(self) -> None:
        out = briefing_baseline.render(bundle(events=None, emails=None))
        self.assertIn("Calendar unavailable", out)
        self.assertIn("Mail unavailable", out)
        self.assertIn("Pay the invoice", out)

    def test_a_quiet_day_is_one_line(self) -> None:
        out = briefing_baseline.render(bundle(board="## To do (todo): 0 card(s)\n",
                                              events=json.dumps({"events": []}),
                                              emails=json.dumps({"emails": []})))
        self.assertEqual(out.strip(), "Nothing on the calendar, nothing due, no unread mail.")

    def test_a_sender_object_is_named_by_its_name(self) -> None:
        mail = {"emails": [{"id": "m", "subject": "Hi", "from": {"name": "Anna", "address": "anna@example.test"},
                            "receivedDateTime": "2026-11-02T05:00:00Z"}]}
        self.assertIn("Anna: Hi", briefing_baseline.render(bundle(emails=json.dumps(mail))))


if __name__ == "__main__":
    unittest.main()
