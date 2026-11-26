import io
import os
import pathlib
import tempfile
import unittest
from unittest import mock

import unittest_ledger


def _fixture_suite() -> unittest.TestSuite:
    # Built here, not at module level, so the real discovery never collects the failing cases.
    class Fixture(unittest.TestCase):
        def test_passes(self) -> None:
            pass

        def test_fails(self) -> None:
            self.fail("fixture failure")

        def test_errors(self) -> None:
            raise RuntimeError("fixture error")

        @unittest.skip("fixture skip")
        def test_skips(self) -> None:
            pass

        def test_one_subtest_fails(self) -> None:
            for value in (1, 2):
                with self.subTest(value=value):
                    self.assertEqual(value, 1)

        @unittest.expectedFailure
        def test_expected_failure(self) -> None:
            self.fail("known")

    return unittest.defaultTestLoader.loadTestsFromTestCase(Fixture)


class RunTests(unittest.TestCase):
    def test_records_one_outcome_per_test(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            with mock.patch.dict(os.environ, {"AURA_TEST_LEDGER_DIR": raw}):
                ok = unittest_ledger.run(_fixture_suite(), io.StringIO())
            lines = (pathlib.Path(raw) / "python-tests.txt").read_text(encoding="utf-8").splitlines()
        self.assertFalse(ok)
        outcomes = {line.rsplit(" ", 1)[0].rsplit(".", 1)[1]: line.rsplit(" ", 1)[1] for line in lines}
        self.assertEqual(
            outcomes,
            {
                "test_errors": "fail",
                "test_expected_failure": "pass",
                "test_fails": "fail",
                "test_one_subtest_fails": "fail",
                "test_passes": "pass",
                "test_skips": "skip",
            },
        )
        self.assertEqual(len(lines), 6, "a test whose subtests fail is one line, not one per subtest")

    def test_a_passing_suite_is_successful(self) -> None:
        suite = unittest.TestSuite(t for t in _fixture_suite() if t.id().endswith("test_passes"))
        with tempfile.TemporaryDirectory() as raw, mock.patch.dict(os.environ, {"AURA_TEST_LEDGER_DIR": raw}):
            self.assertTrue(unittest_ledger.run(suite, io.StringIO()))

    def test_unset_under_ci_is_refused_before_anything_runs(self) -> None:
        with (
            mock.patch.dict(os.environ, {"AURA_TEST_LEDGER_DIR": "", "CI": "true"}),
            self.assertRaisesRegex(RuntimeError, "AURA_TEST_LEDGER_DIR"),
        ):
            unittest_ledger.run(_fixture_suite(), io.StringIO())


if __name__ == "__main__":
    unittest.main()
