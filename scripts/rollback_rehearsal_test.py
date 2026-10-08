from __future__ import annotations

import argparse
import pathlib
import tempfile
import unittest
from unittest import mock

import rollback_rehearsal


class RollbackRehearsalTest(unittest.TestCase):
    def args(self, root: pathlib.Path) -> argparse.Namespace:
        return argparse.Namespace(
            previous_image="aura:previous",
            candidate_image="aura:candidate",
            compose_file=root / "compose.yaml",
            health_url="http://127.0.0.1:9080/healthz",
            container="aura",
            timeout_seconds=30.0,
            output=root / "rollback-report.json",
        )

    def rehearse(self, heads: dict[str, int], fail_on: tuple[str, str] | None = None):
        """Run the rehearsal against a fake stack.

        Returns the report (or the raised exception), the (image, database) deploys in
        order, and every postgres command. heads maps an image to the migration head its
        `aura db status` reports.
        """
        prev_db, restore_db = (
            rollback_rehearsal.PREVIOUS_DATABASE, rollback_rehearsal.RESTORE_DATABASE)
        deploys: list[tuple[str, str]] = []
        pg: list[tuple[str, ...]] = []
        result: object = None

        def command(image: str, command: list[str], _: pathlib.Path, database: str = "") -> str:
            if command[-1] == "aura":
                deploys.append((image, database))
                if fail_on == (image, database):
                    raise RuntimeError(f"{image} failed on {database or 'default'}")
            if command[-1] == "aura db status":
                return f"2026/10/08 INFO starting\nVERSION  DIRTY\n{heads[image]}  false\n"
            return ""

        with tempfile.TemporaryDirectory() as raw:
            root = pathlib.Path(raw)
            (root / "compose.yaml").write_text("services: {}\n", encoding="utf-8")
            with (
                mock.patch.object(rollback_rehearsal, "image_digest",
                                  side_effect=["sha256:" + "1" * 64, "sha256:" + "2" * 64]),
                mock.patch.object(rollback_rehearsal, "candidate_commit", return_value="a" * 40),
                mock.patch.object(rollback_rehearsal, "run_for_image", command),
                mock.patch.object(rollback_rehearsal, "wait_deployment"),
                mock.patch.object(rollback_rehearsal, "postgres",
                                  lambda _f, _r, args: pg.append(tuple(args))),
            ):
                try:
                    result = rollback_rehearsal.run_rehearsal(self.args(root))
                except (RuntimeError, ValueError) as exc:
                    result = exc
        self.assertEqual((prev_db, restore_db), ("aura_rollback_previous", "aura_rollback_restore"))
        return result, deploys, pg

    def test_a_schema_change_rolls_back_by_restoring_the_previous_dump(self) -> None:
        report, deploys, pg = self.rehearse({"aura:previous": 112, "aura:candidate": 138})
        prev_db, restore_db = "aura_rollback_previous", "aura_rollback_restore"

        self.assertTrue(report["passed"])
        self.assertEqual(report["rollback_mode"], "restore")
        self.assertEqual((report["previous_migration_head"], report["candidate_migration_head"]), (112, 138))
        self.assertTrue(report["upgrade_healthy"] and report["restore_verified"])
        self.assertFalse(report["migrations_compatible"])
        self.assertTrue(report["config_started"] and report["readiness_healthy"] and report["candidate_restored"])
        self.assertIn("pg_dump", report["rollback_command"])
        # previous on its own database, the upgrade on that database, the previous image on
        # the restored dump, and the candidate back on the stack's database.
        self.assertEqual(deploys, [
            ("aura:previous", prev_db), ("aura:candidate", prev_db),
            ("aura:previous", restore_db), ("aura:candidate", ""),
        ])
        dump = next(i for i, c in enumerate(pg) if c[0] == "pg_dump")
        restore = next(i for i, c in enumerate(pg) if c[0] == "pg_restore")
        self.assertIn(prev_db, pg[dump])
        self.assertIn(restore_db, pg[restore])
        self.assertLess(dump, restore)
        self.assertTrue(any(f'DROP DATABASE IF EXISTS "{restore_db}"' in " ".join(c) for c in pg[restore:]))

    def test_a_shared_head_rolls_back_by_swapping_the_image(self) -> None:
        report, deploys, pg = self.rehearse({"aura:previous": 138, "aura:candidate": 138})
        prev_db = "aura_rollback_previous"

        self.assertTrue(report["passed"])
        self.assertEqual(report["rollback_mode"], "swap")
        self.assertTrue(report["migrations_compatible"])
        self.assertFalse(report["restore_verified"])
        self.assertEqual(deploys, [
            ("aura:previous", prev_db), ("aura:candidate", prev_db),
            ("aura:previous", prev_db), ("aura:candidate", ""),
        ])
        self.assertFalse(any(c[0] == "pg_restore" for c in pg))

    def test_failure_still_restores_the_candidate_and_drops_the_rehearsal_databases(self) -> None:
        restore_db = "aura_rollback_restore"
        error, deploys, pg = self.rehearse(
            {"aura:previous": 112, "aura:candidate": 138}, fail_on=("aura:previous", restore_db))

        self.assertIsInstance(error, RuntimeError)
        self.assertIn("failed on aura_rollback_restore", str(error))
        self.assertEqual(deploys[-1], ("aura:candidate", ""))
        dropped = " ".join(" ".join(c) for c in pg if c[0] == "psql")
        self.assertIn('DROP DATABASE IF EXISTS "aura_rollback_previous" WITH (FORCE)', dropped)
        self.assertIn(f'DROP DATABASE IF EXISTS "{restore_db}" WITH (FORCE)', dropped)
        self.assertIn(("rm", "-f", rollback_rehearsal.DUMP_PATH), pg)

    def test_a_previous_image_past_the_candidate_head_is_refused(self) -> None:
        error, deploys, _ = self.rehearse({"aura:previous": 139, "aura:candidate": 138})

        self.assertIsInstance(error, ValueError)
        self.assertIn("past the candidate", str(error))
        self.assertEqual(deploys, [("aura:candidate", "")])

    def test_container_logs_keep_the_boot_error_past_the_readiness_retries(self) -> None:
        mount = '{"time":"2026-10-08T10:00:29Z","level":"WARN","msg":"mcp mount failed","err":"401"}'
        probes = [
            f'{{"time":"2026-10-08T10:{m:02d}:00Z","level":"WARN","msg":"agui: readiness probe failed"}}'
            for m in range(1, 59)
        ]
        info = [f'{{"time":"2026-10-08T10:{m:02d}:30Z","level":"INFO","msg":"tick"}}' for m in range(300)]
        outputs = {
            "aura": "\n".join([mount, *probes, *info]),
            "aura-arcadedb-mcp": "sidecar: token issuer not trusted",
        }

        def run(command: list[str], **_: object) -> mock.Mock:
            return mock.Mock(returncode=0, stdout=outputs[command[-1]])

        with mock.patch.object(rollback_rehearsal.subprocess, "run", side_effect=run):
            detail = rollback_rehearsal.container_logs("aura", pathlib.Path("."))

        self.assertIn('"msg":"mcp mount failed"', detail)
        self.assertEqual(detail.count("agui: readiness probe failed"), 1)
        self.assertIn("sidecar: token issuer not trusted", detail)
        self.assertNotIn('"msg":"tick"', detail.split("-- tail --")[0])

    def test_postgres_passes_the_password_by_environment_never_argv(self) -> None:
        completed = mock.Mock(returncode=0, stdout="")
        with (
            mock.patch.dict(rollback_rehearsal.os.environ, {"POSTGRES_PASSWORD": "s3cret"}, clear=True),
            mock.patch.object(rollback_rehearsal.subprocess, "run", return_value=completed) as run,
        ):
            rollback_rehearsal.postgres(pathlib.Path("compose.yaml"), pathlib.Path("."), ["psql", "-c", "SELECT 1"])
        argv, kwargs = run.call_args.args[0], run.call_args.kwargs
        self.assertNotIn("s3cret", " ".join(argv))
        self.assertEqual(argv[argv.index("exec"):argv.index("exec") + 5], ["exec", "-T", "-e", "PGPASSWORD", "postgres"])
        self.assertEqual(kwargs["env"]["PGPASSWORD"], "s3cret")

    def test_migration_head_reads_one_clean_tracker_row(self) -> None:
        self.assertEqual(rollback_rehearsal.migration_head("VERSION  DIRTY\n138  false\n"), 138)
        for output, message in (
            ("VERSION  DIRTY\n138  true\n", "dirty"),
            ("ok: no migrations applied yet\n", "one tracker row"),
            ("VERSION  DIRTY\n137  false\n138  false\n", "one tracker row"),
        ):
            with self.subTest(output=output):
                with self.assertRaisesRegex(RuntimeError, message):
                    rollback_rehearsal.migration_head(output)

    def test_rejects_identical_images(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            root = pathlib.Path(raw)
            (root / "compose.yaml").write_text("services: {}\n", encoding="utf-8")
            with mock.patch.object(
                rollback_rehearsal,
                "image_digest",
                return_value="sha256:" + "1" * 64,
            ):
                with self.assertRaisesRegex(ValueError, "distinct"):
                    rollback_rehearsal.run_rehearsal(self.args(root))

    def test_container_health_requires_healthy_status(self) -> None:
        completed = mock.Mock(returncode=0, stdout="healthy\n", stderr="")
        with mock.patch.object(
            rollback_rehearsal.subprocess, "run", return_value=completed
        ):
            self.assertEqual(
                rollback_rehearsal.container_health("aura", pathlib.Path(".")),
                "healthy",
            )
        completed.stdout = "starting\n"
        with mock.patch.object(
            rollback_rehearsal.subprocess, "run", return_value=completed
        ):
            self.assertEqual(
                rollback_rehearsal.container_health("aura", pathlib.Path(".")),
                "starting",
            )

    def test_wait_deployment_fails_fast_with_container_logs(self) -> None:
        with (
            mock.patch.object(
                rollback_rehearsal,
                "wait_endpoint",
                side_effect=RuntimeError("endpoint refused"),
            ),
            mock.patch.object(
                rollback_rehearsal.urllib.request,
                "urlopen",
                side_effect=OSError("connection refused"),
            ),
            mock.patch.object(
                rollback_rehearsal, "container_health", return_value="exited"
            ),
            mock.patch.object(
                rollback_rehearsal,
                "container_logs",
                create=True,
                return_value="OPENROUTER_API_KEY is required",
            ),
        ):
            with self.assertRaisesRegex(
                RuntimeError, "OPENROUTER_API_KEY is required"
            ):
                rollback_rehearsal.wait_deployment(
                    "http://127.0.0.1:9080/healthz",
                    "aura",
                    30.0,
                    pathlib.Path("."),
                )


    def test_bootstrap_declares_the_absent_previous_image(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            root = pathlib.Path(raw)
            args = self.args(root)
            args.previous_image = ""
            with (
                mock.patch.object(
                    rollback_rehearsal, "image_digest", return_value="sha256:" + "c" * 64
                ) as digest,
                mock.patch.object(
                    rollback_rehearsal, "candidate_commit", return_value="a" * 40
                ),
            ):
                report = rollback_rehearsal.run_bootstrap(args)

        self.assertTrue(report["passed"])
        self.assertTrue(report["bootstrap"])
        self.assertIsNone(report["previous_image"])
        self.assertIsNone(report["previous_image_digest"])
        self.assertEqual(report["candidate_image_digest"], "sha256:" + "c" * 64)
        self.assertTrue(report["bootstrap_reason"].strip())
        # Only the candidate is resolved: bootstrap must never touch a previous image.
        digest.assert_called_once_with("aura:candidate", mock.ANY)

    def test_bootstrap_rejects_a_previous_image(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            args = self.args(pathlib.Path(raw))
            with self.assertRaises(ValueError):
                rollback_rehearsal.run_bootstrap(args)

    def test_rehearsal_without_a_previous_image_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            root = pathlib.Path(raw)
            (root / "compose.yaml").write_text("services: {}\n", encoding="utf-8")
            args = self.args(root)
            args.previous_image = ""
            with self.assertRaises(ValueError):
                rollback_rehearsal.run_rehearsal(args)


if __name__ == "__main__":
    unittest.main()
