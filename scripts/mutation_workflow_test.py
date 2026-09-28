from __future__ import annotations

import json
import pathlib
import re
import unittest

import critical_mutation_gate


REPO = pathlib.Path(__file__).resolve().parents[1]
GO_CACHE = "artifacts/go-mutation-cache"
SKILLS_CACHE = "artifacts/skills-mutation-cache"


def workflow_text(workflow: str) -> str:
    return (REPO / ".github/workflows" / workflow).read_text(encoding="utf-8")


def job_body(workflow: str, job: str) -> str:
    text = workflow_text(workflow)
    return re.split(r"\n  [A-Za-z0-9_-]+:\n", text.split(f"\n  {job}:\n", 1)[1], maxsplit=1)[0]


def job_steps(workflow: str, job: str) -> list[str]:
    return job_body(workflow, job).split("\n      - name:")[1:]


def step_index(steps: list[str], *needles: str) -> int:
    matches = [i for i, step in enumerate(steps) if all(needle in step for needle in needles)]
    if len(matches) != 1:
        raise AssertionError(f"expected one step with {needles}, found {len(matches)}")
    return matches[0]


def matrix_values(body: str, field: str) -> list[str]:
    return re.findall(rf"^\s+(?:- )?{field}: (.+)$", body, re.MULTILINE)


class StrykerJobTest(unittest.TestCase):
    def test_ci_reuses_stryker_incremental_results_across_commits(self) -> None:
        config = json.loads((REPO / "web/stryker.config.json").read_text(encoding="utf-8"))
        self.assertIs(config["incremental"], True)
        self.assertEqual(config["incrementalFile"], "reports/stryker-incremental.json")

        job = job_body("ci.yml", "web-mutation-stryker")
        self.assertIn("actions/cache/restore@", job)
        self.assertIn("actions/cache/save@", job)
        self.assertEqual(job.count("path: web/reports/stryker-incremental.json"), 2)
        self.assertIn("hashFiles('web/package-lock.json'", job)
        self.assertIn("github.sha", job)
        self.assertIn("restore-keys:", job)

    def test_the_stryker_job_hands_its_report_to_the_aggregate(self) -> None:
        steps = job_steps("ci.yml", "web-mutation-stryker")
        upload = steps[step_index(steps, "actions/upload-artifact@", "name: stryker-mutation")]
        self.assertIn("web/reports/mutation/mutation.json", upload)
        self.assertIn("web/reports/stryker-incremental.json", upload)
        self.assertLess(step_index(steps, "npm run mutation"), step_index(steps, "actions/upload-artifact@"))


class GoGroupJobTest(unittest.TestCase):
    def setUp(self) -> None:
        self.body = job_body("ci.yml", "go-mutation")
        self.steps = job_steps("ci.yml", "go-mutation")

    def test_the_groups_partition_every_go_scope(self) -> None:
        groups = [value.split() for value in matrix_values(self.body, "scopes")]
        scopes = [scope for group in groups for scope in group]
        self.assertEqual(sorted(scopes), sorted(critical_mutation_gate.GO_SCOPES))
        self.assertEqual(len(scopes), len(set(scopes)), "a scope is measured by two groups")
        self.assertLessEqual(len(groups), 3, "each group is a runner; keep the job count small")

    def test_a_failing_group_does_not_cancel_its_siblings(self) -> None:
        self.assertIn("fail-fast: false", self.body)

    def test_each_group_restores_measures_its_scopes_and_exports_without_saving(self) -> None:
        restore = step_index(self.steps, "actions/cache/restore@", f"path: {GO_CACHE}")
        run = step_index(self.steps, "python3 scripts/critical_mutation_gate.py")
        upload = step_index(self.steps, "actions/upload-artifact@")
        self.assertLess(restore, run)
        self.assertLess(run, upload)
        self.assertIn("github.sha", self.steps[restore])
        self.assertIn("restore-keys:", self.steps[restore])
        self.assertIn("--measure-scopes ${{ matrix.scopes }}", self.steps[run])
        self.assertIn(f"--go-cache-dir {GO_CACHE}", self.steps[run])
        self.assertIn("--export-dir artifacts/go-mutation-group", self.steps[run])
        self.assertIn("name: go-mutation-${{ matrix.group }}", self.steps[upload])
        self.assertIn("if: always()", self.steps[upload])
        self.assertNotIn("actions/cache/save@", self.body, "only the aggregate saves the cache")


class AggregateJobTest(unittest.TestCase):
    def setUp(self) -> None:
        self.body = job_body("ci.yml", "web-mutation")
        self.steps = job_steps("ci.yml", "web-mutation")

    def test_the_aggregate_keeps_its_name_and_runs_after_both_producers(self) -> None:
        self.assertIn("name: Critical mutation testing (each boundary >= 70% killed)", self.body)
        self.assertRegex(self.body, r"needs: \[web-mutation-stryker, go-mutation\]")
        # A skipped aggregate would read as a green check; it must run and fail closed.
        self.assertIn("if: ${{ !cancelled() }}", self.body)

    def test_the_aggregate_reads_every_group_and_never_measures(self) -> None:
        go = step_index(self.steps, "actions/download-artifact@", "pattern: go-mutation-*")
        self.assertIn("merge-multiple: true", self.steps[go])
        self.assertIn(f"path: {GO_CACHE}", self.steps[go])
        stryker = step_index(self.steps, "actions/download-artifact@", "name: stryker-mutation")
        self.assertIn("path: web/reports", self.steps[stryker])
        run = step_index(self.steps, "python3 scripts/critical_mutation_gate.py")
        self.assertIn("--require-measured", self.steps[run])
        self.assertIn(f"--go-cache-dir {GO_CACHE}", self.steps[run])
        self.assertLess(max(go, stryker), run)

    def test_the_aggregate_saves_the_merged_cache_once(self) -> None:
        lookup = step_index(self.steps, "actions/cache/restore@", "lookup-only: true")
        save = step_index(self.steps, "actions/cache/save@")
        self.assertIn(f"path: {GO_CACHE}", self.steps[lookup])
        self.assertIn(f"path: {GO_CACHE}", self.steps[save])
        self.assertIn("if: always()", self.steps[save])
        self.assertIn("cache-hit != 'true'", self.steps[save])
        self.assertLess(save, step_index(self.steps, "actions/upload-artifact@", "critical-mutation"))

    def test_the_evidence_artifact_carries_exactly_one_report(self) -> None:
        upload = self.steps[step_index(self.steps, "actions/upload-artifact@", "name: critical-mutation")]
        self.assertEqual(upload.count("mutation-report.json"), 1)


class SkillsMutationJobTest(unittest.TestCase):
    def setUp(self) -> None:
        self.body = job_body("skills.yml", "skills-mutation")
        self.steps = job_steps("skills.yml", "skills-mutation")

    def test_the_gate_job_no_longer_mutates(self) -> None:
        gate = job_body("skills.yml", "skills-gate")
        self.assertNotIn("go-mutesting", gate)
        self.assertNotIn(SKILLS_CACHE, gate)

    def test_both_jobs_share_one_environment(self) -> None:
        # GOFLAGS and the DSNs are fingerprint and test inputs: one definition, no drift.
        text = workflow_text("skills.yml")
        self.assertEqual(text.count("AURA_DB_URL:"), 1)
        self.assertLess(text.index("AURA_DB_URL:"), text.index("\njobs:"))

    def test_each_file_measures_against_a_migrated_postgres_with_its_own_cache(self) -> None:
        self.assertIn("fail-fast: false", self.body)
        self.assertEqual(
            sorted(matrix_values(self.body, "args")),
            ["--advisory internal/skills/writer.go", "internal/skills/validator.go"],
        )
        restore = step_index(self.steps, "actions/cache/restore@", f"path: {SKILLS_CACHE}")
        database = step_index(self.steps, "make db-up", "go run ./cmd/aura db migrate")
        run = step_index(self.steps, "python3 scripts/go_mutation_cache.py")
        save = step_index(self.steps, "actions/cache/save@", f"path: {SKILLS_CACHE}")
        self.assertLess(restore, run)
        self.assertLess(database, run)
        self.assertLess(run, save)
        self.assertIn("${{ matrix.name }}-${{ github.sha }}", self.steps[restore])
        self.assertIn('GOFLAGS: "-tags=db_integration"', self.steps[run])
        self.assertIn(f"--cache-dir {SKILLS_CACHE}", self.steps[run])
        self.assertIn("${{ matrix.args }}", self.steps[run])
        self.assertIn("if: always()", self.steps[save])
        self.assertIn("cache-hit != 'true'", self.steps[save])


if __name__ == "__main__":
    unittest.main()
