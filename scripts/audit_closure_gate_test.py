from __future__ import annotations

import json
import pathlib
import tempfile
import unittest

import audit_closure_gate


CANDIDATE = "a" * 40


class AuditClosureGateTest(unittest.TestCase):
    def make_fixture(self) -> tuple[pathlib.Path, pathlib.Path, pathlib.Path, tempfile.TemporaryDirectory[str]]:
        temporary = tempfile.TemporaryDirectory()
        root = pathlib.Path(temporary.name)
        source = root / "register.md"
        source.write_text("# Register\n\n## F-001 one\n## F-002 two\n", encoding="utf-8")
        ledger = root / "ledger.md"
        output = root / "audit-closure-report.json"
        return source, ledger, output, temporary

    def test_terminal_states_pass_and_external_blockers_are_excluded(self) -> None:
        source, ledger, output, temporary = self.make_fixture()
        self.addCleanup(temporary.cleanup)
        ledger.write_text(
            "| ID | State | Evidence | Verification |\n"
            "|---|---|---|---|\n"
            "| F-001 | closed | abc123 | focused test |\n"
            "| F-002 | external_blocked | provider account absent | direct call failed honestly |\n",
            encoding="utf-8",
        )
        report = audit_closure_gate.run_gate(
            sources=[source],
            ledger=ledger,
            output=output,
            candidate=CANDIDATE,
            minimum_score=9.8,
        )
        self.assertTrue(report["passed"])
        self.assertEqual(report["score"], 10.0)
        self.assertEqual(report["counts"]["external_blocked"], 1)
        self.assertEqual(report["external_blockers"], ["F-002"])
        self.assertEqual(json.loads(output.read_text())["candidate_commit"], CANDIDATE)

    def test_missing_duplicate_unknown_or_open_findings_fail(self) -> None:
        source, ledger, output, temporary = self.make_fixture()
        self.addCleanup(temporary.cleanup)
        cases = {
            "missing": "| F-001 | closed | x | y |\n",
            "duplicate": (
                "| F-001 | closed | x | y |\n"
                "| F-001 | closed | x | y |\n"
                "| F-002 | closed | x | y |\n"
            ),
            "unknown": (
                "| F-001 | closed | x | y |\n"
                "| F-002 | closed | x | y |\n"
                "| F-003 | closed | x | y |\n"
            ),
            "open": (
                "| F-001 | closed | x | y |\n"
                "| F-002 | open | x | y |\n"
            ),
        }
        for expected, rows in cases.items():
            with self.subTest(expected):
                ledger.write_text(
                    "| ID | State | Evidence | Verification |\n"
                    "|---|---|---|---|\n" + rows,
                    encoding="utf-8",
                )
                with self.assertRaisesRegex(audit_closure_gate.GateError, expected):
                    audit_closure_gate.run_gate(
                        sources=[source],
                        ledger=ledger,
                        output=output,
                        candidate=CANDIDATE,
                        minimum_score=9.8,
                    )

    def test_blank_evidence_or_invalid_state_fail(self) -> None:
        source, ledger, output, temporary = self.make_fixture()
        self.addCleanup(temporary.cleanup)
        for expected, row in (
            ("evidence", "| F-001 | closed |  | y |\n"),
            ("state", "| F-001 | waived | x | y |\n"),
        ):
            with self.subTest(expected):
                ledger.write_text(
                    "| ID | State | Evidence | Verification |\n"
                    "|---|---|---|---|\n"
                    f"{row}| F-002 | closed | x | y |\n",
                    encoding="utf-8",
                )
                with self.assertRaisesRegex(audit_closure_gate.GateError, expected):
                    audit_closure_gate.run_gate(
                        sources=[source],
                        ledger=ledger,
                        output=output,
                        candidate=CANDIDATE,
                        minimum_score=9.8,
                    )


if __name__ == "__main__":
    unittest.main()
