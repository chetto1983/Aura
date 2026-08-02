from __future__ import annotations

import datetime as dt
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest


CANDIDATE = "a" * 40
NOW = dt.datetime.now(dt.timezone.utc).isoformat()
GATE = pathlib.Path(__file__).with_name("release_readiness_gate.py")


def common(**values: object) -> dict[str, object]:
    return {
        "schema_version": 1,
        "generated_at": NOW,
        "candidate_commit": CANDIDATE,
        **values,
    }


def valid_evidence() -> dict[str, dict[str, object]]:
    return {
        "security-report.json": common(
            passed=True,
            unresolved_release_blocking_alerts=0,
            checks=[
                {"id": name, "executed": True, "passed": True}
                for name in ("codeql", "govulncheck", "workflow_pin", "strict_profile")
            ],
        ),
        "coverage-report.json": common(
            passed=True,
            statements_percent=91.2,
            tiers_executed=["db_integration"],
            empty_tiers=0,
        ),
        "mutation-report.json": common(
            passed=True,
            scopes=[
                {"id": name, "executed": True, "score_percent": 75.0}
                for name in (
                    "gateway",
                    "identity_isolation",
                    "profile_validation",
                    "sandbox",
                    "frontend",
                )
            ],
        ),
        "capability-eval.json": common(
            totals={
                "declared": 16,
                "executed": 16,
                "passed": 16,
                "failed": 0,
                "skipped": 0,
                "missing": 0,
                "blocked": 0,
            },
            scenarios=[
                {
                    "scenario_id": f"{capability_class}.{polarity}",
                    "capability_class": capability_class,
                    "polarity": polarity,
                    "status": "pass",
                }
                for capability_class in (
                    "shell_containment",
                    "filesystem_ownership",
                    "mcp_contract",
                    "memory_contract",
                    "pause_resume_atomicity",
                    "terminal_error_truth",
                    "idempotent_mutation",
                    "workflow_completion",
                )
                for polarity in ("positive", "negative")
            ],
        ),
        "load-report.json": common(
            passed=True,
            supported_concurrency=32,
            endpoints={
                name: {
                    "requests": 200,
                    "success_ratio": 1.0,
                    "p95_ms": 50.0,
                    "p95_budget_ms": 250.0,
                }
                for name in ("healthz", "readyz")
            },
            resource={"peak_rss_bytes": 80_000_000, "max_rss_bytes": 536_870_912},
        ),
        "chaos-report.json": common(
            passed=True,
            scenarios=[
                {
                    "id": name,
                    "executed": True,
                    "passed": True,
                    "cleanup_ok": True,
                    "false_ready_responses": 0,
                    "duplicate_side_effects": 0,
                }
                for name in (
                    "database-outage-recovery",
                    "mcp-timeout-storm",
                    "garage-outage-recovery",
                    "process-kill-write-atomicity",
                )
            ],
        ),
        "dr-report.json": common(
            passed=True,
            planes_executed=3,
            planes_required=3,
            planes=[
                {
                    "plane": name,
                    "restored_items": 1,
                    "checksum_ok": True,
                    "cleanup_ok": True,
                }
                for name in ("postgres", "sidecars", "garage")
            ],
        ),
        "observability-report.json": common(
            passed=True,
            checks=[
                {"id": name, "executed": True, "passed": True}
                for name in (
                    "static_contract",
                    "live_healthz",
                    "live_readyz",
                    "dashboards",
                    "alerts",
                    "runbooks",
                )
            ],
        ),
        "rollback-report.json": common(
            passed=True,
            previous_image_digest="sha256:" + "b" * 64,
            candidate_image_digest="sha256:" + "c" * 64,
            rollback_command="docker compose up -d aura",
            config_started=True,
            migrations_compatible=True,
            readiness_healthy=True,
        ),
        "audit-closure-report.json": common(
            passed=True,
            release_ready=True,
            open_total=0,
            counts={
                "external_blocked": 0,
                "open": 0,
            },
            issues=[],
        ),
    }


class ReleaseReadinessGateTest(unittest.TestCase):
    def run_gate(
        self,
        evidence: dict[str, dict[str, object]],
        *,
        candidate: str = CANDIDATE,
    ) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as raw:
            root = pathlib.Path(raw)
            for name, report in evidence.items():
                (root / name).write_text(json.dumps(report), encoding="utf-8")
            output = root / "release-readiness-report.json"
            result = subprocess.run(
                [
                    sys.executable,
                    str(GATE),
                    "--evidence-dir",
                    str(root),
                    "--candidate",
                    candidate,
                    "--output",
                    str(output),
                ],
                text=True,
                capture_output=True,
                check=False,
            )
            if result.returncode == 0:
                result.report = json.loads(output.read_text(encoding="utf-8"))  # type: ignore[attr-defined]
            return result

    def test_valid_candidate_passes_and_hashes_every_artifact(self) -> None:
        result = self.run_gate(valid_evidence())
        self.assertEqual(result.returncode, 0, result.stderr)
        report = result.report  # type: ignore[attr-defined]
        self.assertTrue(report["passed"])
        self.assertEqual(report["score"], 10.0)
        self.assertEqual(len(report["evidence"]), 10)
        self.assertTrue(all(len(item["sha256"]) == 64 for item in report["evidence"]))

    def test_missing_artifact_fails_closed(self) -> None:
        evidence = valid_evidence()
        del evidence["rollback-report.json"]
        result = self.run_gate(evidence)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("rollback-report.json", result.stderr)

    def test_stale_or_wrong_commit_artifact_fails(self) -> None:
        evidence = valid_evidence()
        evidence["coverage-report.json"]["generated_at"] = "2020-01-01T00:00:00+00:00"
        result = self.run_gate(evidence)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("stale", result.stderr)

        evidence = valid_evidence()
        evidence["load-report.json"]["candidate_commit"] = "b" * 40
        result = self.run_gate(evidence)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("candidate_commit", result.stderr)

    def test_mutation_threshold_and_required_scenarios_fail_closed(self) -> None:
        evidence = valid_evidence()
        evidence["mutation-report.json"]["scopes"][2]["score_percent"] = 69.9  # type: ignore[index]
        result = self.run_gate(evidence)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("profile_validation", result.stderr)

        for filename, collection in (
            ("chaos-report.json", "scenarios"),
            ("dr-report.json", "planes"),
        ):
            evidence = valid_evidence()
            evidence[filename][collection].pop()  # type: ignore[union-attr]
            result = self.run_gate(evidence)
            self.assertNotEqual(result.returncode, 0)
            self.assertIn("missing", result.stderr)

    def test_current_open_items_block_release(self) -> None:
        evidence = valid_evidence()
        evidence["audit-closure-report.json"].update(
            release_ready=False,
            open_total=1,
            counts={"external_blocked": 1, "open": 0},
            issues=[{"id": "EXT-001", "state": "external_blocked"}],
        )
        result = self.run_gate(evidence)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("release_ready", result.stderr)


if __name__ == "__main__":
    unittest.main()
