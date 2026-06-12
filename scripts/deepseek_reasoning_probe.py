#!/usr/bin/env python3
"""Probe OpenRouter models for reasoning behavior or primary-model readiness.

Coverage per https://api-docs.deepseek.com/guides/thinking_mode:
- native thinking toggle passthrough (`thinking: {"type": "enabled"|"disabled"}`, default enabled)
- effort tiers via OpenRouter `reasoning.effort` (DeepSeek maps low/medium->high, xhigh->max)
- adaptive thinking: reasoning budget should scale with prompt complexity at provider defaults
- TTFT + thinking time measured per SSE frame (the API reports neither; we timestamp deltas)

Primary-model coverage:
- text completion
- streaming TTFT/content
- JSON parseability and raw-format compliance
- OpenAI-compatible tool call + tool-result follow-up
- single-variable model-switch gate with reasoning anti-starvation checks

Never prints reasoning text — only lengths, token counts and timings.
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any


DEFAULT_MODEL = "deepseek/deepseek-v4-flash:exacto"
DEFAULT_BASE_URL = "https://openrouter.ai/api/v1"
DEFAULT_PRIMARY_MODEL = "xiaomi/mimo-v2.5"
PRIMARY_TOOL = {
    "type": "function",
    "function": {
        "name": "calc_add",
        "description": "Add two integers and return their sum.",
        "parameters": {
            "type": "object",
            "properties": {
                "a": {"type": "integer"},
                "b": {"type": "integer"},
            },
            "required": ["a", "b"],
            "additionalProperties": False,
        },
    },
}
TIER_CASES = [
    {
        "tier": "none",
        "prompt": "ciao",
        "reasoning": {"effort": "none", "exclude": True},
        "max_tokens": 512,
        "expect_reasoning": False,
    },
    {
        "tier": "low",
        "prompt": "cerca notizie di Cuneo",
        "reasoning": {"effort": "low", "exclude": False},
        "max_tokens": 2048,
        "expect_reasoning": True,
    },
    {
        "tier": "high",
        "prompt": "scrivi uno script di scraping di La Stampa con rate limit e gestione errori",
        "reasoning": {"effort": "high", "exclude": False},
        "max_tokens": 4096,
        "expect_reasoning": True,
    },
]
# No `reasoning`/`thinking` sent on default_* cases: DeepSeek defaults to thinking enabled,
# so the model itself decides how much to think — that is the adaptive behavior under test.
ADAPTIVE_CASES = [
    {
        "case": "default_trivial",
        "prompt": "ciao",
        "max_tokens": 2048,
    },
    {
        "case": "default_moderate",
        "prompt": "Quanto fa 17 per 23 meno 12? Rispondi con risultato e una frase.",
        "max_tokens": 2048,
    },
    {
        "case": "default_complex",
        "prompt": (
            "Hai tre brocche da 12, 7 e 5 litri; solo quella da 12 e' piena. "
            "Travasando senza misurini ottieni esattamente 6 litri in una brocca: "
            "elenca la sequenza minima di travasi, poi una frase di conferma."
        ),
        "max_tokens": 6144,
    },
    {
        "case": "native_disabled",
        "prompt": "ciao",
        "thinking": {"type": "disabled"},
        "max_tokens": 512,
    },
]


def load_dotenv(path: Path) -> None:
    if not path.exists():
        return
    for raw in path.read_text(encoding="utf-8-sig").splitlines():
        line = raw.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        name, value = line.split("=", 1)
        name = name.strip()
        value = value.strip()
        if not name or name in os.environ:
            continue
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        os.environ[name] = value


def redact(text: str) -> str:
    api_key = os.environ.get("OPENROUTER_API_KEY", "")
    if api_key:
        text = text.replace(api_key, "[REDACTED_OPENROUTER_API_KEY]")
    return text


def as_len(value: Any) -> int:
    if value is None:
        return 0
    return len(str(value))


def as_int(value: Any) -> int | None:
    try:
        return int(value)
    except (TypeError, ValueError):
        return None


def nested(obj: Any, *keys: str) -> Any:
    cur = obj
    for key in keys:
        if not isinstance(cur, dict):
            return None
        cur = cur.get(key)
    return cur


def http_json(url: str, api_key: str, body: dict[str, Any], timeout: float) -> tuple[dict[str, Any], int, str]:
    data = json.dumps(body, separators=(",", ":")).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        method="POST",
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
            "Accept": "application/json",
            "HTTP-Referer": "https://local.aura.test",
            "X-Title": "Aura direct DeepSeek reasoning probe",
        },
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 - explicit user-requested live probe.
        raw = resp.read().decode("utf-8", errors="replace")
        return json.loads(raw), resp.status, resp.headers.get("content-type", "")


def http_sse(
    url: str, api_key: str, body: dict[str, Any], timeout: float
) -> tuple[list[tuple[int, dict[str, Any]]], int, str]:
    """Stream SSE frames, tagging each with ms elapsed since the request was sent."""
    data = json.dumps(body, separators=(",", ":")).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        method="POST",
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
            "Accept": "text/event-stream",
            "HTTP-Referer": "https://local.aura.test",
            "X-Title": "Aura direct DeepSeek reasoning stream probe",
        },
    )
    frames: list[tuple[int, dict[str, Any]]] = []
    start = time.monotonic()
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 - explicit user-requested live probe.
        for raw_line in resp:
            line = raw_line.decode("utf-8", errors="replace").strip()
            if not line or line.startswith(":") or not line.startswith("data:"):
                continue
            payload = line.removeprefix("data:").strip()
            if not payload or payload == "[DONE]":
                continue
            frames.append((int((time.monotonic() - start) * 1000), json.loads(payload)))
        return frames, resp.status, resp.headers.get("content-type", "")


def summarize_non_stream(resp: dict[str, Any], status: int, content_type: str, elapsed_ms: int, model: str) -> dict[str, Any]:
    choice = (resp.get("choices") or [{}])[0]
    message = choice.get("message") or {}
    reasoning = message.get("reasoning")
    reasoning_content = message.get("reasoning_content")
    return {
        "test": "non_stream",
        "ok": True,
        "http_status": status,
        "content_type": content_type,
        "elapsed_ms": elapsed_ms,
        "requested_model": model,
        "returned_model": resp.get("model"),
        "finish_reason": choice.get("finish_reason"),
        "message_has_reasoning": reasoning is not None,
        "message_has_reasoning_content": reasoning_content is not None,
        "message_has_reasoning_details": message.get("reasoning_details") is not None,
        "reasoning_chars": as_len(reasoning),
        "reasoning_content_chars": as_len(reasoning_content),
        "content_chars": as_len(message.get("content")),
        "prompt_tokens": nested(resp, "usage", "prompt_tokens"),
        "completion_tokens": nested(resp, "usage", "completion_tokens"),
        "reasoning_tokens": nested(resp, "usage", "completion_tokens_details", "reasoning_tokens"),
    }


def summarize_stream(
    frames: list[tuple[int, dict[str, Any]]], status: int, content_type: str, elapsed_ms: int, model: str
) -> dict[str, Any]:
    reasoning_chunks = 0
    reasoning_content_chunks = 0
    reasoning_details_chunks = 0
    reasoning_chars = 0
    reasoning_content_chars = 0
    content_chunks = 0
    content_chars = 0
    content_parts: list[str] = []
    error_frames = 0
    finish_reason = None
    usage: dict[str, Any] = {}
    ttft_ms: int | None = None
    first_reasoning_ms: int | None = None
    last_reasoning_ms: int | None = None
    first_content_ms: int | None = None

    for at_ms, frame in frames:
        if frame.get("error") is not None:
            error_frames += 1
            continue
        if isinstance(frame.get("usage"), dict):
            usage = frame["usage"]
        for choice in frame.get("choices") or []:
            if choice.get("finish_reason"):
                finish_reason = choice.get("finish_reason")
            delta = choice.get("delta") or {}
            r_len = as_len(delta.get("reasoning"))
            if r_len:
                reasoning_chunks += 1
                reasoning_chars += r_len
            rc_len = as_len(delta.get("reasoning_content"))
            if rc_len:
                reasoning_content_chunks += 1
                reasoning_content_chars += rc_len
            if delta.get("reasoning_details"):
                reasoning_details_chunks += 1
            content_delta = delta.get("content")
            c_len = as_len(content_delta)
            if c_len:
                content_chunks += 1
                content_chars += c_len
                if len("".join(content_parts)) < 500:
                    content_parts.append(str(content_delta))
            if r_len or rc_len:
                if first_reasoning_ms is None:
                    first_reasoning_ms = at_ms
                last_reasoning_ms = at_ms
            if c_len and first_content_ms is None:
                first_content_ms = at_ms
            if (r_len or rc_len or c_len or delta.get("tool_calls")) and ttft_ms is None:
                ttft_ms = at_ms

    # Thinking time = reasoning phase span: first reasoning delta -> first content delta
    # (fallback: last reasoning delta when the stream never reaches content).
    if first_reasoning_ms is None:
        thinking_ms = 0
    elif first_content_ms is not None and first_content_ms >= first_reasoning_ms:
        thinking_ms = first_content_ms - first_reasoning_ms
    elif last_reasoning_ms is not None:
        thinking_ms = last_reasoning_ms - first_reasoning_ms
    else:
        thinking_ms = 0

    return {
        "test": "stream",
        "ok": True,
        "http_status": status,
        "content_type": content_type,
        "elapsed_ms": elapsed_ms,
        "ttft_ms": ttft_ms,
        "first_reasoning_ms": first_reasoning_ms,
        "first_content_ms": first_content_ms,
        "thinking_ms": thinking_ms,
        "requested_model": model,
        "data_frames": len(frames),
        "error_frames": error_frames,
        "finish_reason": finish_reason,
        "delta_reasoning_chunks": reasoning_chunks,
        "delta_reasoning_content_chunks": reasoning_content_chunks,
        "delta_reasoning_details_chunks": reasoning_details_chunks,
        "delta_reasoning_chars": reasoning_chars,
        "delta_reasoning_content_chars": reasoning_content_chars,
        "delta_content_chunks": content_chunks,
        "delta_content_chars": content_chars,
        "content_preview": compact_preview("".join(content_parts)),
        "prompt_tokens": usage.get("prompt_tokens"),
        "completion_tokens": usage.get("completion_tokens"),
        "reasoning_tokens": nested(usage, "completion_tokens_details", "reasoning_tokens"),
    }


def request_body(
    model: str,
    prompt: str,
    stream: bool,
    reasoning: dict[str, Any] | None = None,
    thinking: dict[str, Any] | None = None,
    max_tokens: int = 1200,
) -> dict[str, Any]:
    body: dict[str, Any] = {
        "model": model,
        "messages": [
            {"role": "system", "content": "Rispondi in italiano. Mantieni la risposta finale breve."},
            {"role": "user", "content": prompt},
        ],
        "max_tokens": max_tokens,
    }
    if reasoning is not None:
        body["reasoning"] = reasoning
    if thinking is not None:
        body["thinking"] = thinking
    # DeepSeek thinking mode ignores temperature/top_p/penalties; only send temperature
    # when thinking is explicitly off (effort none / enabled false / native disabled).
    thinking_disabled = (
        (thinking is not None and thinking.get("type") == "disabled")
        or (reasoning is not None and reasoning.get("effort") == "none")
        or (reasoning is not None and reasoning.get("enabled") is False)
    )
    if thinking_disabled:
        body["temperature"] = 0
    if stream:
        body["stream"] = True
        body["stream_options"] = {"include_usage": True}
    return body


def reasoning_off_fields(model: str) -> dict[str, Any]:
    """Provider-specific off-switch for hidden reasoning during primary-model probes."""
    if "deepseek" in model.lower():
        return {"reasoning": {"effort": "none", "exclude": True}}
    return {"reasoning_effort": "none"}


def primary_request_body(
    model: str,
    messages: list[dict[str, Any]],
    *,
    stream: bool = False,
    max_tokens: int = 512,
    tools: list[dict[str, Any]] | None = None,
    tool_choice: str | dict[str, Any] | None = None,
    disable_reasoning: bool = True,
) -> dict[str, Any]:
    body: dict[str, Any] = {
        "model": model,
        "messages": messages,
        "max_tokens": max_tokens,
        "temperature": 0,
    }
    if disable_reasoning:
        body.update(reasoning_off_fields(model))
    if tools is not None:
        body["tools"] = tools
        body["tool_choice"] = tool_choice or "auto"
    if stream:
        body["stream"] = True
        body["stream_options"] = {"include_usage": True}
    return body


def compact_preview(value: Any, limit: int = 220) -> str:
    text = "" if value is None else str(value).strip()
    text = " ".join(text.split())
    if len(text) <= limit:
        return text
    return f"{text[: limit - 3]}..."


def message_content(resp: dict[str, Any]) -> str:
    choice = (resp.get("choices") or [{}])[0]
    message = choice.get("message") or {}
    content = message.get("content")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts = []
        for item in content:
            if isinstance(item, dict) and isinstance(item.get("text"), str):
                parts.append(item["text"])
        return "\n".join(parts)
    return ""


def first_message(resp: dict[str, Any]) -> dict[str, Any]:
    choice = (resp.get("choices") or [{}])[0]
    message = choice.get("message")
    return message if isinstance(message, dict) else {}


def strip_json_fence(text: str) -> str:
    stripped = text.strip()
    if not stripped.startswith("```"):
        return stripped
    body = stripped[3:].strip()
    if body.lower().startswith("json"):
        body = body[4:].lstrip()
    if body.endswith("```"):
        body = body[:-3].strip()
    return body


def summarize_primary_json(
    name: str,
    resp: dict[str, Any],
    status: int,
    content_type: str,
    elapsed_ms: int,
    model: str,
    *,
    content_ok: bool,
    details: dict[str, Any] | None = None,
) -> dict[str, Any]:
    choice = (resp.get("choices") or [{}])[0]
    content = message_content(resp)
    summary: dict[str, Any] = {
        "test": name,
        "ok": content_ok,
        "http_status": status,
        "content_type": content_type,
        "elapsed_ms": elapsed_ms,
        "requested_model": model,
        "returned_model": resp.get("model"),
        "finish_reason": choice.get("finish_reason"),
        "content_chars": len(content),
        "content_preview": compact_preview(content),
        "prompt_tokens": nested(resp, "usage", "prompt_tokens"),
        "completion_tokens": nested(resp, "usage", "completion_tokens"),
        "reasoning_tokens": nested(resp, "usage", "completion_tokens_details", "reasoning_tokens"),
    }
    if details:
        summary.update(details)
    return summary


def has_reasoning(summary: dict[str, Any]) -> bool:
    return (
        summary.get("reasoning_chars", 0)
        + summary.get("reasoning_content_chars", 0)
        + summary.get("delta_reasoning_chars", 0)
        + summary.get("delta_reasoning_content_chars", 0)
    ) > 0


def reasoning_burn(summary: dict[str, Any]) -> int:
    tokens = summary.get("reasoning_tokens")
    if isinstance(tokens, int) and tokens > 0:
        return tokens
    return summary.get("delta_reasoning_chars", 0) + summary.get("delta_reasoning_content_chars", 0)


def run_stream_case(
    url: str,
    api_key: str,
    model: str,
    timeout: float,
    prompt: str,
    reasoning: dict[str, Any] | None,
    thinking: dict[str, Any] | None,
    max_tokens: int,
) -> dict[str, Any]:
    start = time.monotonic()
    body = request_body(model, prompt, stream=True, reasoning=reasoning, thinking=thinking, max_tokens=max_tokens)
    frames, status, content_type = http_sse(url, api_key, body, timeout)
    elapsed_ms = int((time.monotonic() - start) * 1000)
    return summarize_stream(frames, status, content_type, elapsed_ms, model)


def run_tier_matrix(url: str, api_key: str, model: str, timeout: float) -> list[dict[str, Any]]:
    results: list[dict[str, Any]] = []
    for case in TIER_CASES:
        summary = run_stream_case(
            url, api_key, model, timeout, case["prompt"], case["reasoning"], None, case["max_tokens"]
        )
        exposed = has_reasoning(summary)
        summary.update(
            {
                "test": f"adaptive_tier_{case['tier']}",
                "tier": case["tier"],
                "sent_reasoning": case["reasoning"],
                "sent_max_tokens": case["max_tokens"],
                "expect_reasoning": case["expect_reasoning"],
                "reasoning_exposed": exposed,
                "tier_ok": exposed == case["expect_reasoning"],
            }
        )
        results.append(summary)
    return results


def run_adaptive_matrix(url: str, api_key: str, model: str, timeout: float) -> list[dict[str, Any]]:
    results: list[dict[str, Any]] = []
    for case in ADAPTIVE_CASES:
        summary = run_stream_case(
            url, api_key, model, timeout, case["prompt"], None, case.get("thinking"), case["max_tokens"]
        )
        summary.update(
            {
                "test": f"adaptive_thinking_{case['case']}",
                "case": case["case"],
                "sent_thinking": case.get("thinking"),
                "sent_max_tokens": case["max_tokens"],
                "reasoning_exposed": has_reasoning(summary),
                "reasoning_burn": reasoning_burn(summary),
            }
        )
        results.append(summary)
    return results


def adaptive_verdict(results: list[dict[str, Any]]) -> dict[str, Any]:
    by_case = {r["case"]: r for r in results}
    trivial = by_case["default_trivial"]
    moderate = by_case["default_moderate"]
    complex_ = by_case["default_complex"]
    disabled = by_case["native_disabled"]

    burn = {c["case"]: c["reasoning_burn"] for c in results}
    default_thinking_enabled = any(r["reasoning_exposed"] for r in (trivial, moderate, complex_))
    scales_with_complexity = burn["default_trivial"] < burn["default_complex"]
    ratio = round(burn["default_trivial"] / burn["default_complex"], 3) if burn["default_complex"] else None
    # Informational, not gating: OpenRouter does not forward the DeepSeek-native `thinking`
    # param (verified live 2026-06-11); the enforced off-switch is `reasoning.effort: none`
    # in the tier matrix. False here = "use OpenRouter reasoning param, never native thinking".
    native_thinking_param_forwarded = not disabled["reasoning_exposed"]
    return {
        "default_thinking_enabled": default_thinking_enabled,
        "scales_with_complexity": scales_with_complexity,
        "trivial_to_complex_burn_ratio": ratio,
        "native_thinking_param_forwarded": native_thinking_param_forwarded,
        "reasoning_burn_by_case": burn,
        "thinking_ms_by_case": {c["case"]: c["thinking_ms"] for c in results},
        "ttft_ms_by_case": {c["case"]: c["ttft_ms"] for c in results},
        "adaptive_ok": default_thinking_enabled and scales_with_complexity,
    }


def run_reasoning_probe(args: argparse.Namespace, url: str, model: str, base_url: str, api_key: str) -> dict[str, Any]:
    results: list[dict[str, Any]] = []
    prompt_a = (
        "Risolvi questo controllo: se Marco ha 12 mele, ne regala 5, "
        "poi triplica quelle rimaste e ne mangia 4, quante mele restano? "
        "Dammi solo risultato e una frase."
    )
    start = time.monotonic()
    resp, status, content_type = http_json(
        url, api_key, request_body(model, prompt_a, stream=False, reasoning={"effort": "high", "exclude": False}), args.timeout
    )
    elapsed_ms = int((time.monotonic() - start) * 1000)
    results.append(summarize_non_stream(resp, status, content_type, elapsed_ms, model))

    prompt_b = (
        "Risolvi questo controllo: se Anna compra 7 quaderni a 3 euro, paga con 30 euro "
        "e poi divide il resto tra 3 amici, quanti euro riceve ciascuno? "
        "Dammi solo risultato e una frase."
    )
    results.append(
        run_stream_case(url, api_key, model, args.timeout, prompt_b, {"effort": "high", "exclude": False}, None, 1200)
    )

    tier_results = run_tier_matrix(url, api_key, model, args.timeout)
    results.extend(tier_results)

    adaptive_results = run_adaptive_matrix(url, api_key, model, args.timeout)
    results.extend(adaptive_results)
    adaptive = adaptive_verdict(adaptive_results)

    non_stream_exposed = (results[0]["reasoning_chars"] + results[0]["reasoning_content_chars"]) > 0
    stream_exposed = (results[1]["delta_reasoning_chars"] + results[1]["delta_reasoning_content_chars"]) > 0
    tier_ok = all(result["tier_ok"] for result in tier_results)
    return {
        "profile": "reasoning",
        "ok": non_stream_exposed and stream_exposed and tier_ok and adaptive["adaptive_ok"],
        "base_url": base_url,
        "model": model,
        "reasoning_exposed_non_stream": non_stream_exposed,
        "reasoning_exposed_stream": stream_exposed,
        "adaptive_tiers_ok": tier_ok,
        "adaptive_thinking": adaptive,
        "stream_ttft_ms": results[1]["ttft_ms"],
        "stream_thinking_ms": results[1]["thinking_ms"],
        "doc_notes": {
            "thinking_mode_doc": "https://api-docs.deepseek.com/guides/thinking_mode",
            "native_toggle": "thinking: {type: enabled|disabled}; default enabled",
            "effort_mapping": "DeepSeek maps low/medium->high, xhigh->max; default high (agent traffic: max)",
            "sampling": "thinking mode ignores temperature/top_p/presence/frequency penalties",
        },
        "results": results,
    }


def run_primary_probe(args: argparse.Namespace, url: str, model: str, base_url: str, api_key: str) -> dict[str, Any]:
    disable_reasoning = not args.allow_reasoning
    results: list[dict[str, Any]] = []

    text_messages = [
        {"role": "system", "content": "Rispondi in italiano, breve e diretto."},
        {"role": "user", "content": "Quanto fa 17*23-12? Rispondi con solo il numero e una parola di conferma."},
    ]
    start = time.monotonic()
    resp, status, content_type = http_json(
        url,
        api_key,
        primary_request_body(model, text_messages, max_tokens=128, disable_reasoning=disable_reasoning),
        args.timeout,
    )
    elapsed_ms = int((time.monotonic() - start) * 1000)
    content = message_content(resp)
    results.append(
        summarize_primary_json(
            "primary_text",
            resp,
            status,
            content_type,
            elapsed_ms,
            model,
            content_ok="379" in content,
            details={"expected_fragment": "379"},
        )
    )

    stream_messages = [
        {"role": "system", "content": "Rispondi in italiano, breve e diretto."},
        {"role": "user", "content": "Scrivi una frase di massimo 12 parole: Aura usa RAG per documenti aggiornati."},
    ]
    start = time.monotonic()
    frames, status, content_type = http_sse(
        url,
        api_key,
        primary_request_body(model, stream_messages, stream=True, max_tokens=128, disable_reasoning=disable_reasoning),
        args.timeout,
    )
    elapsed_ms = int((time.monotonic() - start) * 1000)
    stream_summary = summarize_stream(frames, status, content_type, elapsed_ms, model)
    stream_summary["test"] = "primary_stream"
    stream_summary["ok"] = stream_summary.get("delta_content_chars", 0) > 0
    results.append(stream_summary)

    json_messages = [
        {"role": "system", "content": "Rispondi solo con JSON valido, senza markdown."},
        {
            "role": "user",
            "content": (
                "Valuta se xiaomi/mimo-v2.5 puo' essere candidato come modello principale. "
                "Schema: {\"decision\":\"candidate|reject\",\"score\":0-100,\"reasons\":[\"...\"]}."
            ),
        },
    ]
    start = time.monotonic()
    resp, status, content_type = http_json(
        url,
        api_key,
        primary_request_body(model, json_messages, max_tokens=256, disable_reasoning=disable_reasoning),
        args.timeout,
    )
    elapsed_ms = int((time.monotonic() - start) * 1000)
    raw_content = message_content(resp).strip()
    raw_json = strip_json_fence(raw_content)
    raw_strict_json = raw_content == raw_json
    parsed_json: dict[str, Any] | None = None
    json_error = ""
    try:
        candidate = json.loads(raw_json)
        if isinstance(candidate, dict):
            parsed_json = candidate
        else:
            json_error = "top-level JSON is not an object"
    except json.JSONDecodeError as exc:
        json_error = str(exc)
    json_ok = (
        isinstance(parsed_json, dict)
        and parsed_json.get("decision") in {"candidate", "reject"}
        and isinstance(parsed_json.get("score"), int)
        and isinstance(parsed_json.get("reasons"), list)
    )
    results.append(
        summarize_primary_json(
            "primary_json",
            resp,
            status,
            content_type,
            elapsed_ms,
            model,
            content_ok=json_ok,
            details={
                "json_parse_ok": parsed_json is not None,
                "json_raw_strict_ok": raw_strict_json,
                "json_error": json_error,
                "parsed_decision": parsed_json.get("decision") if parsed_json else None,
                "parsed_score": parsed_json.get("score") if parsed_json else None,
            },
        )
    )

    tool_messages = [
        {"role": "system", "content": "Usa i tool quando l'utente chiede calcoli tramite tool."},
        {"role": "user", "content": "Usa il tool calc_add per sommare 17 e 23. Non calcolare a mente."},
    ]
    start = time.monotonic()
    resp, status, content_type = http_json(
        url,
        api_key,
        primary_request_body(
            model,
            tool_messages,
            max_tokens=256,
            tools=[PRIMARY_TOOL],
            tool_choice="auto",
            disable_reasoning=disable_reasoning,
        ),
        args.timeout,
    )
    elapsed_ms = int((time.monotonic() - start) * 1000)
    message = first_message(resp)
    tool_calls = message.get("tool_calls") if isinstance(message.get("tool_calls"), list) else []
    first_tool = tool_calls[0] if tool_calls else {}
    function = first_tool.get("function") if isinstance(first_tool, dict) else {}
    args_raw = function.get("arguments") if isinstance(function, dict) else ""
    parsed_args: dict[str, Any] = {}
    args_error = ""
    try:
        parsed = json.loads(args_raw or "{}")
        if isinstance(parsed, dict):
            parsed_args = parsed
    except json.JSONDecodeError as exc:
        args_error = str(exc)
    tool_ok = (
        function.get("name") == "calc_add"
        and as_int(parsed_args.get("a")) == 17
        and as_int(parsed_args.get("b")) == 23
    )
    results.append(
        summarize_primary_json(
            "primary_tool_call",
            resp,
            status,
            content_type,
            elapsed_ms,
            model,
            content_ok=tool_ok,
            details={
                "tool_call_count": len(tool_calls),
                "tool_name": function.get("name"),
                "tool_args": parsed_args,
                "tool_args_error": args_error,
            },
        )
    )

    followup_ok = False
    if tool_calls:
        followup_messages = [
            *tool_messages,
            {
                "role": "assistant",
                "content": message.get("content") or "",
                "tool_calls": tool_calls,
            },
            {
                "role": "tool",
                "tool_call_id": first_tool.get("id"),
                "name": "calc_add",
                "content": "40",
            },
            {"role": "user", "content": "Ora rispondi con il risultato finale in una frase breve."},
        ]
        start = time.monotonic()
        resp, status, content_type = http_json(
            url,
            api_key,
            primary_request_body(
                model,
                followup_messages,
                max_tokens=128,
                tools=[PRIMARY_TOOL],
                tool_choice="auto",
                disable_reasoning=disable_reasoning,
            ),
            args.timeout,
        )
        elapsed_ms = int((time.monotonic() - start) * 1000)
        followup_content = message_content(resp)
        followup_ok = "40" in followup_content
        results.append(
            summarize_primary_json(
                "primary_tool_followup",
                resp,
                status,
                content_type,
                elapsed_ms,
                model,
                content_ok=followup_ok,
                details={"expected_fragment": "40"},
            )
        )
    else:
        results.append(
            {
                "test": "primary_tool_followup",
                "ok": False,
                "skipped": True,
                "reason": "tool call was not emitted",
            }
        )

    ok = all(result.get("ok") is True for result in results)
    return {
        "profile": "primary",
        "ok": ok,
        "base_url": base_url,
        "model": model,
        "reasoning_disabled": disable_reasoning,
        "main_model_candidate": ok,
        "notes": {
            "reasoning_off": (
                "MiMo consumed hidden reasoning budget in prior multimodal tests; "
                "primary profile disables reasoning unless --allow-reasoning is passed."
            ),
            "gates": "text, stream, JSON, tool call and tool-result follow-up must all pass.",
        },
        "results": results,
    }


def has_reasoning_tokens(summary: dict[str, Any]) -> bool:
    tokens = summary.get("reasoning_tokens")
    return isinstance(tokens, int) and tokens > 0


def mark_reasoning_gate(
    summary: dict[str, Any],
    *,
    name: str,
    expect_reasoning: bool | None,
    max_elapsed_ms: int | None = None,
) -> dict[str, Any]:
    reasoning_seen = has_reasoning(summary) or has_reasoning_tokens(summary)
    content_ok = summary.get("delta_content_chars", 0) > 0
    finish_ok = summary.get("finish_reason") == "stop"
    latency_ok = max_elapsed_ms is None or summary.get("elapsed_ms", 0) <= max_elapsed_ms
    if expect_reasoning is True:
        reasoning_ok = reasoning_seen
    elif expect_reasoning is False:
        reasoning_ok = not reasoning_seen
    else:
        reasoning_ok = True
    summary.update(
        {
            "test": f"model_switch_reasoning_{name}",
            "ok": content_ok and finish_ok and latency_ok and reasoning_ok,
            "content_ok": content_ok,
            "finish_ok": finish_ok,
            "reasoning_seen": reasoning_seen,
            "expect_reasoning": expect_reasoning,
            "reasoning_ok": reasoning_ok,
            "max_elapsed_ms": max_elapsed_ms,
            "latency_ok": latency_ok,
        }
    )
    return summary


def run_switch_reasoning_probe(
    args: argparse.Namespace, url: str, model: str, base_url: str, api_key: str
) -> dict[str, Any]:
    results: list[dict[str, Any]] = []

    none_summary = run_stream_case(
        url,
        api_key,
        model,
        args.timeout,
        "ciao. Rispondi con una frase brevissima.",
        {"effort": "none", "exclude": True},
        None,
        512,
    )
    results.append(mark_reasoning_gate(none_summary, name="none_offswitch", expect_reasoning=False))

    low_summary = run_stream_case(
        url,
        api_key,
        model,
        args.timeout,
        "Quanto fa 17 per 23 meno 12? Mostra solo risultato e una frase.",
        {"effort": "low", "exclude": False},
        None,
        1024,
    )
    results.append(mark_reasoning_gate(low_summary, name="low_content", expect_reasoning=True, max_elapsed_ms=30_000))

    high_prompt = (
        "Devi fare debug di una pipeline RAG: estrazione ok, embedding ok, search zero risultati. "
        "Elenca 4 ipotesi ordinate per probabilita' e il primo comando di verifica. Rispondi breve."
    )
    high_summary = run_stream_case(
        url,
        api_key,
        model,
        args.timeout,
        high_prompt,
        {"effort": "high", "exclude": False},
        None,
        2048,
    )
    results.append(mark_reasoning_gate(high_summary, name="high_no_starvation", expect_reasoning=True, max_elapsed_ms=60_000))

    router_messages = [
        {
            "role": "system",
            "content": (
                "Classify the latest user request into one reasoning tier for the next assistant turn. "
                "Reply only JSON: {\"tier\":\"none\"|\"low\"|\"high\"}."
            ),
        },
        {"role": "user", "content": high_prompt},
    ]
    router_body = {
        "model": model,
        "messages": router_messages,
        "max_tokens": 32,
        "temperature": 0,
        "reasoning": {"enabled": False},
        "tool_choice": "none",
        "stream": True,
        "stream_options": {"include_usage": True},
    }
    start = time.monotonic()
    frames, status, content_type = http_sse(url, api_key, router_body, args.timeout)
    elapsed_ms = int((time.monotonic() - start) * 1000)
    router_summary = summarize_stream(frames, status, content_type, elapsed_ms, model)
    router_summary = mark_reasoning_gate(router_summary, name="router_enabled_false", expect_reasoning=False, max_elapsed_ms=15_000)
    router_content = strip_json_fence(router_summary.get("content_preview", ""))
    router_json_ok = False
    try:
        parsed = json.loads(router_content)
        router_json_ok = isinstance(parsed, dict) and parsed.get("tier") in {"none", "low", "high"}
    except json.JSONDecodeError:
        router_json_ok = False
    router_summary["router_json_ok"] = router_json_ok
    router_summary["ok"] = router_summary["ok"] and router_json_ok
    results.append(router_summary)

    ok = all(result.get("ok") is True for result in results)
    return {
        "profile": "model-switch-reasoning",
        "ok": ok,
        "base_url": base_url,
        "model": model,
        "wire_contract": "Aura OpenRouter path: reasoning.effort/exclude and reasoning.enabled=false",
        "results": results,
    }


def run_model_switch_probe(args: argparse.Namespace, url: str, model: str, base_url: str, api_key: str) -> dict[str, Any]:
    primary = run_primary_probe(args, url, model, base_url, api_key)
    reasoning = run_switch_reasoning_probe(args, url, model, base_url, api_key)
    return {
        "profile": "model-switch",
        "ok": primary["ok"] and reasoning["ok"],
        "base_url": base_url,
        "model": model,
        "single_variable_ready": primary["ok"] and reasoning["ok"],
        "change_surface": "AURA_LLM_MODEL",
        "profiles": {
            "primary": primary,
            "reasoning": reasoning,
        },
    }


def run_probe(args: argparse.Namespace) -> int:
    load_dotenv(Path(args.env_file))
    api_key = os.environ.get("OPENROUTER_API_KEY", "")
    if not api_key:
        print(json.dumps({"ok": False, "error": "OPENROUTER_API_KEY missing"}, separators=(",", ":")))
        return 2

    default_model = DEFAULT_PRIMARY_MODEL if args.profile in {"primary", "model-switch"} else DEFAULT_MODEL
    model = args.model or os.environ.get("AURA_LLM_MODEL") or default_model
    base_url = (args.base_url or os.environ.get("AURA_LLM_BASE_URL") or DEFAULT_BASE_URL).rstrip("/")
    url = f"{base_url}/chat/completions"

    try:
        if args.profile == "reasoning":
            summary = run_reasoning_probe(args, url, model, base_url, api_key)
        elif args.profile == "primary":
            summary = run_primary_probe(args, url, model, base_url, api_key)
        elif args.profile == "model-switch":
            summary = run_model_switch_probe(args, url, model, base_url, api_key)
        else:
            reasoning_summary = run_reasoning_probe(args, url, model, base_url, api_key)
            primary_summary = run_primary_probe(args, url, model, base_url, api_key)
            summary = {
                "profile": "all",
                "ok": reasoning_summary["ok"] and primary_summary["ok"],
                "base_url": base_url,
                "model": model,
                "profiles": {
                    "reasoning": reasoning_summary,
                    "primary": primary_summary,
                },
            }
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        print(
            json.dumps(
                {
                    "ok": False,
                    "http_status": exc.code,
                    "error": redact(body[:800]),
                },
                ensure_ascii=False,
                separators=(",", ":"),
            )
        )
        return 1
    except Exception as exc:  # noqa: BLE001 - this probe should report failures as JSON.
        print(json.dumps({"ok": False, "error": redact(str(exc))}, ensure_ascii=False, separators=(",", ":")))
        return 1

    print(json.dumps(summary, ensure_ascii=False, separators=(",", ":")))
    return 0 if summary["ok"] else 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env-file", default=".env", help="dotenv file to load without echoing values")
    parser.add_argument("--base-url", default="", help="OpenAI-compatible base URL")
    parser.add_argument("--model", default="", help="model id to probe")
    parser.add_argument(
        "--profile",
        choices=("reasoning", "primary", "model-switch", "all"),
        default="reasoning",
        help="probe profile: DeepSeek reasoning matrix, primary readiness, model-switch gate, or both legacy profiles",
    )
    parser.add_argument(
        "--allow-reasoning",
        action="store_true",
        help="do not send the primary-profile hidden-reasoning off-switch",
    )
    parser.add_argument("--timeout", type=float, default=120.0, help="per-request timeout in seconds")
    return run_probe(parser.parse_args())


if __name__ == "__main__":
    sys.exit(main())
