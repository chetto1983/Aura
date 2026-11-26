"""Runs the scripts/ Python self-tests and records each test's outcome for the test ledger.

    PYTHONPATH=scripts python3 scripts/unittest_ledger.py

It is `python3 -m unittest discover -s scripts -t scripts -p '*_test.py'` plus one line per test in
$AURA_TEST_LEDGER_DIR/python-tests.txt, "<test id> pass|fail|skip"
(docs/superpowers/specs/2026-11-25-test-ledger-design.md §4.4). An expected failure is a pass; a test
whose subtest fails is one fail line.
"""

import sys
import unittest
from typing import TextIO

import test_ledger


class _Recording(unittest.TextTestResult):
    def __init__(self, *args, **kwargs) -> None:
        super().__init__(*args, **kwargs)
        self.outcomes: dict[str, str] = {}

    def _note(self, test: unittest.TestCase, outcome: str) -> None:
        if self.outcomes.get(test.id()) != "fail":
            self.outcomes[test.id()] = outcome

    def addSuccess(self, test: unittest.TestCase) -> None:
        super().addSuccess(test)
        self._note(test, "pass")

    def addFailure(self, test: unittest.TestCase, err) -> None:
        super().addFailure(test, err)
        self._note(test, "fail")

    def addError(self, test: unittest.TestCase, err) -> None:
        super().addError(test, err)
        self._note(test, "fail")

    def addSkip(self, test: unittest.TestCase, reason: str) -> None:
        super().addSkip(test, reason)
        self._note(test, "skip")

    def addExpectedFailure(self, test: unittest.TestCase, err) -> None:
        super().addExpectedFailure(test, err)
        self._note(test, "pass")

    def addUnexpectedSuccess(self, test: unittest.TestCase) -> None:
        super().addUnexpectedSuccess(test)
        self._note(test, "fail")

    def addSubTest(self, test: unittest.TestCase, subtest: unittest.TestCase, err) -> None:
        super().addSubTest(test, subtest, err)
        if err is not None:
            self._note(test, "fail")


def run(suite: unittest.TestSuite, stream: TextIO) -> bool:
    """Runs suite, appends its outcomes to the ledger, and reports whether it passed."""
    test_ledger.append("python-tests.txt", "")  # refuses an unset directory under CI before any test runs
    result = unittest.TextTestRunner(stream=stream, resultclass=_Recording).run(suite)
    assert isinstance(result, _Recording)
    test_ledger.append("python-tests.txt", "".join(f"{test} {outcome}\n" for test, outcome in result.outcomes.items()))
    return result.wasSuccessful()


def main() -> int:
    suite = unittest.defaultTestLoader.discover("scripts", pattern="*_test.py", top_level_dir="scripts")
    return 0 if run(suite, sys.stderr) else 1


if __name__ == "__main__":
    sys.exit(main())
