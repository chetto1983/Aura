"""Merge real + synthetic traces into train/val/test JSONL.

Each output row is self-contained for SFT:

    {"messages": [...normal-form trace...],
     "tools":    [...default manifest tooldefs...],
     "source":   "real" | "synthetic"}

The actual chat-template *text* is rendered later (train.py / evaluate.py) by
the model's own tokenizer, so this stage stays stdlib-only and the dataset is
tokenizer-agnostic. Dedup is by trace fingerprint so a real trace and a
near-identical synthetic one don't both survive. The split is deterministic
(seeded) and stratified so each split keeps the real/synthetic mix.
"""

from __future__ import annotations

import json
import random
from pathlib import Path

from . import config as cfgmod
from .format import Trace, split_at_assistant_calls, trace_fingerprint
from .tools import ToolCatalog


def _load_jsonl(path: Path) -> list[dict]:
    if not path.exists():
        return []
    rows = []
    for line in path.read_text().splitlines():
        line = line.strip()
        if line:
            rows.append(json.loads(line))
    return rows


def _usable(trace: Trace) -> bool:
    """Keep only traces with at least one assistant tool call to learn from."""
    return bool(split_at_assistant_calls(trace))


def build(cfg: cfgmod.Config) -> dict[str, int]:
    raw = cfg.path("raw_dir")
    rows = _load_jsonl(raw / "real.jsonl") + _load_jsonl(raw / "synthetic.jsonl")

    catalog = ToolCatalog.load(cfg.path("tools_json"))
    tools = catalog.default_tooldefs()

    seen: set[str] = set()
    examples: list[dict] = []
    for row in rows:
        trace = row.get("messages") or []
        if not _usable(trace):
            continue
        fp = trace_fingerprint(trace)
        if fp in seen:
            continue
        seen.add(fp)
        examples.append({"messages": trace, "tools": tools, "source": row.get("source", "unknown")})

    rng = random.Random(cfg.seed)
    by_source: dict[str, list[dict]] = {}
    for ex in examples:
        by_source.setdefault(ex["source"], []).append(ex)

    train: list[dict] = []
    val: list[dict] = []
    test: list[dict] = []
    for group in by_source.values():
        rng.shuffle(group)
        n = len(group)
        n_train = int(n * cfg.train_frac)
        n_val = int(n * cfg.val_frac)
        train += group[:n_train]
        val += group[n_train : n_train + n_val]
        test += group[n_train + n_val :]
    rng.shuffle(train)

    out_dir = cfg.path("dataset_dir")
    out_dir.mkdir(parents=True, exist_ok=True)
    counts = {}
    for name, split in (("train", train), ("val", val), ("test", test)):
        path = out_dir / f"{name}.jsonl"
        with path.open("w") as f:
            for ex in split:
                f.write(json.dumps(ex, ensure_ascii=False) + "\n")
        counts[name] = len(split)
        print(f"  {name}: {len(split)} -> {path}")
    counts["total"] = len(examples)
    return counts


def main(argv: list[str] | None = None) -> int:
    import argparse

    ap = argparse.ArgumentParser(description="Build the SFT dataset from harvested + synthetic traces.")
    ap.add_argument("--config", default=None)
    args = ap.parse_args(argv)

    cfg = cfgmod.load(args.config)
    counts = build(cfg)
    print(f"dataset built: {counts}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
