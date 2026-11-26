"""Writes into the test ledger's directory for the Python harnesses.

The counterpart of scripts/go_test_ledger.sh and scripts/run_script_tests.sh for code that runs or
records tests itself (agent_memory_eval.py, capability_eval.py, unittest_ledger.py): the same
directory, the same refusal under CI (docs/superpowers/specs/2026-11-25-test-ledger-design.md §4.2).
"""

import os
import pathlib
import re

_FILE_NAME = re.compile(r"[A-Za-z0-9._-]+")


def append(file_name: str, text: str) -> None:
    """Appends text to $AURA_TEST_LEDGER_DIR/<file_name>; outside CI an unset directory records nothing."""
    if not _FILE_NAME.fullmatch(file_name):
        raise ValueError(f"test ledger file {file_name!r} is not letters, digits, '.', '_' or '-'")
    directory = os.environ.get("AURA_TEST_LEDGER_DIR", "")
    if not directory:
        if os.environ.get("CI"):
            raise RuntimeError("AURA_TEST_LEDGER_DIR is unset under CI, so this run would never reach the test ledger")
        return
    ledger = pathlib.Path(directory)
    ledger.mkdir(parents=True, exist_ok=True)
    with (ledger / file_name).open("a", encoding="utf-8") as stream:
        stream.write(text)
