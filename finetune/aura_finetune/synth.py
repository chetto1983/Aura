"""Synthetic trace generation by teacher distillation.

This is the distil-labs move: a large teacher (DeepSeek-V4 via OpenRouter, the
same model Aura runs in production) writes multi-turn tool-calling traces over
Aura's *real* tool surface, which then supervise the 270M student. Each seed is
a user goal; the teacher emits a complete, valid trace in the pipeline's normal
form, including Aura's signature deferred-tool -> tool_search(select:...) ->
loaded-schema dance.

Every generated trace is verified before it is kept: tool names must exist and
each tool call's arguments must validate against that tool's JSON schema.
Invalid traces are dropped (a small specialized model learns from clean data,
not from the teacher's occasional malformed call).
"""

from __future__ import annotations

import json
import os
from pathlib import Path
from typing import Any

from . import config as cfgmod
from .format import Trace, normalize_arguments
from .tools import ToolCatalog

_SYSTEM = """You generate TRAINING DATA for a small tool-calling model that drives the Aura agent.

Aura's tool protocol (study it, then imitate it exactly):
- On a fresh turn the model sees a DEFAULT MANIFEST: each tool's name + one-line summary only.
- Tools marked DEFERRED hide their full schema. To use a deferred tool the model MUST first call
  `tool_search` with arguments {"query": "select:Name1,Name2"} to load the full schemas.
- After tool_search returns, the model calls the now-loaded tool with arguments matching its schema.
- Non-deferred tools (e.g. text_response, ask_user, tool_search, task, skill, todo) can be called directly.
- A tool result comes back as a role:"tool" message. The model continues until the goal is done,
  ending with a `text_response` tool call carrying the final answer to the user.

Output ONE realistic multi-turn conversation as a JSON object:
{"messages": [ ...turns... ]}

Turn shapes (use EXACTLY these keys):
  {"role":"user","content":"..."}
  {"role":"assistant","tool_calls":[{"name":"<tool>","arguments":{...}}]}
  {"role":"tool","name":"<tool>","content":"<plausible result text>"}
  {"role":"assistant","tool_calls":[{"name":"text_response","arguments":{"text":"<final answer>"}}]}

Rules:
- Realistic, concrete arguments and plausible tool outputs. Vary phrasing and values across samples.
- Respect the deferred -> tool_search(select:...) -> call ordering for any DEFERRED tool you use.
- Every tool name must be one of the provided tools. Arguments must satisfy the tool's JSON schema.
- Keep it to at most {max_turns} assistant turns. Output ONLY the JSON object, no prose, no markdown fence.
"""


def _manifest_block(catalog: ToolCatalog) -> str:
    lines = ["DEFAULT MANIFEST (name — summary, [DEFERRED] if schema hidden):"]
    for s in sorted(catalog._specs, key=lambda x: x.name):  # noqa: SLF001 - internal view
        tag = " [DEFERRED]" if s.deferred else ""
        lines.append(f"- {s.name}{tag} — {s.summary}")
    return "\n".join(lines)


def _schema_block(catalog: ToolCatalog, names: list[str]) -> str:
    blocks = ["FULL SCHEMAS for the tools relevant to this seed:"]
    for n in names:
        spec = catalog.get(n)
        if spec is None:
            continue
        blocks.append(f"### {n}\n{spec.description}\nparameters: {json.dumps(spec.parameters)}")
    return "\n\n".join(blocks)


def _extract_json_object(text: str) -> dict | None:
    text = text.strip()
    if text.startswith("```"):
        text = text.strip("`")
        text = text[text.find("{"):]
    start = text.find("{")
    if start < 0:
        return None
    depth = 0
    for i in range(start, len(text)):
        if text[i] == "{":
            depth += 1
        elif text[i] == "}":
            depth -= 1
            if depth == 0:
                try:
                    return json.loads(text[start : i + 1])
                except json.JSONDecodeError:
                    return None
    return None


def _validate_trace(trace: Trace, catalog: ToolCatalog) -> bool:
    try:
        import jsonschema  # type: ignore

        validate = jsonschema.validate
    except ModuleNotFoundError:
        validate = None  # name-only validation if jsonschema absent

    saw_call = False
    for msg in trace:
        if msg.get("role") != "assistant":
            continue
        for call in msg.get("tool_calls", []):
            name = call.get("name", "")
            spec = catalog.get(name)
            if spec is None:
                return False
            saw_call = True
            if validate is not None:
                try:
                    validate(instance=normalize_arguments(call.get("arguments")), schema=spec.parameters)
                except Exception:  # noqa: BLE001 - any schema failure rejects the trace
                    return False
    return saw_call


def _seed_tool_names(seed: dict, catalog: ToolCatalog) -> list[str]:
    names = seed.get("tools") or []
    names = [n for n in names if catalog.get(n) is not None]
    if "text_response" not in names:
        names.append("text_response")
    if "tool_search" not in names:
        names.append("tool_search")
    return names


def generate(cfg: cfgmod.Config) -> list[Trace]:
    try:
        from openai import OpenAI  # type: ignore
    except ModuleNotFoundError as exc:  # pragma: no cover - dependency guard
        raise SystemExit(
            "openai is required for synthetic generation — `pip install -r requirements.txt`"
        ) from exc

    api_key = os.environ.get(cfg.teacher_api_key_env)
    if not api_key:
        raise SystemExit(
            f"{cfg.teacher_api_key_env} is not set; cannot reach the teacher model."
        )

    catalog = ToolCatalog.load(cfg.path("tools_json"))
    client = OpenAI(base_url=cfg.teacher_base_url, api_key=api_key)

    seeds = [
        json.loads(line)
        for line in cfg.path("seeds_path").read_text().splitlines()
        if line.strip() and not line.lstrip().startswith("#")
    ]

    system = _SYSTEM.replace("{max_turns}", str(cfg.synth_max_turns))
    manifest = _manifest_block(catalog)

    traces: list[Trace] = []
    kept = dropped = 0
    for seed in seeds:
        names = _seed_tool_names(seed, catalog)
        schemas = _schema_block(catalog, names)
        user = (
            f"{manifest}\n\n{schemas}\n\n"
            f"SEED GOAL: {seed['goal']}\n"
            f"Generate a distinct realistic conversation that accomplishes this goal."
        )
        for _ in range(cfg.synth_samples_per_seed):
            try:
                resp = client.chat.completions.create(
                    model=cfg.teacher_model,
                    temperature=cfg.synth_temperature,
                    messages=[{"role": "system", "content": system}, {"role": "user", "content": user}],
                )
            except Exception as exc:  # noqa: BLE001 - network/provider hiccup, keep going
                print(f"  teacher error on seed '{seed['goal'][:40]}...': {exc}")
                dropped += 1
                continue
            obj = _extract_json_object(resp.choices[0].message.content or "")
            trace = obj.get("messages") if isinstance(obj, dict) else None
            if isinstance(trace, list) and _validate_trace(trace, catalog):
                traces.append(trace)
                kept += 1
            else:
                dropped += 1
    print(f"synthetic: kept {kept}, dropped {dropped} (invalid/unparseable)")
    return traces


def main(argv: list[str] | None = None) -> int:
    import argparse

    ap = argparse.ArgumentParser(description="Generate synthetic Aura tool-calling traces.")
    ap.add_argument("--config", default=None)
    ap.add_argument("--out", default=None, help="JSONL output (default: <raw_dir>/synthetic.jsonl)")
    args = ap.parse_args(argv)

    cfg = cfgmod.load(args.config)
    out = Path(args.out) if args.out else cfg.path("raw_dir") / "synthetic.jsonl"
    out.parent.mkdir(parents=True, exist_ok=True)

    traces = generate(cfg)
    with out.open("w") as f:
        for t in traces:
            f.write(json.dumps({"messages": t, "source": "synthetic"}) + "\n")
    print(f"wrote {len(traces)} synthetic traces -> {out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
