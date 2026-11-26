"""Records the `go test -json` stream a Python harness already collects for the test ledger.

The counterpart of scripts/go_test_ledger.sh for scripts that run go test themselves
(agent_memory_eval.py, capability_eval.py): the same file, the same refusal under CI
(docs/superpowers/specs/2026-11-25-test-ledger-design.md §4.2).
"""

import os
import pathlib
import re

_LABEL = re.compile(r"[A-Za-z0-9._-]+")


def record(label: str, go_test_json: str) -> None:
    """Appends go_test_json to $AURA_TEST_LEDGER_DIR/<label>.jsonl."""
    if not _LABEL.fullmatch(label):
        raise ValueError(f"test ledger label {label!r} is not letters, digits, '.', '_' or '-'")
    directory = os.environ.get("AURA_TEST_LEDGER_DIR", "")
    if not directory:
        if os.environ.get("CI"):
            raise RuntimeError("AURA_TEST_LEDGER_DIR is unset under CI, so this run would never reach the test ledger")
        return
    ledger = pathlib.Path(directory)
    ledger.mkdir(parents=True, exist_ok=True)
    with (ledger / f"{label}.jsonl").open("a", encoding="utf-8") as stream:
        stream.write(go_test_json)
