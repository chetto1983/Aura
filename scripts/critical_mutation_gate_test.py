from __future__ import annotations

import json
import os
import pathlib
import tempfile
import time
import unittest

import critical_mutation_gate


REPO = pathlib.Path(__file__).resolve().parents[1]

MEDIA_FRONTEND_TESTS = (
    "src/chat/artifacts/PreviewModal.test.tsx",
    "src/chat/artifacts/renderers/GeneratedImagePreview.test.tsx",
    "src/chat/artifacts/renderers/VideoPreview.test.tsx",
    "src/chat/generation/GenerationFrame.test.tsx",
    "src/chat/generation/GenerationToolDisplay.test.tsx",
    "src/chat/generation/generationState.test.ts",
    "src/chat/generation/generationThread.test.tsx",
)


def scored_scope(scope_id: str, killed: int = 8, survived: int = 2) -> dict[str, object]:
    scored = killed + survived
    # A scope that ran nothing reports a perfect score, which is exactly the shape the
    # gate must still reject.
    return {
        "id": scope_id,
        "executed": True,
        "killed": killed,
        "survived": survived,
        "score_percent": 100.0 if scored == 0 else killed * 100 / scored,
    }


def every_required_scope() -> list[dict[str, object]]:
    return [scored_scope(name) for name in sorted(critical_mutation_gate.REQUIRED_SCOPE_IDS)]


class GoMutationParserTest(unittest.TestCase):
    def test_sandbox_scope_is_a_compilable_policy_boundary(self) -> None:
        self.assertEqual(
            critical_mutation_gate.GO_SCOPES["sandbox"],
            "internal/sandbox/usersandbox/spec.go",
        )

    def test_media_boundaries_are_scoped_on_files_that_exist(self) -> None:
        self.assertEqual(
            critical_mutation_gate.GO_SCOPES["media_clamp"],
            "internal/mediagen/clamp.go",
        )
        self.assertEqual(
            critical_mutation_gate.GO_SCOPES["media_watcher"],
            "internal/mediagen/watcher_state.go",
        )
        for relative in critical_mutation_gate.GO_SCOPES.values():
            self.assertTrue((REPO / relative).is_file(), relative)

    def test_parses_killed_total_and_score(self) -> None:
        output = (
            "PASS \"/tmp/example.go.0\" with checksum abc\n"
            "FAIL \"/tmp/example.go.1\" with checksum def\n"
            "The mutation score is 0.750000 "
            "(3 passed, 1 failed, 2 duplicated, 1 skipped, total is 7)\n"
        )
        parsed = critical_mutation_gate.parse_go_mutation_output(output)
        self.assertEqual(parsed["killed"], 3)
        self.assertEqual(parsed["survived"], 1)
        self.assertEqual(parsed["score_percent"], 75.0)

    def test_rejects_missing_or_inconsistent_summary(self) -> None:
        with self.assertRaisesRegex(ValueError, "summary"):
            critical_mutation_gate.parse_go_mutation_output("PASS only")
        with self.assertRaisesRegex(ValueError, "score"):
            critical_mutation_gate.parse_go_mutation_output(
                "mutation score is 0.900000 (3 passed, 1 failed, 0 duplicated, 0 skipped)"
            )


class ScopeContractTest(unittest.TestCase):
    def test_every_required_scope_at_threshold_passes(self) -> None:
        self.assertEqual(
            critical_mutation_gate.scope_failures(every_required_scope(), 70.0), []
        )

    def test_a_missing_scope_fails(self) -> None:
        for dropped in ("media_clamp", "media_watcher", "media_frontend"):
            with self.subTest(dropped=dropped):
                scopes = [
                    scope for scope in every_required_scope() if scope["id"] != dropped
                ]
                self.assertIn(
                    f"missing scope {dropped}",
                    critical_mutation_gate.scope_failures(scopes, 70.0),
                )

    def test_a_scope_with_zero_executed_mutants_fails(self) -> None:
        scopes = every_required_scope()
        scopes[0] = scored_scope(str(scopes[0]["id"]), killed=0, survived=0)
        self.assertIn(
            f"{scopes[0]['id']} executed no mutants",
            critical_mutation_gate.scope_failures(scopes, 70.0),
        )

    def test_a_scope_that_did_not_execute_fails(self) -> None:
        scopes = every_required_scope()
        scopes[0]["executed"] = False
        self.assertIn(
            f"{scopes[0]['id']} did not execute",
            critical_mutation_gate.scope_failures(scopes, 70.0),
        )

    def test_a_scope_below_the_threshold_fails(self) -> None:
        scopes = every_required_scope()
        scopes[0] = scored_scope(str(scopes[0]["id"]), killed=6, survived=4)
        self.assertIn(
            f"{scopes[0]['id']}=60.00%",
            critical_mutation_gate.scope_failures(scopes, 70.0),
        )


class MediaFrontendScopeTest(unittest.TestCase):
    def test_stryker_mutates_every_media_file(self) -> None:
        config = json.loads(
            (REPO / "web/stryker.config.json").read_text(encoding="utf-8")
        )
        for relative in critical_mutation_gate.MEDIA_FRONTEND_FILES:
            self.assertIn(relative, config["mutate"])
            self.assertTrue((REPO / "web" / relative).is_file(), relative)

    def test_vitest_mutation_run_includes_the_media_suites(self) -> None:
        include = (REPO / "web/vitest.stryker.config.ts").read_text(encoding="utf-8")
        for relative in MEDIA_FRONTEND_TESTS:
            self.assertIn(f"'{relative}'", include)
            self.assertTrue((REPO / "web" / relative).is_file(), relative)

    def test_media_survivors_are_not_masked_by_the_aggregate(self) -> None:
        media = critical_mutation_gate.MEDIA_FRONTEND_FILES[0]
        report = {
            "schemaVersion": "1.0",
            "files": {
                name: {"mutants": [{"id": name, "status": "Killed"}] * 20}
                for name in ("src/unrelated.ts",)
            },
        }
        report["files"][media] = {
            "mutants": [{"id": "m1", "status": "Killed"}]
            + [{"id": "m2", "status": "Survived"}] * 6
        }
        for name in critical_mutation_gate.MEDIA_FRONTEND_FILES[1:]:
            report["files"][name] = {"mutants": [{"id": name, "status": "Killed"}]}
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "mutation.json"
            path.write_text(json.dumps(report), encoding="utf-8")
            aggregate = critical_mutation_gate.parse_frontend_report(path)
            scoped = critical_mutation_gate.parse_frontend_report(
                path, only=critical_mutation_gate.MEDIA_FRONTEND_FILES
            )
        self.assertGreaterEqual(aggregate["score_percent"], 70.0)
        self.assertLess(scoped["score_percent"], 70.0)

    def test_a_media_file_absent_from_the_report_fails(self) -> None:
        report = {
            "schemaVersion": "1.0",
            "files": {"src/unrelated.ts": {"mutants": [{"id": "1", "status": "Killed"}]}},
        }
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "mutation.json"
            path.write_text(json.dumps(report), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "does not mutate"):
                critical_mutation_gate.parse_frontend_report(
                    path, only=critical_mutation_gate.MEDIA_FRONTEND_FILES
                )

    def test_report_paths_are_matched_through_the_project_root(self) -> None:
        self.assertEqual(
            critical_mutation_gate.normalized_report_path("web\\src\\components\\image.tsx"),
            "src/components/image.tsx",
        )


class FrontendMutationParserTest(unittest.TestCase):
    def test_stryker_config_emits_the_machine_readable_report(self) -> None:
        config = json.loads(
            (REPO / "web/stryker.config.json").read_text(encoding="utf-8")
        )
        self.assertIn("json", config["reporters"])
        self.assertEqual(
            config["jsonReporter"]["fileName"],
            "reports/mutation/mutation.json",
        )

    def test_scores_detected_and_undetected_mutants(self) -> None:
        report = {
            "schemaVersion": "1.0",
            "files": {
                "src/a.ts": {
                    "mutants": [
                        {"id": "1", "status": "Killed"},
                        {"id": "2", "status": "Timeout"},
                        {"id": "3", "status": "Survived"},
                        {"id": "4", "status": "NoCoverage"},
                        {"id": "5", "status": "Ignored"},
                    ]
                }
            },
        }
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "mutation.json"
            path.write_text(json.dumps(report), encoding="utf-8")
            parsed = critical_mutation_gate.parse_frontend_report(path)
        self.assertEqual(parsed["killed"], 2)
        self.assertEqual(parsed["survived"], 2)
        self.assertEqual(parsed["score_percent"], 50.0)
        self.assertEqual(parsed["excluded"], 1)

    def test_rejects_empty_scored_report(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "mutation.json"
            path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "1.0",
                        "files": {"src/a.ts": {"mutants": [{"status": "Ignored"}]}},
                    }
                ),
                encoding="utf-8",
            )
            with self.assertRaisesRegex(ValueError, "no scored mutants"):
                critical_mutation_gate.parse_frontend_report(path)

    def test_rejects_stale_machine_report(self) -> None:
        report = {
            "schemaVersion": "1.0",
            "files": {
                "src/a.ts": {"mutants": [{"id": "1", "status": "Killed"}]}
            },
        }
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "mutation.json"
            path.write_text(json.dumps(report), encoding="utf-8")
            stale = time.time() - 48 * 60 * 60
            os.utime(path, (stale, stale))
            with self.assertRaisesRegex(ValueError, "stale"):
                critical_mutation_gate.parse_frontend_report(path)


if __name__ == "__main__":
    unittest.main()
