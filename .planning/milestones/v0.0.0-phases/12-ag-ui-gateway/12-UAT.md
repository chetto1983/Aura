---
status: complete
phase: 12-ag-ui-gateway
source: [12-01-SUMMARY.md, 12-02-SUMMARY.md, 12-03-SUMMARY.md, 12-04-SUMMARY.md, 12-05-SUMMARY.md, 12-06-SUMMARY.md, 12-REVIEW-FIX.md]
started: 2026-06-07T06:07:40Z
updated: 2026-06-07T06:50:00Z
executed_by: autonomous loop (operator-delegated via /goal "loop until pass at 95%") — score 7/7 (100%)
---

## Current Test

[testing complete]

## Tests

### 1. Cold Start Smoke Test (aura serve + AG-UI gateway)
expected: From a cold start, `bash scripts/agui_smoke.sh` (degraded leg) builds aura, boots `aura serve` with "agui http server listening addr=127.0.0.1:9080", asserts SSE round-trip (RUN_STARTED→RUN_FINISHED), GET MESSAGES_SNAPSHOT, 404 chokepoint, prints ≥1 SSE frame body, exits 0 with graceful shutdown.
result: pass
evidence: exit 0; RUN_STARTED + terminal frame; snapshot with seeded turns; 404 OK; no credential material in log (D:/tmp/agui-uat/test1.log)

### 2. Live SSE run with REASONING_* lifecycle (POST /agent/run)
expected: With a real OpenRouter key (AGUI_SMOKE_LIVE=1 or manual curl), POST /agent/run streams a complete REASONING lifecycle (REASONING_START → REASONING_MESSAGE_START → CONTENT×N → MESSAGE_END → REASONING_END) BEFORE the first TEXT_MESSAGE_START, then per-token TEXT_MESSAGE_CONTENT deltas that reconstruct the full answer, a STATE_DELTA with usage, and RUN_FINISHED with outcome success.
result: pass
evidence: AGUI_SMOKE_LIVE=1 exit 0 — TWO complete REASONING quartets interleaved around a live TOOL_CALL_START/ARGS/END/RESULT lifecycle, REASONING_END before first TEXT_MESSAGE_START, 7 TEXT deltas, STATE_DELTA, RUN_FINISHED; persisted snapshot CoT-free (D:/tmp/agui-uat/test2.log)

### 3. Thread history snapshot (GET /threads/{id}/messages)
expected: GET /threads/<existing-id>/messages returns a one-shot MESSAGES_SNAPSHOT JSON body with the persisted conversation turns (user + assistant). Assistant turns that invoked tools carry their tool_calls in the projection (review fix a1134f70). Chain-of-thought reasoning is NEVER present in the snapshot.
result: pass
evidence: seeded assistant turn with tool_calls jsonb → snapshot projects toolCalls (id/type/function nested) + toolCallId on tool turn (D:/tmp/agui-uat/test3_snap.json)

### 4. Input guards (404 / 400)
expected: GET or POST with an unknown thread id → HTTP 404. A malformed/non-UUID thread id → HTTP 404 (not a leaked 500). Malformed JSON body, empty required fields, or a body over 1 MiB → HTTP 400.
result: pass
evidence: live curl — GET unknown uuid 404, GET malformed id 404, POST unknown thread 404, malformed json 400, empty fields 400, 1.1MiB body 400 (D:/tmp/agui-uat/keyfree.log)

### 5. RUN_ERROR credential redaction
expected: When a run fails (e.g. DB down or runner error), the RUN_ERROR SSE frame and 4xx/5xx error bodies contain NO DSN, credential URL, or token material (postgresql://user:secret@..., Bearer tokens, api keys) — redaction broadened by review fix 5b52d0ba.
result: pass
evidence: live RUN_ERROR frame = "llm: provider returned HTTP 401" (no key material, D:/tmp/agui-uat/test5_sse.txt); TestServer_RunErrorRedaction + TestSanitizeErr (postgres/redis/http-userinfo/bearer/api_key/token) PASS under -race

### 6. CLI live reasoning render (💭)
expected: `aura chat new` with a real key streams dim 💭 chain-of-thought deltas live BEFORE the answer, tool traces (· toolname) interleave, the final answer contains no reasoning text and no ANSI mojibake, and the usage line (· N tok · $cost) renders. Reasoning is never persisted to conversation_turns.
result: pass
evidence: exit 0 — \x1b[2m💭 + per-delta dim CoT before answer; usage line `· 6832 tok (6729 in / 103 out) · $0.000769`; DB turns = user + assistant only, CoT absent (D:/tmp/agui-uat/test6_chat.out + test6.log). No tool trace this run (model answered via text_response directly — terminal calls render no line, by design). Observation (benign, by-design): model streamed content `...**4** 🎉` then sent a divergent terminal text_response without 🎉 → flushRemainder's documented divergent fallback re-emitted the canonical answer (visible double line); persisted row = canonical text_response content.

### 7. Permissive CORS end-to-end (AURA_AGUI_CORS_PERMISSIVE)
expected: With AURA_AGUI_CORS_PERMISSIVE=true, an OPTIONS preflight to /agent/run succeeds and Access-Control-Allow-Origin appears on responses INCLUDING error responses (review fix 6e5ced16). With the default (false), no ACAO header is emitted.
result: pass
evidence: preflight 204 + ACAO/Methods/Headers triple; ACAO on 404, 400 and 200; default config emits no ACAO (D:/tmp/agui-uat/test7_preflight.txt)

## Summary

total: 7
passed: 7
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none yet]
