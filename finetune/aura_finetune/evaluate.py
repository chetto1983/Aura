"""Tool-call-equivalence evaluation — the distil-labs benchmark, on Aura tools.

For every assistant turn in the test set that issues tool calls, we feed the
model the conversation *up to* that turn (with a generation prompt) and check
whether its predicted call(s) match the gold call(s): same tool name(s), same
arguments (order- and whitespace-insensitive). The headline number is the
fraction of turns where prediction == gold.

Run it twice — `--base` (FunctionGemma out of the box) and `--adapter <dir>`
(after fine-tuning) — to reproduce the blog's pre/post jump.
"""

from __future__ import annotations

import json
import re
from pathlib import Path

from . import config as cfgmod
from .format import (
    Message,
    Trace,
    calls_equivalent,
    normalize_arguments,
    split_at_assistant_calls,
    to_chat_messages,
)

# FunctionGemma renders a call as <start_function_call>call:name{...}<end_function_call>.
_CALL_RE = re.compile(r"call:\s*([A-Za-z_][A-Za-z0-9_]*)\s*\{(.*?)\}", re.DOTALL)
_KV_RE = re.compile(r"([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(\"(?:[^\"\\]|\\.)*\"|'[^']*'|[^,}]*)")


def _coerce_value(raw: str):
    raw = raw.strip()
    if (raw.startswith('"') and raw.endswith('"')) or (raw.startswith("'") and raw.endswith("'")):
        inner = raw[1:-1]
        try:
            return json.loads(f'"{inner}"') if raw[0] == '"' else inner
        except json.JSONDecodeError:
            return inner
    for caster in (int, float):
        try:
            return caster(raw)
        except ValueError:
            pass
    if raw in {"true", "false"}:
        return raw == "true"
    if raw == "null":
        return None
    return raw


def parse_function_calls(text: str) -> list[dict]:
    """Best-effort parse of generated tool calls back to normal form.

    Handles both FunctionGemma's `name{k:v,...}` block and a JSON arguments
    block, so the metric is robust to template/quantization differences.
    """
    calls: list[dict] = []
    for name, body in _CALL_RE.findall(text):
        body = body.strip()
        args: dict
        try:
            parsed = json.loads("{" + body + "}") if body and not body.startswith("{") else json.loads(body or "{}")
            args = parsed if isinstance(parsed, dict) else {}
        except json.JSONDecodeError:
            args = {k: _coerce_value(v) for k, v in _KV_RE.findall(body)}
        calls.append({"name": name, "arguments": normalize_arguments(args)})
    return calls


def _generate(model, tokenizer, messages: list[Message], tools: list[dict], max_new_tokens: int) -> str:
    inputs = tokenizer.apply_chat_template(
        messages,
        tools=tools or None,
        add_generation_prompt=True,
        return_tensors="pt",
        return_dict=True,
    ).to(model.device)
    out = model.generate(**inputs, max_new_tokens=max_new_tokens, do_sample=False)
    gen = out[0][inputs["input_ids"].shape[1]:]
    return tokenizer.decode(gen, skip_special_tokens=False)


def evaluate(cfg: cfgmod.Config, *, adapter: str | None, max_new_tokens: int = 256) -> dict:
    from unsloth import FastLanguageModel  # type: ignore

    model_name = adapter or cfg.base_model
    model, tokenizer = FastLanguageModel.from_pretrained(
        model_name=model_name,
        max_seq_length=cfg.max_seq_len,
        load_in_16bit=True,
    )
    FastLanguageModel.for_inference(model)

    rows = [
        json.loads(l)
        for l in (cfg.path("dataset_dir") / "test.jsonl").read_text().splitlines()
        if l.strip()
    ]

    total = correct = 0
    per_source: dict[str, list[int]] = {}
    for row in rows:
        trace: Trace = row["messages"]
        tools = row.get("tools", [])
        src = row.get("source", "unknown")
        for prefix, gold in split_at_assistant_calls(trace):
            messages = to_chat_messages(prefix)
            text = _generate(model, tokenizer, messages, tools, max_new_tokens)
            pred: Message = {"role": "assistant", "tool_calls": parse_function_calls(text)}
            ok = int(calls_equivalent(pred, gold))
            total += 1
            correct += ok
            per_source.setdefault(src, [0, 0])
            per_source[src][0] += ok
            per_source[src][1] += 1

    report = {
        "model": model_name,
        "tool_call_equivalence": (correct / total) if total else 0.0,
        "n_turns": total,
        "by_source": {
            s: {"accuracy": (c / n) if n else 0.0, "n": n} for s, (c, n) in per_source.items()
        },
    }
    return report


def main(argv: list[str] | None = None) -> int:
    import argparse

    ap = argparse.ArgumentParser(description="Measure tool-call equivalence on the Aura test set.")
    ap.add_argument("--config", default=None)
    ap.add_argument("--base", action="store_true", help="evaluate the un-finetuned base model")
    ap.add_argument("--adapter", default=None, help="path to a trained LoRA adapter")
    ap.add_argument("--max-new-tokens", type=int, default=256)
    ap.add_argument("--out", default=None, help="write the JSON report here")
    args = ap.parse_args(argv)

    cfg = cfgmod.load(args.config)
    adapter = None if args.base else (args.adapter or str(cfg.path("out_dir") / "lora"))
    report = evaluate(cfg, adapter=adapter, max_new_tokens=args.max_new_tokens)
    print(json.dumps(report, indent=2))

    out = Path(args.out) if args.out else cfg.path("out_dir") / (
        "eval_base.json" if args.base else "eval_finetuned.json"
    )
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(report, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
