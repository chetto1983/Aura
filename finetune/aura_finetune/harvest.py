"""Harvest real tool-calling traces from Aura's Postgres (read-only).

Reconstructs normalized traces from `aura.conversation_turns`: an assistant
row's `tool_calls` jsonb is the OpenAI wire shape
(`[{id,type,function:{name,arguments}}]`); the following `tool` rows carry the
result preview keyed by `tool_call_id`. We resolve each tool result back to its
tool name via the id map built from the assistant turn.

Only conversations that contain at least one assistant tool call are kept — a
pure-chat thread teaches nothing about tool dispatch.

This is the "real" half of the hybrid dataset. It issues SELECTs only.
"""

from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Any, Iterable

from . import config as cfgmod
from .format import Trace, normalize_arguments

_LIST_CONVERSATIONS = """
SELECT c.id
FROM aura.conversations c
WHERE EXISTS (
    SELECT 1 FROM aura.conversation_turns t
    WHERE t.conversation_id = c.id AND t.role = 'assistant' AND t.tool_calls IS NOT NULL
)
ORDER BY c.last_active_at DESC
LIMIT %s;
"""

_LIST_TURNS = """
SELECT seq, role, content, content_sidecar_path, tool_call_id, tool_calls
FROM aura.conversation_turns
WHERE conversation_id = %s
ORDER BY seq ASC;
"""


def _parse_wire_tool_calls(raw: Any) -> list[dict]:
    """OpenAI wire tool_calls -> normal form [{name, arguments(dict)}]."""
    if raw is None:
        return []
    if isinstance(raw, (str, bytes)):
        raw = json.loads(raw)
    calls = []
    for c in raw or []:
        fn = c.get("function", c)
        calls.append(
            {
                "id": c.get("id", ""),
                "name": fn.get("name", ""),
                "arguments": normalize_arguments(fn.get("arguments")),
            }
        )
    return calls


def _turn_content(content: Any, sidecar: Any) -> str:
    if content:
        return content
    if sidecar:
        # Spillover lives in a sidecar FILE on the Aura host; if reachable, read
        # it, else keep an explicit marker so the trace stays honest.
        p = Path(str(sidecar))
        if p.exists():
            try:
                return p.read_text(errors="replace")
            except OSError:
                pass
        return f"[spilled output unavailable: {sidecar}]"
    return ""


def _rows_to_trace(rows: Iterable[tuple]) -> Trace:
    trace: Trace = []
    id_to_name: dict[str, str] = {}
    for seq, role, content, sidecar, tool_call_id, tool_calls in rows:
        if role == "assistant":
            calls = _parse_wire_tool_calls(tool_calls)
            for c in calls:
                if c.get("id"):
                    id_to_name[c["id"]] = c["name"]
            entry: dict[str, Any] = {"role": "assistant"}
            text = _turn_content(content, sidecar)
            if text:
                entry["content"] = text
            if calls:
                entry["tool_calls"] = [
                    {"name": c["name"], "arguments": c["arguments"]} for c in calls
                ]
            trace.append(entry)
        elif role == "tool":
            trace.append(
                {
                    "role": "tool",
                    "name": id_to_name.get(tool_call_id or "", ""),
                    "content": _turn_content(content, sidecar),
                }
            )
        else:  # system / user
            trace.append({"role": role, "content": _turn_content(content, sidecar)})
    return trace


def harvest(cfg: cfgmod.Config) -> list[Trace]:
    try:
        import psycopg  # type: ignore
    except ModuleNotFoundError as exc:  # pragma: no cover - dependency guard
        raise SystemExit(
            "psycopg is required for harvesting — `pip install -r requirements.txt`"
        ) from exc

    dsn = os.environ.get(cfg.pg_dsn_env)
    if not dsn:
        raise SystemExit(
            f"{cfg.pg_dsn_env} is not set; cannot harvest real traces. "
            "Set it to the Aura Postgres DSN, or skip harvest and rely on synthetic data."
        )

    traces: list[Trace] = []
    with psycopg.connect(dsn) as conn:  # read-only usage; no writes issued
        with conn.cursor() as cur:
            cur.execute(_LIST_CONVERSATIONS, (cfg.harvest_limit,))
            conv_ids = [r[0] for r in cur.fetchall()]
        for cid in conv_ids:
            with conn.cursor() as cur:
                cur.execute(_LIST_TURNS, (cid,))
                rows = cur.fetchall()
            trace = _rows_to_trace(rows)
            if any(m.get("role") == "assistant" and m.get("tool_calls") for m in trace):
                traces.append(trace)
    return traces


def main(argv: list[str] | None = None) -> int:
    import argparse

    ap = argparse.ArgumentParser(description="Harvest real Aura tool-calling traces.")
    ap.add_argument("--config", default=None)
    ap.add_argument("--out", default=None, help="JSONL output (default: <raw_dir>/real.jsonl)")
    args = ap.parse_args(argv)

    cfg = cfgmod.load(args.config)
    out = Path(args.out) if args.out else cfg.path("raw_dir") / "real.jsonl"
    out.parent.mkdir(parents=True, exist_ok=True)

    traces = harvest(cfg)
    with out.open("w") as f:
        for t in traces:
            f.write(json.dumps({"messages": t, "source": "real"}) + "\n")
    print(f"harvested {len(traces)} real traces -> {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
