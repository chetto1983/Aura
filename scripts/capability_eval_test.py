import json
import unittest

import capability_eval


class CapabilityEvalTests(unittest.TestCase):
    def test_manifest_has_both_polarities_per_class(self) -> None:
        capability_eval.validate_manifest(capability_eval.SCENARIOS)
        self.assertGreaterEqual(len(capability_eval.SCENARIOS), 16)

    def test_parser_records_terminal_test_events(self) -> None:
        events = [
            json.dumps({"Action": "run", "Test": "TestOne"}),
            json.dumps({"Action": "pass", "Test": "TestOne", "Elapsed": 0.02}),
            json.dumps({"Action": "skip", "Test": "TestTwo", "Elapsed": 0}),
            "not-json",
        ]
        self.assertEqual(
            capability_eval.parse_test_results(events),
            {
                "TestOne": {"status": "pass", "elapsed_seconds": 0.02},
                "TestTwo": {"status": "skip", "elapsed_seconds": 0.0},
            },
        )


if __name__ == "__main__":
    unittest.main()
