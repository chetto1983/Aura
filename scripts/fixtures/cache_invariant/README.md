# cache_invariant fixtures

Deterministic replay fixtures for the hidden `aura cache-audit` KV-cache prefix
invariant gate (Phase 6, D-04/D-05/D-06). `turn-01.json` … `turn-20.json` are
fed in order to the real `runner.Turn → LlmAgent.Run → PromptBuilder.Build` loop
against `internal/agent/agenttest.FakeClient`; the audit hashes each captured
`Requests[n].Messages[0]` and asserts all 20 are byte-identical.

## JSON shape

```jsonc
{
  "user": "<the user message for this turn>",      // required, non-empty
  "responses": [                                    // required, ≥1, ordered
    // A text response (terminal): streams content then a finish reason.
    { "text": "<assistant text>", "finish": "stop" },

    // A tool-call response: emits finalized tool calls, then the agent threads
    // the tool results and consumes the NEXT response in the SAME turn. So a
    // tool round needs ≥2 responses (the tool call, then a terminal text).
    { "tool_calls": [ { "id": "c1", "name": "current_time", "arguments": "{}" } ] }
  ]
}
```

`finish` defaults to `"stop"` when omitted. Exactly one of `text` / `tool_calls`
is set per response. Content is FIXED — no clock, no UUIDs in fixture text — so
the replay is reproducible.

## Tool-call turns

At least two fixtures script a tool round (turn-05 → `current_time`, turn-12 →
`tool_search`) followed by a terminal `text_response`. A tool round is exactly
where a future slice (microcompact / Agent.md / cached insight) could poison the
assembled prefix, so the gate must exercise it.

Exit codes: `0` all 20 hashes equal · `1` `messages[0]` mutated at turn N · `2`
fixture missing/corrupt.
