# Phase 3: llm-client-toolresult - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-30
**Phase:** 03-llm-client-toolresult
**Areas discussed:** Commit decomposition, OTel scope, current_time delivery, System prompt + chat texture, Malformed tool-calls, read_tool_output units, finish_reason=length, SSE parser+usage, Price-table, Generation params, Golden fixtures, Spillover/session, tool_choice, OpenRouter routing, Secret redaction, Degradation policy, Loop output semantics, Multi tool-calls, REPL feedback, Timeout wiring, Test tiers, aura config, go.mod OTel, slog

---

## Commit decomposition

| Option | Description | Selected |
|--------|-------------|----------|
| N atomic sub-commits | 7 commits in dependency order, Gate 2 green between | ✓ |
| Mega-commit | Whole phase in one commit | |
| Hybrid (3-4 commits) | Grouped coherent blocks | |

**User's choice:** N atomic sub-commits.

| Option | Description | Selected |
|--------|-------------|----------|
| Combined PRD-amend commit | A1+A2+A3 (+new) in one commit first | ✓ |
| Three separate commits | One per amendment | |
| Decide in planning | Planner counts + groups | |

**User's choice:** One combined PRD-amendment commit.

---

## OTel scope

| Option | Description | Selected |
|--------|-------------|----------|
| Full OTel slice | otel deps + full-tree SpanID minting + real TracerProvider | ✓ |
| Minimal: span-only | Mint only the llm.request span ID | |
| Decide in planning | Researcher sizes effort | |

**User's choice:** Phase 3 = full OTel slice.

| Option | Description | Selected |
|--------|-------------|----------|
| stdout default, OTLP opt-in, none disables | zero-config dev | |
| stdout only | hardcoded | |
| OTLP default | points at collector by default | ✓ |

**User's choice:** OTLP default.

| Option | Description | Selected |
|--------|-------------|----------|
| Silent-drop + override env | OTLP fails in background, never crash/pollute | ✓ |
| Auto-fallback to stdout | repoint to stdout if OTLP down | |
| Fail-fast at boot | refuse to start if OTLP unreachable | |

**User's choice:** Silent-drop + override env (`AURA_OTEL_EXPORTER`/`AURA_OTEL_ENDPOINT`).

---

## current_time delivery

| Option | Description | Selected |
|--------|-------------|----------|
| Tool-only now, tail-injection → Phase 6 | cache-safe tool; defer ambient injection | ✓ (after research) |
| Tool + tail-injection now | both in Phase 3 | |
| Tool + ambient-date in system prompt | REJECTED anti-pattern | |

**User's choice:** "cerca pattern industriali e decidiamo la migliore" → researched (web + Codex + Claude Code + Honcho/Hermes anti-pattern). Consensus: never live clock in cached prefix; tool path cache-safe; tail-injection is Phase 6 prompt-builder domain. Selected tool-only.
**Notes:** Codex injects date via per-turn environment context / `date` command, not cached instructions. Claude Code ~92% cache-hit via reminder-on-next-user-message.

---

## System prompt + chat texture

| Option | Description | Selected |
|--------|-------------|----------|
| Minimal tool-aware EN, mechanism-not-list, const in agent pkg | byte-stable, tool schemas in req.Tools | ✓ (after research) |
| Rich/extended persona | elaborate prompt | |
| Config-driven from day 1 | load from ~/.aura/env | |

**User's choice:** "cerca pattern industriali" → researched (Codex one-line identity + concise sections; nanobot composes from snippets). Selected minimal, mechanism-not-name-list (cache-stable), const in `internal/agent`.

| Option | Description | Selected |
|--------|-------------|----------|
| 1st Ctrl+C aborts turn, 2nd/EOF exits | mature REPL UX | ✓ |
| Ctrl+C always exits | kill process | |

**User's choice:** Two-stage Ctrl+C.

| Option | Description | Selected |
|--------|-------------|----------|
| One-line cost footer | compact per-turn | ✓ |
| Verbose multi-line | detailed block | |
| End-of-session only | totals at /exit | |

**User's choice:** One-line per-turn footer.

---

## Malformed / unknown tool-calls

| Option | Description | Selected |
|--------|-------------|----------|
| Error-back-to-model, bounded by budget | resilient; never terminal | ✓ |
| Terminal on tool error | run fails | |

**User's choice:** Error-back-to-model.

---

## read_tool_output offset/limit units

| Option | Description | Selected |
|--------|-------------|----------|
| Byte-based + SPEC-amendment | robust on arbitrary content; matches acceptance | ✓ |
| Line-based | breaks on newline-free content | |

**User's choice:** Byte-based (Amendment A4 to SPEC Req#7).

---

## finish_reason="length" (truncation)

| Option | Description | Selected |
|--------|-------------|----------|
| Partial + truncation notice, no auto-continue | partial enters history | ✓ |
| Auto-continue up to N | unpredictable cost | |

**User's choice:** Partial + notice, no auto-continue.

---

## SSE parser robustness + usage

| Option | Description | Selected |
|--------|-------------|----------|
| Defensive + include_usage on, cost via research | skip comments/[DONE], ReadString not Scanner | ✓ |
| Minimal parser | fragile on real wire | |

**User's choice:** Defensive + `stream_options.include_usage=true`; cost field (usage chunk vs GET /generation) flagged for research.

---

## Price-table (A3)

| Option | Description | Selected |
|--------|-------------|----------|
| Seed in llm pkg + ~/.aura override, unknown→n/a | table is safety net | ✓ |
| Only ~/.aura/llm.json | no out-of-box fallback | |
| Decide in planning | | |

**User's choice:** Seed map + override; unknown model → USD "n/a".

---

## Generation params

| Option | Description | Selected |
|--------|-------------|----------|
| Temp 0.7, MaxTokens 4096, env-override | makes length testable | ✓ |
| Temp 0.3 (loop-reliability) | flatter chat | |
| Decide in planning | | |

**User's choice:** Temp 0.7 / MaxTokens 4096, both `AURA_LLM_*`-overridable.

---

## Golden fixtures layout

| Option | Description | Selected |
|--------|-------------|----------|
| testdata/*.sse + httptest streaming | Go convention | ✓ |
| Inline Go strings | noisy for multi-chunk | |
| Decide in planning | | |

**User's choice:** `testdata/*.sse` + httptest.

---

## Spillover / session wiring

| Option | Description | Selected |
|--------|-------------|----------|
| Shared helper ctx-injected + session_id=ThreadID | DRY; sidecar conversations/<ThreadID>/ | ✓ |
| Per-tool spillover | duplicated logic | |
| Agent post-process | changes ToolResult semantics | |

**User's choice:** Shared helper + session_id = Event.ThreadID.

---

## tool_choice

| Option | Description | Selected |
|--------|-------------|----------|
| auto + content-stop fallback | model decides | ✓ |
| required | forces a tool every turn | |
| Decide in planning | | |

**User's choice:** auto + content-stop fallback.

---

## OpenRouter routing

| Option | Description | Selected |
|--------|-------------|----------|
| Minimal + data_collection deny, rest in research | privacy-first | ✓ |
| No preferences | no control | |
| Full set now | anticipates Phase 6 | |

**User's choice:** Minimal + `data_collection:deny`; advanced routing flagged for research.

---

## Secret redaction

| Option | Description | Selected |
|--------|-------------|----------|
| Structural discipline + anti-leak test | key never in serialized struct + test | ✓ |
| Convention only (no test) | regression-prone | |

**User's choice:** Structural redaction + anti-leak test.

---

## Degradation policy

| Option | Description | Selected |
|--------|-------------|----------|
| Degrade clean, never crash, coherent history | Ctrl+C/overflow/write-fail all non-terminal | ✓ |
| Fail-fast on each | fragile | |
| Decide in planning | | |

**User's choice:** Degrade clean.

---

## Loop output semantics

| Option | Description | Selected |
|--------|-------------|----------|
| Design A reinforced: text_response terminal, stream the text arg, content-stop fallback | preserves Phase-2 tool-first uniformity; no PRD-amendment | ✓ (after research) |
| Design B: natural content streams, stop terminates | chat-first, breaks tool-first uniformity, needs amendment | |

**User's choice:** "Tu decidi / ricerca più a fondo" → researched (smolagents `final_answer` validates the terminal-tool pattern; OpenAI/agents confirm tool-call arg deltas ARE wire-streamable). Selected Design A; the "don't show JSON" caveat neutralized by extracting the `text` value → user sees prose.

---

## Multiple tool_calls per turn

| Option | Description | Selected |
|--------|-------------|----------|
| Yes, sequential dispatch, all results then next call | OpenAI-correct | ✓ |
| One at a time | wastes batches | |

**User's choice:** Sequential dispatch of all tool_calls.

---

## REPL feedback during tool use

| Option | Description | Selected |
|--------|-------------|----------|
| Dim line per tool-call, nothing on result | transparency without noise | ✓ |
| Silence until reply | user can't tell if stuck | |
| Verbose args + result | invades streaming | |

**User's choice:** Dim line per tool-call.

---

## Timeout / attribution headers

| Option | Description | Selected |
|--------|-------------|----------|
| ctx-deadline total, NO http.Client.Timeout + Aura headers | streaming-safe | ✓ |
| http.Client.Timeout global | kills long healthy streams | |
| Decide in planning | | |

**User's choice:** ctx-deadline + connect dialer; `HTTP-Referer`/`X-Title` attribution.

---

## Test tiers

| Option | Description | Selected |
|--------|-------------|----------|
| Deterministic CI tier + manual real smoke | httptest+fixtures in CI, real OpenRouter local | ✓ |
| Real OpenRouter in CI | paid + flaky | |
| Decide in planning | | |

**User's choice:** Deterministic CI + manual `scripts/llm_smoke.sh`.

---

## aura config

| Option | Description | Selected |
|--------|-------------|----------|
| show / get / set minimal, cobra | redacts APIKey | ✓ |
| show only | no write | |
| Decide in planning | | |

**User's choice:** show / get / set.

---

## go.mod OTel deps

| Option | Description | Selected |
|--------|-------------|----------|
| OTel set pinned, OTLP/gRPC default, versions in research | otel+sdk+stdouttrace+otlptracegrpc | ✓ |
| Minimal: API + stdout only | contradicts OTLP default | |
| Decide in planning | | |

**User's choice:** Full OTel set, versions pinned by researcher.

---

## slog setup

| Option | Description | Selected |
|--------|-------------|----------|
| Thin, request_id-correlated, no secret | OTel primary, slog secondary | ✓ |
| Verbose at INFO | noisy, leak-risk | |
| Decide in planning | | |

**User's choice:** Thin slog, DEBUG wire / WARN-ERROR failures, request_id attr.

---

## Claude's Discretion

- Exact Temp/MaxTokens (0.7/4096), otel module versions, read_tool_output default byte limit, price-table seed numbers, `aura config` key naming, spillover helper exact path.

## Deferred Ideas

- Ambient-date tail-injection → Phase 6; caching-aware OpenRouter routing → Phase 6; auto-continue on length → future; concurrent tool execution → Phase 9; conversation persistence/microcompact → Phase 4; composable snippet prompt builder → Phase 6; `tool_choice="required"` → revisit if needed.
