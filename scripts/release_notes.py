#!/usr/bin/env python3
"""Print the CHANGELOG.md section of one release tag, for goreleaser --release-notes.

Without it the GitHub release body is goreleaser's commit list: about 1,700 lines
for v1.1.0, unreadable as release notes. A tag with no section fails the release
rather than publishing that list.
"""
from __future__ import annotations

import argparse
import pathlib
import re
import sys


def section(changelog: str, tag: str) -> str:
    heading = re.compile(rf"^## {re.escape(tag)}(\s|$)")
    lines = changelog.splitlines()
    start = next((i for i, line in enumerate(lines) if heading.match(line)), None)
    if start is None:
        raise ValueError(f"CHANGELOG.md has no '## {tag}' section")
    end = next(
        (i for i in range(start + 1, len(lines)) if lines[i].startswith("## ")),
        len(lines),
    )
    body = "\n".join(lines[start + 1 : end]).strip()
    if not body:
        raise ValueError(f"CHANGELOG.md section '## {tag}' is empty")
    return body + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("tag")
    parser.add_argument("--changelog", type=pathlib.Path, default=pathlib.Path("CHANGELOG.md"))
    args = parser.parse_args()
    try:
        sys.stdout.write(section(args.changelog.read_text(encoding="utf-8"), args.tag))
    except (OSError, ValueError) as exc:
        print(f"release-notes: FAIL: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
