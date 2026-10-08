from __future__ import annotations

import unittest

from release_readiness_errors import GateError
from release_readiness_rollback import validate_rollback


def restore_report(**overrides: object) -> dict[str, object]:
    report: dict[str, object] = {
        "schema_version": 1,
        "passed": True,
        "previous_image_digest": "sha256:" + "b" * 64,
        "candidate_image_digest": "sha256:" + "c" * 64,
        "rollback_mode": "restore",
        "rollback_command": "restore the pre-upgrade pg_dump into a new database, then ...",
        "previous_migration_head": 112,
        "candidate_migration_head": 138,
        "config_started": True,
        "upgrade_healthy": True,
        "migrations_compatible": False,
        "restore_verified": True,
        "readiness_healthy": True,
    }
    report.update(overrides)
    return report


class RestoreRollbackTest(unittest.TestCase):
    def test_a_verified_restore_passes_without_migration_compatibility(self) -> None:
        self.assertEqual(
            validate_rollback(restore_report()),
            {
                "previous_image_digest": "sha256:" + "b" * 64,
                "candidate_image_digest": "sha256:" + "c" * 64,
                "rollback_mode": "restore",
                "previous_migration_head": 112,
                "candidate_migration_head": 138,
            },
        )

    def test_a_restore_fails_closed_on_any_missing_proof(self) -> None:
        for overrides, message in (
            ({"restore_verified": False}, "restore_verified"),
            ({"upgrade_healthy": False}, "upgrade_healthy"),
            ({"readiness_healthy": False}, "readiness_healthy"),
            ({"config_started": False}, "config_started"),
            ({"previous_migration_head": None}, "both migration heads"),
            ({"candidate_migration_head": True}, "both migration heads"),
            ({"previous_migration_head": 0}, "both migration heads"),
            ({"previous_migration_head": 138}, "past the previous head"),
            ({"previous_migration_head": 139}, "past the previous head"),
            ({"rollback_mode": "rewind"}, "unknown rollback_mode"),
        ):
            with self.subTest(overrides=overrides):
                with self.assertRaisesRegex(GateError, message):
                    validate_rollback(restore_report(**overrides))


class SwapRollbackTest(unittest.TestCase):
    def test_a_report_without_a_mode_is_a_swap_and_needs_migration_compatibility(self) -> None:
        report = restore_report(migrations_compatible=True)
        for field in ("rollback_mode", "upgrade_healthy", "restore_verified",
                      "previous_migration_head", "candidate_migration_head"):
            del report[field]
        self.assertEqual(validate_rollback(report)["rollback_mode"], "swap")
        with self.assertRaisesRegex(GateError, "migrations_compatible"):
            validate_rollback({**report, "migrations_compatible": False})

    def test_a_named_swap_also_needs_a_healthy_upgrade(self) -> None:
        swap = restore_report(rollback_mode="swap", migrations_compatible=True, restore_verified=False)
        self.assertEqual(validate_rollback(swap)["rollback_mode"], "swap")
        with self.assertRaisesRegex(GateError, "upgrade_healthy"):
            validate_rollback({**swap, "upgrade_healthy": False})
        with self.assertRaisesRegex(GateError, "migrations_compatible"):
            validate_rollback({**swap, "migrations_compatible": False})


if __name__ == "__main__":
    unittest.main()
