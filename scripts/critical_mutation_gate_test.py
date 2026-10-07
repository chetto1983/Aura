from __future__ import annotations

import json
import os
import pathlib
import re
import tempfile
import time
import unittest

import critical_mutation_gate
import go_mutation_cache
from evidence_metadata import candidate_commit


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

    def test_elicitation_boundaries_are_scoped(self) -> None:
        # The held clock and the request routing are the two Go boundaries a form rests on.
        self.assertEqual(
            critical_mutation_gate.GO_SCOPES["pausable"],
            "internal/pausable/context.go",
        )
        self.assertEqual(
            critical_mutation_gate.GO_SCOPES["elicitation_route"],
            "internal/agent/mcptools/elicitation_route.go",
        )

    def test_turn_reading_is_scoped(self) -> None:
        # The turn reading decides every turn's effort and the tools it preloads.
        self.assertEqual(
            critical_mutation_gate.GO_SCOPES["turn_reading"],
            "internal/agent/llm_agent_turn_reading.go",
        )

    def test_required_ids_and_go_scopes_cannot_drift(self) -> None:
        # REQUIRED_SCOPE_IDS is written out by hand on purpose. Deleting a boundary from
        # GO_SCOPES must fail the suite here rather than quietly delete its own requirement.
        self.assertEqual(
            critical_mutation_gate.REQUIRED_SCOPE_IDS
            - critical_mutation_gate.FRONTEND_SCOPE_IDS,
            set(critical_mutation_gate.GO_SCOPES),
        )
        self.assertTrue(
            critical_mutation_gate.MEDIA_SCOPE_IDS
            <= critical_mutation_gate.REQUIRED_SCOPE_IDS
        )

    def test_parses_killed_total_and_score(self) -> None:
        output = (
            "PASS \"/tmp/example.go.0\" with checksum abc\n"
            "FAIL \"/tmp/example.go.1\" with checksum def\n"
            "The mutation score is 0.750000 "
            "(3 passed, 1 failed, 2 duplicated, 1 skipped, total is 7)\n"
        )
        parsed = go_mutation_cache.parse_go_mutation_output(output)
        self.assertEqual(parsed["killed"], 3)
        self.assertEqual(parsed["survived"], 1)
        self.assertEqual(parsed["score_percent"], 75.0)

    def test_rejects_missing_or_inconsistent_summary(self) -> None:
        with self.assertRaisesRegex(ValueError, "summary"):
            go_mutation_cache.parse_go_mutation_output("PASS only")
        with self.assertRaisesRegex(ValueError, "score"):
            go_mutation_cache.parse_go_mutation_output(
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
            f"{scopes[0]['id']} executed no mutants",
            critical_mutation_gate.scope_failures(scopes, 70.0),
        )

    def test_the_executed_flag_is_read_off_the_counts(self) -> None:
        measured = critical_mutation_gate.scope(
            "media_clamp", ["internal/mediagen/clamp.go"], {"killed": 7, "survived": 3}
        )
        self.assertIs(measured["executed"], True)
        empty = critical_mutation_gate.scope(
            "media_clamp", ["internal/mediagen/clamp.go"], {"killed": 0, "survived": 0}
        )
        self.assertIs(empty["executed"], False)
        self.assertIn(
            "media_clamp executed no mutants",
            critical_mutation_gate.scope_failures([empty], 70.0),
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

    def test_a_media_file_with_no_scored_mutant_fails(self) -> None:
        # The shape that used to pass: two of the eight files measured nothing — one silenced
        # with `// Stryker disable all`, one that produced no mutants at all — and the other
        # six carried the scope to 100%.
        silenced = critical_mutation_gate.MEDIA_FRONTEND_FILES[0]
        empty = critical_mutation_gate.MEDIA_FRONTEND_FILES[1]
        report: dict[str, object] = {"schemaVersion": "1.0", "files": {}}
        files = report["files"]
        assert isinstance(files, dict)
        for name in critical_mutation_gate.MEDIA_FRONTEND_FILES:
            files[name] = {"mutants": [{"id": name, "status": "Killed"}] * 5}
        files[silenced] = {"mutants": [{"id": "s", "status": "Ignored"}] * 4}
        files[empty] = {"mutants": []}
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "mutation.json"
            path.write_text(json.dumps(report), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "scores no mutant") as caught:
                critical_mutation_gate.parse_frontend_report(
                    path, only=critical_mutation_gate.MEDIA_FRONTEND_FILES
                )
            self.assertIn(silenced, str(caught.exception))
            self.assertIn(empty, str(caught.exception))
            # The aggregate is unaffected — which is exactly why the per-file check is needed.
            self.assertEqual(
                critical_mutation_gate.parse_frontend_report(path)["score_percent"], 100.0
            )

    def test_a_media_file_whose_mutants_all_failed_to_compile_fails(self) -> None:
        report: dict[str, object] = {"schemaVersion": "1.0", "files": {}}
        files = report["files"]
        assert isinstance(files, dict)
        for name in critical_mutation_gate.MEDIA_FRONTEND_FILES:
            files[name] = {"mutants": [{"id": name, "status": "Killed"}] * 5}
        broken = critical_mutation_gate.MEDIA_FRONTEND_FILES[2]
        files[broken] = {"mutants": [{"id": "c", "status": "CompileError"}] * 3}
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "mutation.json"
            path.write_text(json.dumps(report), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, re.escape(broken)):
                critical_mutation_gate.parse_frontend_report(
                    path, only=critical_mutation_gate.MEDIA_FRONTEND_FILES
                )

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


FINGERPRINT = "f" * 64
GO_SCOPES = critical_mutation_gate.GO_SCOPES


def mutesting_log(killed: int = 8, survived: int = 2) -> str:
    total = killed + survived
    return (
        f"The mutation score is {killed / total:.6f} "
        f"({killed} passed, {survived} failed, 0 duplicated, 0 skipped, total is {total})\n"
    )


class GateModesTest(unittest.TestCase):
    """CI's parallel shape with fakes: group jobs measure, the aggregate only reads."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self._tmp.name)
        self.cache_dir = self.root / "cache"
        self.cache_dir.mkdir()
        self.candidate = candidate_commit(REPO)
        self.mutated: list[str] = []
        self.outcomes: dict[str, tuple[int, str]] = {}
        self.frontend = self.root / "mutation.json"
        self.frontend.write_text(
            json.dumps(
                {
                    "schemaVersion": "1.0",
                    "files": {
                        name: {"mutants": [{"id": name, "status": "Killed"}]}
                        for name in critical_mutation_gate.MEDIA_FRONTEND_FILES
                    },
                }
            ),
            encoding="utf-8",
        )

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def mutate(self, relative_path: str) -> tuple[int, str]:
        self.mutated.append(relative_path)
        return self.outcomes.get(relative_path, (0, mutesting_log()))

    def make_measurer(self, executable, repo, log_dir, cache_dir, relative_paths, require_measured=False):  # type: ignore[no-untyped-def]
        return go_mutation_cache.scope_measurer(
            relative_paths,
            lambda path: FINGERPRINT,
            None if require_measured else self.mutate,
            cache_dir,
            log_dir,
            self.candidate,
        )

    def store(
        self,
        scope_id: str,
        commit: str | None = None,
        duration: float = 12.5,
        fingerprint: str = FINGERPRINT,
        log: str | None = None,
    ) -> None:
        entry = {
            "schema_version": go_mutation_cache.SCHEMA_VERSION,
            "scope": scope_id,
            "file": GO_SCOPES[scope_id],
            "fingerprint": fingerprint,
            "commit": commit or self.candidate,
            "duration_seconds": duration,
            "log": log or mutesting_log(),
        }
        (self.cache_dir / f"{scope_id}.json").write_text(json.dumps(entry), encoding="utf-8")

    def args(self, *extra: str):  # type: ignore[no-untyped-def]
        return critical_mutation_gate.parse_args(
            [
                "--go-mutesting", "fake",
                "--frontend-report", str(self.frontend),
                "--output", str(self.root / "report.json"),
                "--log-dir", str(self.root / "logs"),
                "--go-cache-dir", str(self.cache_dir),
                *extra,
            ]
        )

    def aggregate(self) -> dict[str, object]:
        return critical_mutation_gate.run(
            self.args("--require-measured"), make_measurer=self.make_measurer
        )

    def written_report(self) -> dict[str, object]:
        return json.loads((self.root / "report.json").read_text(encoding="utf-8"))

    def group(self, *scopes: str) -> pathlib.Path:
        export = self.root / "export"
        critical_mutation_gate.measure_group(
            self.args("--measure-scopes", *scopes, "--export-dir", str(export)),
            make_measurer=self.make_measurer,
        )
        return export

    def test_the_aggregate_reports_this_runs_measurements_as_measurements(self) -> None:
        for index, scope_id in enumerate(GO_SCOPES):
            self.store(scope_id, duration=100.0 + index)
        report = self.aggregate()
        measured = [scope for scope in report["scopes"] if scope["id"] in GO_SCOPES]  # type: ignore[union-attr,index]
        self.assertEqual([scope["id"] for scope in measured], list(GO_SCOPES))
        for index, scope in enumerate(measured):
            self.assertNotIn("reused_from", scope)
            self.assertEqual(scope["duration_seconds"], 100.0 + index)
            self.assertEqual(scope["fingerprint"], FINGERPRINT)
        self.assertIs(report["passed"], True)
        self.assertEqual(self.mutated, [])

    def test_the_aggregate_names_an_older_measurement_as_reused(self) -> None:
        for scope_id in GO_SCOPES:
            self.store(scope_id)
        self.store("pausable", commit="b" * 40)
        scopes = {scope["id"]: scope for scope in self.aggregate()["scopes"]}  # type: ignore[union-attr,index]
        self.assertEqual(scopes["pausable"]["reused_from"], "b" * 40)
        self.assertNotIn("reused_from", scopes["gateway"])

    def test_the_aggregate_fails_closed_when_a_group_artifact_is_missing(self) -> None:
        for scope_id in GO_SCOPES:
            if scope_id != "elicitation_route":
                self.store(scope_id)
        with self.assertRaisesRegex(RuntimeError, "elicitation_route: no measurement .*missing"):
            self.aggregate()
        self.assertEqual(self.mutated, [], "the aggregate re-measured instead of failing")
        report = self.written_report()
        self.assertIs(report["passed"], False)
        self.assertIn("elicitation_route", str(report["error"]))

    def test_the_aggregate_fails_closed_on_a_stale_entry(self) -> None:
        for scope_id in GO_SCOPES:
            self.store(scope_id)
        self.store("media_clamp", fingerprint="0" * 64)
        with self.assertRaisesRegex(RuntimeError, "media_clamp: no measurement .*stale"):
            self.aggregate()
        self.assertEqual(self.mutated, [])

    def test_the_aggregate_fails_on_a_below_floor_group_result(self) -> None:
        for scope_id in GO_SCOPES:
            self.store(scope_id)
        self.store("gateway", log=mutesting_log(killed=6, survived=4))
        with self.assertRaisesRegex(RuntimeError, "gateway=60.00%"):
            self.aggregate()

    def test_a_group_measures_only_its_scopes_and_exports_them(self) -> None:
        export = self.group("gateway", "pausable")
        self.assertEqual(self.mutated, [GO_SCOPES["gateway"], GO_SCOPES["pausable"]])
        self.assertEqual(
            sorted(path.name for path in export.iterdir()),
            ["gateway.json", "gateway.log", "pausable.json", "pausable.log"],
        )
        self.assertFalse((self.root / "report.json").exists())

    def test_a_group_exports_a_reused_entry_unchanged(self) -> None:
        self.store("gateway", commit="b" * 40)
        export = self.group("gateway")
        self.assertEqual(self.mutated, [])
        exported = json.loads((export / "gateway.json").read_text(encoding="utf-8"))
        self.assertEqual(exported["commit"], "b" * 40)

    def test_a_group_rejects_an_unknown_scope_before_measuring(self) -> None:
        with self.assertRaisesRegex(ValueError, "unknown Go scope: nosuch"):
            self.group("gateway", "nosuch")
        self.assertEqual(self.mutated, [])

    def test_a_failing_scope_does_not_stop_its_group_siblings(self) -> None:
        self.outcomes[GO_SCOPES["gateway"]] = (2, "panic: boom\n")
        with self.assertRaisesRegex(RuntimeError, "gateway: go-mutesting exited 2"):
            self.group("gateway", "pausable")
        export = self.root / "export"
        self.assertTrue((export / "pausable.json").is_file())
        self.assertTrue((export / "gateway.log").is_file())
        self.assertFalse((export / "gateway.json").exists())


if __name__ == "__main__":
    unittest.main()
