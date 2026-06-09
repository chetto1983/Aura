#!/usr/bin/env python3
"""Probe OpenRouter/DeepSeek reasoning exposure without printing reasoning text."""

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
TIER_CASES = [
    {
        "tier": "none",
        "prompt": "ciao",
        "effort": "none",
        "exclude": True,
        "max_tokens": 512,
        "expect_reasoning": False,
    },
    {
        "tier": "low",
        "prompt": "cerca notizie di Cuneo",
        "effort": "low",
        "exclude": False,
        "max_tokens": 2048,
        "expect_reasoning": True,
    },
    {
        "tier": "high",
        "prompt": "scrivi uno script di scraping di La Stampa con rate limit e gestione errori",
        "effort": "high",
        "exclude": False,
        "max_tokens": 4096,
        "expect_reasoning": True,
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


def http_sse(url: str, api_key: str, body: dict[str, Any], timeout: float) -> tuple[list[dict[str, Any]], int, str]:
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
    frames: list[dict[str, Any]] = []
    with urllib.request.urlopen(req, timeout=timeout) as resp:  # noqa: S310 - explicit user-requested live probe.
        for raw_line in resp:
            line = raw_line.decode("utf-8", errors="replace").strip()
            if not line or line.startswith(":") or not line.startswith("data:"):
                continue
            payload = line.removeprefix("data:").strip()
            if not payload or payload == "[DONE]":
                continue
            frames.append(json.loads(payload))
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
        "reasoning_chars": as_len(reasoning),
        "reasoning_content_chars": as_len(reasoning_content),
        "content_chars": as_len(message.get("content")),
        "prompt_tokens": nested(resp, "usage", "prompt_tokens"),
        "completion_tokens": nested(resp, "usage", "completion_tokens"),
        "reasoning_tokens": nested(resp, "usage", "completion_tokens_details", "reasoning_tokens"),
    }


def summarize_stream(frames: list[dict[str, Any]], status: int, content_type: str, elapsed_ms: int, model: str) -> dict[str, Any]:
    reasoning_chunks = 0
    reasoning_content_chunks = 0
    reasoning_chars = 0
    reasoning_content_chars = 0
    content_chunks = 0
    content_chars = 0
    error_frames = 0
    finish_reason = None
    usage: dict[str, Any] = {}

    for frame in frames:
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
            c_len = as_len(delta.get("content"))
            if c_len:
                content_chunks += 1
                content_chars += c_len

    return {
        "test": "stream",
        "ok": True,
        "http_status": status,
        "content_type": content_type,
        "elapsed_ms": elapsed_ms,
        "requested_model": model,
        "data_frames": len(frames),
        "error_frames": error_frames,
        "finish_reason": finish_reason,
        "delta_reasoning_chunks": reasoning_chunks,
        "delta_reasoning_content_chunks": reasoning_content_chunks,
        "delta_reasoning_chars": reasoning_chars,
        "delta_reasoning_content_chars": reasoning_content_chars,
        "delta_content_chunks": content_chunks,
        "delta_content_chars": content_chars,
        "prompt_tokens": usage.get("prompt_tokens"),
        "completion_tokens": usage.get("completion_tokens"),
        "reasoning_tokens": nested(usage, "completion_tokens_details", "reasoning_tokens"),
    }


def request_body(
    model: str,
    prompt: str,
    stream: bool,
    effort: str = "high",
    exclude: bool = False,
    max_tokens: int = 1200,
) -> dict[str, Any]:
    body: dict[str, Any] = {
        "model": model,
        "messages": [
            {"role": "system", "content": "Rispondi in italiano. Mantieni la risposta finale breve."},
            {"role": "user", "content": prompt},
        ],
        "reasoning": {"effort": effort, "exclude": exclude},
        "max_tokens": max_tokens,
        "temperature": 0,
    }
    if stream:
        body["stream"] = True
        body["stream_options"] = {"include_usage": True}
    return body


def has_reasoning(summary: dict[str, Any]) -> bool:
    return (
        summary.get("reasoning_chars", 0)
        + summary.get("reasoning_content_chars", 0)
        + summary.get("delta_reasoning_chars", 0)
        + summary.get("delta_reasoning_content_chars", 0)
    ) > 0


def run_tier_matrix(url: str, api_key: str, model: str, timeout: float) -> list[dict[str, Any]]:
    results: list[dict[str, Any]] = []
    for case in TIER_CASES:
        start = time.monotonic()
        body = request_body(
            model,
            case["prompt"],
            stream=True,
            effort=case["effort"],
            exclude=case["exclude"],
            max_tokens=case["max_tokens"],
        )
        frames, status, content_type = http_sse(url, api_key, body, timeout)
        elapsed_ms = int((time.monotonic() - start) * 1000)
        summary = summarize_stream(frames, status, content_type, elapsed_ms, model)
        exposed = has_reasoning(summary)
        summary.update(
            {
                "test": f"adaptive_tier_{case['tier']}",
                "tier": case["tier"],
                "sent_effort": case["effort"],
                "sent_exclude": case["exclude"],
                "sent_max_tokens": case["max_tokens"],
                "expect_reasoning": case["expect_reasoning"],
                "reasoning_exposed": exposed,
                "tier_ok": exposed == case["expect_reasoning"],
            }
        )
        results.append(summary)
    return results


def run_probe(args: argparse.Namespace) -> int:
    load_dotenv(Path(args.env_file))
    api_key = os.environ.get("OPENROUTER_API_KEY", "")
    if not api_key:
        print(json.dumps({"ok": False, "error": "OPENROUTER_API_KEY missing"}, separators=(",", ":")))
        return 2

    model = args.model or os.environ.get("AURA_LLM_MODEL") or DEFAULT_MODEL
    base_url = (args.base_url or os.environ.get("AURA_LLM_BASE_URL") or DEFAULT_BASE_URL).rstrip("/")
    url = f"{base_url}/chat/completions"

    results: list[dict[str, Any]] = []
    try:
        prompt_a = (
            "Risolvi questo controllo: se Marco ha 12 mele, ne regala 5, "
            "poi triplica quelle rimaste e ne mangia 4, quante mele restano? "
            "Dammi solo risultato e una frase."
        )
        start = time.monotonic()
        resp, status, content_type = http_json(url, api_key, request_body(model, prompt_a, stream=False), args.timeout)
        elapsed_ms = int((time.monotonic() - start) * 1000)
        results.append(summarize_non_stream(resp, status, content_type, elapsed_ms, model))

        prompt_b = (
            "Risolvi questo controllo: se Anna compra 7 quaderni a 3 euro, paga con 30 euro "
            "e poi divide il resto tra 3 amici, quanti euro riceve ciascuno? "
            "Dammi solo risultato e una frase."
        )
        start = time.monotonic()
        frames, status, content_type = http_sse(url, api_key, request_body(model, prompt_b, stream=True), args.timeout)
        elapsed_ms = int((time.monotonic() - start) * 1000)
        results.append(summarize_stream(frames, status, content_type, elapsed_ms, model))

        tier_results = run_tier_matrix(url, api_key, model, args.timeout)
        results.extend(tier_results)
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

    non_stream_exposed = (results[0]["reasoning_chars"] + results[0]["reasoning_content_chars"]) > 0
    stream_exposed = (results[1]["delta_reasoning_chars"] + results[1]["delta_reasoning_content_chars"]) > 0
    tier_ok = all(result.get("tier_ok", True) for result in results[2:])
    summary = {
        "ok": non_stream_exposed and stream_exposed and tier_ok,
        "base_url": base_url,
        "model": model,
        "reasoning_exposed_non_stream": non_stream_exposed,
        "reasoning_exposed_stream": stream_exposed,
        "adaptive_tiers_ok": tier_ok,
        "results": results,
    }
    print(json.dumps(summary, ensure_ascii=False, separators=(",", ":")))
    return 0 if summary["ok"] else 1


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--env-file", default=".env", help="dotenv file to load without echoing values")
    parser.add_argument("--base-url", default="", help="OpenAI-compatible base URL")
    parser.add_argument("--model", default="", help="model id to probe")
    parser.add_argument("--timeout", type=float, default=120.0, help="per-request timeout in seconds")
    return run_probe(parser.parse_args())


if __name__ == "__main__":
    sys.exit(main())
