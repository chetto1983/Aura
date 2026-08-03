from __future__ import annotations

import hashlib
import json
import pathlib
import subprocess
from typing import Any

from evidence_metadata import candidate_commit


RUNTIME_MARKER = "AURA_AGENT_MEMORY_RUNTIME_JSON="


def _git_paths(repo: pathlib.Path, args: list[str]) -> list[str]:
    completed = subprocess.run(
        ["git", *args, "-z"],
        cwd=repo,
        text=False,
        capture_output=True,
        check=False,
    )
    if completed.returncode != 0:
        detail = completed.stderr.decode(errors="replace").strip() or "git returned no detail"
        raise RuntimeError(f"cannot inventory dirty candidate files: {detail}")
    return [item.decode(errors="surrogateescape") for item in completed.stdout.split(b"\0") if item]


def candidate_state(repo: pathlib.Path) -> dict[str, Any]:
    root = repo.resolve()
    paths = set(_git_paths(root, ["diff", "--name-only", "HEAD"]))
    paths.update(_git_paths(root, ["ls-files", "--others", "--exclude-standard"]))
    dirty_files: list[dict[str, Any]] = []
    for relative in sorted(paths):
        path = (root / relative).resolve()
        if not path.is_relative_to(root):
            raise RuntimeError(f"dirty candidate path escapes repository: {relative}")
        entry: dict[str, Any] = {"path": relative.replace("\\", "/")}
        if path.is_file():
            entry["sha256"] = hashlib.sha256(path.read_bytes()).hexdigest()
        else:
            entry["deleted"] = True
        dirty_files.append(entry)
    return {"commit": candidate_commit(root), "dirty_files": dirty_files}


def manifest_sha256(manifest: dict[str, Any]) -> str:
    encoded = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
    return hashlib.sha256(encoded).hexdigest()


def parse_runtime_marker(raw: str) -> dict[str, Any]:
    try:
        payload = json.loads(raw.strip())
    except json.JSONDecodeError as exc:
        raise ValueError(f"invalid runtime marker JSON: {exc}") from exc
    if not isinstance(payload, dict):
        raise ValueError("runtime marker root must be an object")
    for key in ("arcadedb_version", "mcp_server_version", "embedding_model"):
        if not isinstance(payload.get(key), str) or not payload[key].strip():
            raise ValueError(f"runtime marker needs non-empty {key}")
    if payload.get("embedding_dimension") != 768:
        raise ValueError("runtime marker must prove EmbeddingGemma width 768")
    return {
        "arcadedb_version": payload["arcadedb_version"].strip(),
        "mcp_server_version": payload["mcp_server_version"].strip(),
        "embedding_model": payload["embedding_model"].strip(),
        "embedding_dimension": 768,
    }
