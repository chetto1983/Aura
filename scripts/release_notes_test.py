from __future__ import annotations

import pathlib
import unittest

import release_notes

CHANGELOG = """# Changelog

Intro.

## v1.1.0 (2026-10-08)

### Upgrading

- Back up first.

## v1.1.0-rc1 (2026-10-01)

- Older.
"""


class ReleaseNotesTest(unittest.TestCase):
    def test_returns_only_the_tag_section_without_its_heading(self) -> None:
        self.assertEqual(
            release_notes.section(CHANGELOG, "v1.1.0"),
            "### Upgrading\n\n- Back up first.\n",
        )

    def test_a_tag_prefix_does_not_match_a_longer_tag(self) -> None:
        self.assertEqual(release_notes.section(CHANGELOG, "v1.1.0-rc1"), "- Older.\n")
        with self.assertRaisesRegex(ValueError, "no '## v1.1' section"):
            release_notes.section(CHANGELOG, "v1.1")

    def test_missing_or_empty_section_fails(self) -> None:
        with self.assertRaisesRegex(ValueError, "no '## v9.9.9' section"):
            release_notes.section(CHANGELOG, "v9.9.9")
        with self.assertRaisesRegex(ValueError, "is empty"):
            release_notes.section("## v1.0.0\n\n## v0.9.0\n- x\n", "v1.0.0")

    def test_the_repository_changelog_has_a_section_for_the_current_release(self) -> None:
        text = (pathlib.Path(__file__).resolve().parents[1] / "CHANGELOG.md").read_text(
            encoding="utf-8"
        )
        self.assertIn("### Upgrading from v1.0.2-rc1", release_notes.section(text, "v1.1.0"))


if __name__ == "__main__":
    unittest.main()
