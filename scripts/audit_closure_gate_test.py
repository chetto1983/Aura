from __future__ import annotations

import json
import pathlib
import tempfile
import unittest

import audit_closure_gate


CANDIDATE = "a" * 40
HEADER = (
    "| ID | State | Current evidence | Closure condition | Owner |\n"
    "|---|---|---|---|---|\n"
)


class AuditClosureGateTest(unittest.TestCase):
    def make_fixture(
        self,
    ) -> tuple[pathlib.Path, pathlib.Path, tempfile.TemporaryDirectory[str]]:
        temporary = tempfile.TemporaryDirectory()
        root = pathlib.Path(temporary.name)
        register = root / "README.md"
        output = root / "audit-closure-report.json"
        return register, output, temporary

    def run_fixture(
        self,
        register: pathlib.Path,
        output: pathlib.Path,
        rows: str,
        required_ids: set[str],
    ) -> dict[str, object]:
        register.write_text(HEADER + rows, encoding="utf-8")
        return audit_closure_gate.run_gate(
            register=register,
            output=output,
            candidate=CANDIDATE,
            required_ids=frozenset(required_ids),
        )

    def test_current_open_register_passes_disclosure_gate(self) -> None:
        register, output, temporary = self.make_fixture()
        self.addCleanup(temporary.cleanup)
        report = self.run_fixture(
            register,
            output,
            "| EXT-001 | external_blocked | account absent | connect fixture account | operator |\n"
            "| REL-009 | open | no rollback baseline | promote distinct image | release owner |\n",
            {"EXT-001", "REL-009"},
        )
        self.assertTrue(report["passed"])
        self.assertFalse(report["release_ready"])
        self.assertEqual(report["open_total"], 2)
        self.assertEqual(report["counts"]["external_blocked"], 1)  # type: ignore[index]
        self.assertEqual(report["counts"]["open"], 1)  # type: ignore[index]
        self.assertNotIn("score", report)
        self.assertEqual(json.loads(output.read_text())["candidate_commit"], CANDIDATE)

    def test_missing_duplicate_or_unknown_current_ids_fail(self) -> None:
        register, output, temporary = self.make_fixture()
        self.addCleanup(temporary.cleanup)
        cases = {
            "missing": "| EXT-001 | external_blocked | x | y | z |\n",
            "duplicate": (
                "| EXT-001 | external_blocked | x | y | z |\n"
                "| EXT-001 | external_blocked | x | y | z |\n"
                "| REL-009 | open | x | y | z |\n"
            ),
            "unknown": (
                "| EXT-001 | external_blocked | x | y | z |\n"
                "| REL-009 | open | x | y | z |\n"
                "| UX-999 | open | x | y | z |\n"
            ),
        }
        for expected, rows in cases.items():
            with self.subTest(expected):
                register.write_text(HEADER + rows, encoding="utf-8")
                with self.assertRaisesRegex(audit_closure_gate.GateError, expected):
                    audit_closure_gate.run_gate(
                        register=register,
                        output=output,
                        candidate=CANDIDATE,
                        required_ids=frozenset({"EXT-001", "REL-009"}),
                    )

    def test_blank_fields_or_terminal_state_fail(self) -> None:
        register, output, temporary = self.make_fixture()
        self.addCleanup(temporary.cleanup)
        cases = (
            ("evidence", "| EXT-001 | external_blocked |  | y | z |\n"),
            ("closure condition", "| EXT-001 | external_blocked | x |  | z |\n"),
            ("owner", "| EXT-001 | external_blocked | x | y |  |\n"),
            ("state", "| EXT-001 | closed | x | y | z |\n"),
        )
        for expected, row in cases:
            with self.subTest(expected):
                register.write_text(HEADER + row, encoding="utf-8")
                with self.assertRaisesRegex(audit_closure_gate.GateError, expected):
                    audit_closure_gate.run_gate(
                        register=register,
                        output=output,
                        candidate=CANDIDATE,
                        required_ids=frozenset({"EXT-001"}),
                    )

    def test_empty_register_is_release_ready_when_no_ids_are_required(self) -> None:
        register, output, temporary = self.make_fixture()
        self.addCleanup(temporary.cleanup)
        report = self.run_fixture(register, output, "", set())
        self.assertTrue(report["release_ready"])
        self.assertEqual(report["open_total"], 0)
        self.assertEqual(report["issues"], [])

    def test_historical_audit_paths_fail(self) -> None:
        audit_closure_gate.validate_tracked_audit_paths(["docs/audit/README.md"])
        with self.assertRaisesRegex(audit_closure_gate.GateError, "historical audit paths"):
            audit_closure_gate.validate_tracked_audit_paths(
                ["docs/audit/README.md", "docs/audit/old-plan.md"]
            )

    def test_missing_header_or_invalid_id_fail(self) -> None:
        register, output, temporary = self.make_fixture()
        self.addCleanup(temporary.cleanup)
        register.write_text("# no current issue table\n", encoding="utf-8")
        with self.assertRaisesRegex(audit_closure_gate.GateError, "header"):
            audit_closure_gate.run_gate(
                register=register,
                output=output,
                candidate=CANDIDATE,
                required_ids=frozenset(),
            )

        register.write_text(
            HEADER + "| BAD-001 | open | x | y | z |\n",
            encoding="utf-8",
        )
        with self.assertRaisesRegex(audit_closure_gate.GateError, "invalid ID"):
            audit_closure_gate.run_gate(
                register=register,
                output=output,
                candidate=CANDIDATE,
                required_ids=frozenset(),
            )


if __name__ == "__main__":
    unittest.main()
