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
            ready_url="http://127.0.0.1:9080/readyz",
            timeout_seconds=30.0,
            output=root / "rollback-report.json",
        )

    def test_rehearsal_rolls_back_then_restores_candidate(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            root = pathlib.Path(raw)
            (root / "compose.yaml").write_text("services: {}\n", encoding="utf-8")
            args = self.args(root)
            commands: list[tuple[str, tuple[str, ...]]] = []

            def command(image: str, command: list[str], _: pathlib.Path) -> None:
                commands.append((image, tuple(command)))

            with (
                mock.patch.object(
                    rollback_rehearsal,
                    "image_digest",
                    side_effect=["sha256:" + "1" * 64, "sha256:" + "2" * 64],
                ),
                mock.patch.object(
                    rollback_rehearsal, "candidate_commit", return_value="a" * 40
                ),
                mock.patch.object(rollback_rehearsal, "run_for_image", command),
                mock.patch.object(rollback_rehearsal, "wait_endpoints"),
            ):
                report = rollback_rehearsal.run_rehearsal(args)

        self.assertTrue(report["passed"])
        self.assertTrue(report["candidate_restored"])
        self.assertTrue(
            all(
                command[0:4]
                == (
                    "docker",
                    "compose",
                    "-f",
                    str((root / "compose.yaml").resolve()),
                )
                for _, command in commands
            )
        )
        self.assertEqual(
            [image for image, command in commands if command[-1] == "aura"],
            ["aura:candidate", "aura:previous", "aura:candidate"],
        )
        self.assertNotEqual(
            report["previous_image_digest"], report["candidate_image_digest"]
        )

    def test_failure_still_attempts_candidate_restore_and_fails_closed(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            root = pathlib.Path(raw)
            (root / "compose.yaml").write_text("services: {}\n", encoding="utf-8")
            args = self.args(root)
            deploys: list[str] = []

            def command(image: str, command: list[str], _: pathlib.Path) -> None:
                if command[-1] == "aura":
                    deploys.append(image)
                    if image == "aura:previous":
                        raise RuntimeError("previous image failed")

            with (
                mock.patch.object(
                    rollback_rehearsal,
                    "image_digest",
                    side_effect=["sha256:" + "1" * 64, "sha256:" + "2" * 64],
                ),
                mock.patch.object(
                    rollback_rehearsal, "candidate_commit", return_value="b" * 40
                ),
                mock.patch.object(rollback_rehearsal, "run_for_image", command),
                mock.patch.object(rollback_rehearsal, "wait_endpoints"),
            ):
                with self.assertRaisesRegex(RuntimeError, "previous image failed"):
                    rollback_rehearsal.run_rehearsal(args)

        self.assertEqual(
            deploys, ["aura:candidate", "aura:previous", "aura:candidate"]
        )
        self.assertFalse(args.output.exists())

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


if __name__ == "__main__":
    unittest.main()
