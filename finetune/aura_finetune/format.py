"""Normalized trace schema + conversion to FunctionGemma's chat template.

A *trace* is a list of messages in a small, OpenAI-flavored normal form that
both the harvester (Postgres) and the synthesizer (teacher) produce, and the
dataset builder consumes:

    {"role": "system",    "content": "..."}                       # optional, once
    {"role": "user",      "content": "..."}
    {"role": "assistant", "content": "...", "tool_calls": [
        {"name": "shell_exec", "arguments": {"command": "ls"}}
    ]}
    {"role": "tool",      "name": "shell_exec", "content": "..."}

`arguments` is always a dict in the normal form. FunctionGemma's chat template
renders an assistant tool call as `<start_function_call>call:name{...}` and a
tool result inside its tool-role block; we never hand-emit those tokens — the
tokenizer's chat template does it from this structure.
"""

from __future__ import annotations

import json
from typing import Any

Message = dict[str, Any]
Trace = list[Message]


def normalize_arguments(args: Any) -> dict:
    """Coerce tool-call arguments to a dict (Aura wire-format stores them as a
    JSON *string*; HF templates want a dict)."""
    if args is None:
        return {}
    if isinstance(args, dict):
        return args
    if isinstance(args, str):
        args = args.strip()
        if not args:
            return {}
        try:
            parsed = json.loads(args)
            return parsed if isinstance(parsed, dict) else {"_value": parsed}
        except json.JSONDecodeError:
            return {"_raw": args}
    return {"_value": args}


def to_chat_messages(trace: Trace) -> list[Message]:
    """Project a normalized trace to the message dicts apply_chat_template wants.

    FunctionGemma/Gemma-3 templates accept assistant `tool_calls` with
    `function.arguments` as a dict and a `tool` role with `name` + `content`.
    """
    out: list[Message] = []
    for msg in trace:
        role = msg["role"]
        if role == "assistant" and msg.get("tool_calls"):
            calls = [
                {
                    "type": "function",
                    "function": {
                        "name": c["name"],
                        "arguments": normalize_arguments(c.get("arguments")),
                    },
                }
                for c in msg["tool_calls"]
            ]
            entry: Message = {"role": "assistant", "tool_calls": calls}
            if msg.get("content"):
                entry["content"] = msg["content"]
            out.append(entry)
        elif role == "tool":
            out.append(
                {
                    "role": "tool",
                    "name": msg.get("name", ""),
                    "content": _as_text(msg.get("content", "")),
                }
            )
        else:
            out.append({"role": role, "content": _as_text(msg.get("content", ""))})
    return out


def _as_text(content: Any) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):  # some templates wrap tool output in a list
        parts = []
        for part in content:
            if isinstance(part, dict):
                parts.append(str(part.get("content") or part.get("text") or json.dumps(part)))
            else:
                parts.append(str(part))
        return "\n".join(parts)
    return json.dumps(content) if content is not None else ""


def render_text(
    messages: list[Message],
    tools: list[dict],
    tokenizer: Any,
    *,
    add_generation_prompt: bool = False,
) -> str:
    """Render to a single training string via the model's own chat template."""
    return tokenizer.apply_chat_template(
        messages,
        tools=tools or None,
        tokenize=False,
        add_generation_prompt=add_generation_prompt,
    )


def split_at_assistant_calls(trace: Trace) -> list[tuple[Trace, Message]]:
    """Yield (prefix, gold_assistant_turn) pairs for every assistant turn that
    contains tool_calls — the unit of the tool-call-equivalence metric. The
    prefix is everything strictly before the assistant turn.
    """
    pairs: list[tuple[Trace, Message]] = []
    for i, msg in enumerate(trace):
        if msg.get("role") == "assistant" and msg.get("tool_calls"):
            pairs.append((trace[:i], msg))
    return pairs


def canonical_calls(msg: Message) -> list[tuple[str, str]]:
    """Order-insensitive, whitespace-insensitive canonical form of an
    assistant turn's tool calls, for equivalence comparison."""
    calls = []
    for c in msg.get("tool_calls", []):
        name = c.get("name") or c.get("function", {}).get("name", "")
        args = c.get("arguments")
        if args is None:
            args = c.get("function", {}).get("arguments")
        args = normalize_arguments(args)
        calls.append((name, json.dumps(args, sort_keys=True, separators=(",", ":"))))
    return sorted(calls)


def calls_equivalent(pred: Message, gold: Message) -> bool:
    return canonical_calls(pred) == canonical_calls(gold)


def trace_fingerprint(trace: Trace) -> str:
    """Stable hash of a trace for dedup across harvest + synth."""
    import hashlib

    norm = []
    for m in trace:
        norm.append(
            {
                "role": m.get("role"),
                "content": _as_text(m.get("content", "")).strip(),
                "calls": [
                    [c.get("name"), normalize_arguments(c.get("arguments"))]
                    for c in m.get("tool_calls", [])
                ],
                "name": m.get("name", ""),
            }
        )
    blob = json.dumps(norm, sort_keys=True, ensure_ascii=False)
    return hashlib.sha256(blob.encode("utf-8")).hexdigest()
