from __future__ import annotations

import contextlib
import io
import json
import pathlib
import tempfile
import unittest

import critical_mutation_gate
import go_mutation_cache


MODULE = {"Path": "example.com/m", "Main": True}
TOOLCHAIN = {
    "GOFLAGS": "",
    "GOVERSION": "go1.27.1",
    "GOOS": "linux",
    "GOARCH": "amd64",
    "runner": "mod\tgithub.com/avito-tech/go-mutesting\tv0.0.0-20251226130216-48d0401f00fb",
}
SUMMARY = (
    "PASS \"/tmp/x.go.0\" with checksum a\n"
    "The mutation score is {score:.6f} "
    "({killed} passed, {survived} failed, 0 duplicated, 0 skipped, total is {total})\n"
)


def mutesting_log(killed: int = 8, survived: int = 2) -> str:
    total = killed + survived
    return SUMMARY.format(score=killed / total, killed=killed, survived=survived, total=total)


class FakeModule:
    """A throwaway module tree plus the `go list -deps -test -json` view of internal/a."""

    def __init__(self, root: pathlib.Path) -> None:
        self.root = root
        for relative, body in {
            "go.mod": "module example.com/m\n",
            "go.sum": "example.org/dep v1.0.0 h1:abc=\n",
            "internal/a/a.go": "package a\n",
            "internal/a/a_test.go": "package a\n",
            "internal/a/testdata/case.json": "{}\n",
            "internal/b/b.go": "package b\n",
            "internal/b/embed/doc.md": "doc\n",
            "internal/c/c.go": "package c\n",
        }.items():
            self.write(relative, body)
        self.cache = root / "gocache"
        self.write("gocache/testmain.go", "package main\n")

    def write(self, relative: str, body: str) -> None:
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(body, encoding="utf-8")

    def packages(self) -> list[dict[str, object]]:
        a = str(self.root / "internal/a")
        return [
            {"ImportPath": "fmt", "Dir": "/usr/lib/go/src/fmt", "Standard": True, "GoFiles": ["print.go"]},
            {
                "ImportPath": "example.org/dep",
                "Dir": "/home/x/go/pkg/mod/example.org/dep@v1.0.0",
                "Module": {"Path": "example.org/dep", "Main": False},
                "GoFiles": ["dep.go"],
            },
            {
                "ImportPath": "example.com/m/internal/b",
                "Dir": str(self.root / "internal/b"),
                "Module": MODULE,
                "GoFiles": ["b.go"],
                "EmbedFiles": ["embed/doc.md"],
            },
            {
                "ImportPath": "example.com/m/internal/a",
                "Dir": a,
                "Module": MODULE,
                "GoFiles": ["a.go"],
                "TestGoFiles": ["a_test.go"],
            },
            {
                "ImportPath": "example.com/m/internal/a [example.com/m/internal/a.test]",
                "Dir": a,
                "Module": MODULE,
                "ForTest": "example.com/m/internal/a",
                "GoFiles": ["a.go", "a_test.go"],
            },
            # The synthesized test main: its one file lives in GOCACHE, not in the module.
            {
                "ImportPath": "example.com/m/internal/a.test",
                "Dir": a,
                "Name": "main",
                "Module": MODULE,
                "GoFiles": [str(self.cache / "testmain.go")],
            },
        ]

    def fingerprint(self, toolchain: dict[str, str] | None = None, **kwargs: object) -> str:
        packages = kwargs.pop("packages", None) or self.packages()
        return go_mutation_cache.fingerprint(
            self.root, "internal/a/a.go", toolchain or TOOLCHAIN, packages, **kwargs
        )


class FingerprintTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.module = FakeModule(pathlib.Path(self._tmp.name).resolve())
        self.baseline = self.module.fingerprint()

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def test_identical_inputs_give_the_same_fingerprint_in_any_order(self) -> None:
        self.assertRegex(self.baseline, r"^[0-9a-f]{64}$")
        self.assertEqual(self.module.fingerprint(), self.baseline)
        reordered = list(reversed(self.module.packages()))
        self.assertEqual(self.module.fingerprint(packages=reordered), self.baseline)

    def test_a_changed_input_of_the_closure_changes_it(self) -> None:
        for relative in (
            "internal/a/a.go",
            "internal/a/a_test.go",
            "internal/a/testdata/case.json",
            "internal/b/b.go",
            "internal/b/embed/doc.md",
            "go.mod",
            "go.sum",
        ):
            with self.subTest(changed=relative):
                original = (self.module.root / relative).read_text(encoding="utf-8")
                self.module.write(relative, original + "// changed\n")
                try:
                    self.assertNotEqual(self.module.fingerprint(), self.baseline)
                finally:
                    self.module.write(relative, original)

    def test_a_new_testdata_file_changes_it(self) -> None:
        self.module.write("internal/a/testdata/nested/extra.txt", "new\n")
        self.assertNotEqual(self.module.fingerprint(), self.baseline)

    def test_a_changed_toolchain_changes_it(self) -> None:
        for key, value in (
            ("GOFLAGS", "-tags=db_integration"),
            ("GOVERSION", "go1.27.2"),
            ("runner", "mod\tgithub.com/avito-tech/go-mutesting\tv0.0.1"),
        ):
            with self.subTest(changed=key):
                self.assertNotEqual(
                    self.module.fingerprint({**TOOLCHAIN, key: value}), self.baseline
                )

    def test_the_scope_file_is_part_of_the_identity(self) -> None:
        other = go_mutation_cache.fingerprint(
            self.module.root, "internal/b/b.go", TOOLCHAIN, self.module.packages()
        )
        self.assertNotEqual(other, self.baseline)

    def test_a_file_outside_the_closure_does_not_change_it(self) -> None:
        self.module.write("internal/c/c.go", "package c\n\nvar changed = 1\n")
        self.module.write("gocache/testmain.go", "package main\n// regenerated\n")
        self.assertEqual(self.module.fingerprint(), self.baseline)

    def test_only_main_module_files_are_read(self) -> None:
        read: list[pathlib.Path] = []

        def recording(path: pathlib.Path) -> bytes:
            read.append(path)
            return path.read_bytes()

        self.module.fingerprint(read=recording)
        relative = sorted(path.relative_to(self.module.root).as_posix() for path in read)
        self.assertEqual(
            relative,
            [
                "go.mod",
                "go.sum",
                "internal/a/a.go",
                "internal/a/a_test.go",
                "internal/a/testdata/case.json",
                "internal/b/b.go",
                "internal/b/embed/doc.md",
            ],
        )

    def test_go_list_json_stream_is_decoded(self) -> None:
        stream = '{\n\t"ImportPath": "a"\n}\n{\n\t"ImportPath": "b"\n}\n'
        self.assertEqual(
            [item["ImportPath"] for item in go_mutation_cache.decode_packages(stream)],
            ["a", "b"],
        )


class MeasureTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        root = pathlib.Path(self._tmp.name)
        self.cache_dir = root / "cache"
        self.log_dir = root / "logs"
        self.calls: list[str] = []
        self.output = (0, mutesting_log())

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def mutate(self, relative_path: str) -> tuple[int, str]:
        self.calls.append(relative_path)
        return self.output

    def measure(
        self, fingerprint: str = "f" * 64, commit: str = "1" * 40, required: bool = False
    ) -> dict[str, object]:
        return go_mutation_cache.measure(
            "gateway",
            "internal/gateway/classify.go",
            fingerprint,
            None if required else self.mutate,
            self.cache_dir,
            self.log_dir,
            commit,
        )

    def test_an_entry_measured_on_this_commit_is_not_a_reuse(self) -> None:
        # A sibling job of the same run measured it: that is a measurement of the candidate,
        # carrying that job's duration, not a result carried over from another commit.
        first = self.measure(commit="1" * 40)
        again = self.measure(commit="1" * 40, required=True)
        self.assertEqual(len(self.calls), 1)
        self.assertNotIn("reused_from", again)
        self.assertEqual(again["duration_seconds"], first["duration_seconds"])
        self.assertEqual(
            (self.log_dir / "gateway.log").read_text(encoding="utf-8"), mutesting_log()
        )

    def test_a_required_measurement_that_is_missing_or_stale_fails_closed(self) -> None:
        with self.assertRaisesRegex(RuntimeError, "gateway: no measurement .*missing"):
            self.measure(required=True)
        self.measure(fingerprint="e" * 64)
        with self.assertRaisesRegex(RuntimeError, "gateway: no measurement .*stale"):
            self.measure(fingerprint="f" * 64, required=True)
        self.assertEqual(len(self.calls), 1, "a required measurement must never run go-mutesting")

    def test_every_fingerprint_is_taken_before_the_first_mutant_runs(self) -> None:
        # A run leaves files behind (rapid writes a timestamped fail file into the mutated
        # package's testdata/rapid/), and a later scope whose closure holds that package would
        # hash them: a fingerprint that no fresh checkout can ever reproduce.
        def fingerprint_of(relative_path: str) -> str:
            return ("d" if self.calls else "c") * 64

        measure_scope = go_mutation_cache.scope_measurer(
            ["internal/gateway/classify.go", "internal/agent/mcptools/elicitation_route.go"],
            fingerprint_of,
            self.mutate,
            self.cache_dir,
            self.log_dir,
            "1" * 40,
        )
        first = measure_scope("gateway", "internal/gateway/classify.go")
        second = measure_scope("elicitation_route", "internal/agent/mcptools/elicitation_route.go")
        self.assertEqual(len(self.calls), 2)
        self.assertEqual({first["fingerprint"], second["fingerprint"]}, {"c" * 64})

    def test_a_cold_cache_runs_go_mutesting_and_stores_the_result(self) -> None:
        measured = self.measure()
        self.assertEqual(self.calls, ["internal/gateway/classify.go"])
        self.assertEqual((measured["killed"], measured["survived"]), (8, 2))
        self.assertEqual(measured["fingerprint"], "f" * 64)
        self.assertNotIn("reused_from", measured)
        self.assertIsInstance(measured["duration_seconds"], float)
        self.assertTrue((self.cache_dir / "gateway.json").is_file())
        self.assertEqual(
            (self.log_dir / "gateway.log").read_text(encoding="utf-8"), mutesting_log()
        )

    def test_an_identical_fingerprint_reuses_without_running(self) -> None:
        self.measure(commit="1" * 40)
        self.output = (0, mutesting_log(killed=1, survived=9))
        reused = self.measure(commit="2" * 40)
        self.assertEqual(len(self.calls), 1, "go-mutesting ran for an unchanged closure")
        self.assertEqual(reused["reused_from"], "1" * 40)
        self.assertEqual(reused["fingerprint"], "f" * 64)
        self.assertEqual((reused["killed"], reused["survived"]), (8, 2))
        self.assertLess(reused["duration_seconds"], 5.0)
        log = (self.log_dir / "gateway.log").read_text(encoding="utf-8")
        self.assertIn("1" * 40, log.splitlines()[0])
        self.assertTrue(log.endswith(mutesting_log()))

    def test_a_reuse_keeps_naming_the_commit_that_measured(self) -> None:
        self.measure(commit="1" * 40)
        self.measure(commit="2" * 40)
        third = self.measure(commit="3" * 40)
        self.assertEqual(third["reused_from"], "1" * 40)

    def test_a_different_fingerprint_runs_again(self) -> None:
        self.measure(fingerprint="f" * 64)
        measured = self.measure(fingerprint="e" * 64)
        self.assertEqual(len(self.calls), 2)
        self.assertNotIn("reused_from", measured)

    def test_an_unusable_cache_entry_means_run_never_error(self) -> None:
        entry = self.cache_dir / "gateway.json"
        for label, damage in (
            ("not json", lambda stored: "{broken"),
            ("not an object", lambda stored: "[]"),
            ("other schema", lambda stored: json.dumps({**stored, "schema_version": 999})),
            ("no summary in the log", lambda stored: json.dumps({**stored, "log": "PASS only\n"})),
            ("another file", lambda stored: json.dumps({**stored, "file": "internal/gateway/x.go"})),
            ("commit not a sha", lambda stored: json.dumps({**stored, "commit": "HEAD"})),
        ):
            with self.subTest(label):
                self.measure()
                stored = json.loads(entry.read_text(encoding="utf-8"))
                entry.write_text(damage(stored), encoding="utf-8")
                self.calls.clear()
                measured = self.measure()
                self.assertEqual(len(self.calls), 1)
                self.assertNotIn("reused_from", measured)

    def test_a_failed_run_is_logged_and_never_cached(self) -> None:
        self.output = (2, "panic: boom\n")
        with self.assertRaisesRegex(RuntimeError, "gateway: go-mutesting exited 2"):
            self.measure()
        self.assertEqual(
            (self.log_dir / "gateway.log").read_text(encoding="utf-8"), "panic: boom\n"
        )
        self.assertFalse((self.cache_dir / "gateway.json").exists())

    def test_an_unparseable_run_is_never_cached(self) -> None:
        self.output = (0, "PASS only\n")
        with self.assertRaisesRegex(ValueError, "summary"):
            self.measure()
        self.assertFalse((self.cache_dir / "gateway.json").exists())

    def test_a_below_floor_result_is_reused_as_measured(self) -> None:
        # Identical inputs give the identical verdict: a weak scope stays weak until
        # something in its closure changes, rather than being re-rolled for a pass.
        self.output = (0, mutesting_log(killed=1, survived=9))
        self.measure()
        reused = self.measure(commit="2" * 40)
        self.assertEqual(reused["score_percent"], 10.0)
        self.assertEqual(len(self.calls), 1)


class GateReportTest(unittest.TestCase):
    def test_the_gate_scope_carries_the_reuse_provenance(self) -> None:
        measured = {
            **go_mutation_cache.parse_go_mutation_output(mutesting_log()),
            "fingerprint": "f" * 64,
            "duration_seconds": 0.4,
            "reused_from": "1" * 40,
        }
        scope = critical_mutation_gate.scope("gateway", ["internal/gateway/classify.go"], measured)
        self.assertIs(scope["executed"], True)
        self.assertEqual(scope["reused_from"], "1" * 40)
        self.assertEqual(scope["fingerprint"], "f" * 64)
        self.assertEqual(scope["duration_seconds"], 0.4)
        failures = critical_mutation_gate.scope_failures([scope], 70.0)
        self.assertFalse([failure for failure in failures if failure.startswith("gateway")])


class CommandLineTest(unittest.TestCase):
    def run_cli(self, argv: list[str], results: dict[str, object]) -> tuple[int, str]:
        def measure_scope(scope_id: str, relative_path: str) -> dict[str, object]:
            result = results[relative_path]
            if isinstance(result, Exception):
                raise result
            return result  # type: ignore[return-value]

        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            code = go_mutation_cache.main(argv, measure_scope=measure_scope)
        return code, out.getvalue()

    @staticmethod
    def measured(killed: int, survived: int, reused_from: str | None = None) -> dict[str, object]:
        measured: dict[str, object] = {
            **go_mutation_cache.parse_go_mutation_output(mutesting_log(killed, survived)),
            "fingerprint": "f" * 64,
            "duration_seconds": 1.0,
        }
        if reused_from:
            measured["reused_from"] = reused_from
        return measured

    ARGS = [
        "internal/skills/validator.go",
        "--advisory",
        "internal/skills/writer.go",
        "--cache-dir",
        "unused-cache",
        "--log-dir",
        "unused-logs",
    ]

    def test_hard_and_advisory_files_above_the_floor_pass(self) -> None:
        code, out = self.run_cli(
            self.ARGS,
            {
                "internal/skills/validator.go": self.measured(33, 0, reused_from="1" * 40),
                "internal/skills/writer.go": self.measured(71, 11),
            },
        )
        self.assertEqual(code, 0, out)
        self.assertIn("ok: mutation score 100.00% >= 70% for internal/skills/validator.go (hard gate", out)
        self.assertIn("reused from " + "1" * 40, out)
        self.assertIn("ok: mutation score 86.59% >= 70% for internal/skills/writer.go (advisory", out)

    def test_a_hard_file_below_the_floor_fails(self) -> None:
        code, out = self.run_cli(
            self.ARGS,
            {
                "internal/skills/validator.go": self.measured(6, 4),
                "internal/skills/writer.go": self.measured(71, 11),
            },
        )
        self.assertEqual(code, 1)
        self.assertIn("FAIL: mutation score 60.00% < 70% for internal/skills/validator.go", out)

    def test_an_advisory_file_below_the_floor_or_broken_does_not_fail(self) -> None:
        for writer in (self.measured(25, 41), RuntimeError("writer: go-mutesting exited 2")):
            with self.subTest(writer=type(writer).__name__):
                code, out = self.run_cli(
                    self.ARGS,
                    {
                        "internal/skills/validator.go": self.measured(33, 0),
                        "internal/skills/writer.go": writer,
                    },
                )
                self.assertEqual(code, 0, out)
                self.assertIn("advisory:", out)
                self.assertIn("internal/skills/writer.go", out)

    def test_a_hard_file_that_cannot_be_measured_fails(self) -> None:
        code, out = self.run_cli(
            self.ARGS,
            {
                "internal/skills/validator.go": ValueError("go-mutesting output has no mutation summary"),
                "internal/skills/writer.go": self.measured(71, 11),
            },
        )
        self.assertEqual(code, 1)
        self.assertIn("FAIL: internal/skills/validator.go", out)

    def test_an_advisory_only_invocation_scores_without_gating(self) -> None:
        # One file per parallel job: the writer job carries no hard file of its own.
        code, out = self.run_cli(
            ["--advisory", "internal/skills/writer.go", "--cache-dir", "c", "--log-dir", "l"],
            {"internal/skills/writer.go": self.measured(25, 41)},
        )
        self.assertEqual(code, 0, out)
        self.assertIn("advisory: mutation score 37.88% < 70% for internal/skills/writer.go", out)

    def test_an_invocation_without_any_file_is_refused(self) -> None:
        with self.assertRaises(SystemExit), contextlib.redirect_stderr(io.StringIO()):
            self.run_cli(["--cache-dir", "c", "--log-dir", "l"], {})


if __name__ == "__main__":
    unittest.main()
