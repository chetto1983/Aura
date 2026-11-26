import os
import pathlib
import tempfile
import unittest
from unittest import mock

import go_test_ledger

EVENTS = '{"Action":"run","Test":"TestOne"}\n{"Action":"pass","Test":"TestOne"}\n'


class RecordTests(unittest.TestCase):
    def test_appends_the_stream_under_its_label(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            ledger = pathlib.Path(raw) / "ledger"
            with mock.patch.dict(os.environ, {"AURA_TEST_LEDGER_DIR": str(ledger)}):
                go_test_ledger.record("eval", EVENTS)
                go_test_ledger.record("eval", EVENTS)
            self.assertEqual((ledger / "eval.jsonl").read_text(encoding="utf-8"), EVENTS * 2)

    def test_unset_under_ci_is_refused(self) -> None:
        with (
            mock.patch.dict(os.environ, {"AURA_TEST_LEDGER_DIR": "", "CI": "true"}),
            self.assertRaisesRegex(RuntimeError, "AURA_TEST_LEDGER_DIR"),
        ):
            go_test_ledger.record("eval", EVENTS)

    def test_unset_outside_ci_records_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            with mock.patch.dict(os.environ, {"AURA_TEST_LEDGER_DIR": "", "CI": ""}):
                go_test_ledger.record("eval", EVENTS)
            self.assertEqual(list(pathlib.Path(raw).iterdir()), [])

    def test_a_label_is_a_file_name(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            with mock.patch.dict(os.environ, {"AURA_TEST_LEDGER_DIR": raw}):
                for label in ("", "../escape", "a/b"):
                    with self.assertRaises(ValueError):
                        go_test_ledger.record(label, EVENTS)
            self.assertEqual(list(pathlib.Path(raw).iterdir()), [])


if __name__ == "__main__":
    unittest.main()
