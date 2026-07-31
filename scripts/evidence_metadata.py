from __future__ import annotations

import pathlib
import re
import subprocess


FULL_GIT_SHA = re.compile(r"^[0-9a-f]{40}$")


def candidate_commit(repo: pathlib.Path | None = None) -> str:
    root = repo or pathlib.Path(__file__).resolve().parents[1]
    completed = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=root,
        text=True,
        capture_output=True,
        check=False,
    )
    value = completed.stdout.strip()
    if completed.returncode != 0 or FULL_GIT_SHA.fullmatch(value) is None:
        detail = completed.stderr.strip() or value or "git returned no commit"
        raise RuntimeError(f"cannot bind evidence to candidate commit: {detail}")
    return value
