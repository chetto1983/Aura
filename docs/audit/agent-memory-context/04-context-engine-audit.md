# Context Engine Audit

## Executive domain conclusion

Aura's context architecture has a deliberate layered design and several
excellent dynamic-recall invariants. It is not safe at the exact LLM boundary.
The history ladder acts before the final request is assembled, the active
resume round is not protected, hooks may replace the protected prefix, and the
audited worktree currently fails to substitute richer model-only user context.

## Exact main-model call path

```text
Runner.turnLocked
  -> persist visible user turn
  -> prepare optional dynamic recall
  -> conversations.LoadManagedHistory
  -> currentRoundModelHistory
  -> agent.NewLlmAgent (canonical system prefix)
  -> PromptBuilder.buildBase (volatile hints + tool definitions)
  -> BeforeModel hooks
  -> conditional dynamic-tail validation
  -> openai_compat.Client.Stream
```

Primary evidence:

- runner orchestration: `internal/runner/runner.go:326-393`
- history ladder: `internal/conversations/context.go:195-307`
- model-only substitution: `internal/runner/turn_model_context.go:24-35`
- system construction: `internal/agent/llm_agent_construct.go:9-58`
- late prompt/tool rendering: `internal/agent/prompt/builder.go:120-132`
- hook execution and final send: `internal/agent/llm_agent.go:386-387`
- exact serialization: `internal/llm/openai_compat/client.go:170-218`

## Context sources and precedence

1. Canonical system policy.
2. Always-on skill block, protected by the persisted-history ladder.
3. Persisted conversation turns from PostgreSQL.
4. Current richer model-only user context, intended to replace only the latest
   persisted visible user text.
5. Optional dynamic long-term recall, fenced as untrusted reference data.
6. Volatile budget/workspace/date/source hints.
7. Active tool schemas.
8. Hook mutations.
9. Within-turn assistant tool calls and tool results.

The current operator input is declared authoritative over recalled memory.
Generic MCP results are untrusted. Storage-level provenance is not carried into
the memory text visible to the model.

## Model-only current user regression

`currentRoundModelHistory` copies the slice and ranges backward by value.
Assigning `v.Content` mutates only the loop copy. Callers that build model-only
attachment IDs, document catalogs, and pinned skill authority frames therefore
persist visible user text correctly but send that same plain text to the model.

The focused existing test failed with the exact missing wire content. See
[CTX-001](08-findings-register.md#ctx-001-model-only-user-context-substitution-mutates-a-copy).

## History ladder and truncation

The cap formula is:

```text
ContextWindow
- max(MaxOutputTokens, 20,000)
- 13,000 fixed headroom
- provider error reserve
```

The ladder:

1. loads the entire conversation;
2. spills eligible old sidecar-backed tool outputs to pointers;
3. treats a validated dynamic tail as indivisible;
4. warns above 75% of the hard cap;
5. drops oldest rounds;
6. preserves the leading system/always block;
7. returns `ErrContextWindowExceeded` if the protected remainder cannot fit.

Important failure: `dropOldestRound` empties the only body when there is no
later user turn. On a resume with no fresh user, that body can be the unresolved
active user/tool round. The ladder then succeeds with protected prefix but no
current task state. See
[CTX-002](08-findings-register.md#ctx-002-resume-truncation-can-delete-the-current-unresolved-round).

Ordinary old rounds are hard-dropped, not summarized. This avoids summary drift
but can remove unresolved historical facts/tool results unless the active-round
invariant is added. The configured `HistoryHardCapTurns` is unused, so storage
and pre-truncation work grow without bound
([CTX-006](08-findings-register.md#ctx-006-advertised-history-hard-cap-is-not-wired)).

## Exact token-budget analysis

`totalTokens` counts persisted turn content and tool-call fields. It does not
count the exact:

- system prompt and message framing;
- always-changing workspace/budget/time/source message;
- full active tool definitions;
- hook-added/replaced content;
- all within-turn tool-result growth;
- final synthesis's live history.

The dynamic-tail validator is conditional on a tail and still excludes complete
system/tools. The fixed 13K estimate can be smaller than real tool manifests.
Final synthesis uses accumulated live history without rerunning the ladder.

This produces [CTX-004](08-findings-register.md#ctx-004-token-budget-does-not-cover-the-exact-final-request).
Configuration accepts nonpositive context windows and does not enforce
`MaxOutputTokens >= MaxTokens`; a nonpositive window disables bounding
([CTX-005](08-findings-register.md#ctx-005-token-configuration-can-disable-or-under-reserve-protection)).

## Hook mutability and instruction safety

`BeforeModel` may replace the complete request. Command-hook validation checks
counts and a per-field byte maximum, not:

- byte identity of the system prefix;
- survival/identity of the latest user request;
- role ordering;
- assistant tool-call/tool-result pairing;
- final token fit.

Prefix drift is a warning/metric only. A buggy or compromised configured hook
can therefore delete policy/current task and still reach the provider
([CTX-003](08-findings-register.md#ctx-003-beforemodel-hooks-can-rewrite-protected-request-state)).

## Dynamic recall behavior

Production control ordering is intentionally static under PRD amendment #93;
the dynamic policy is shadowed rather than active champion. This is not a
defect. When recall runs:

- only long-term entities/preferences are requested;
- Aura supplies the owner UUID;
- result JSON is strict-decoded;
- corpus epoch before/after, IDs, counts, limits, and revisions are checked;
- exact tail content and placement are committed before streaming;
- the text is fenced as untrusted reference data.

Positive controls are strong, but per-item provenance is stripped and the
cardinality knob means per kind, not per turn. Recall failures silently fall
back to static context, and readiness does not expose the degraded core memory
capability.

## Other LLM calls

- Adaptive router: two compact messages, 32 output tokens
  (`internal/agent/llm_agent_reasoning.go:55-102`).
- Completion critic: two bounded messages, 256 output tokens
  (`internal/agent/llm_agent_completion.go:40-105`).
- Final synthesis: full live history plus nudge, without the persisted ladder
  (`internal/agent/llm_agent_finalize.go:196-229`).
- Auto-title: at most six 500-byte user/assistant excerpts
  (`internal/conversations/title.go:39-94`).

The router, critic, and title paths are appropriately bounded. Final synthesis
shares the exact-request risk.

## Multi-agent context sharing

Swarm handoff is explicit and conservative. The worker receives only a
self-contained goal; it cannot see the parent's conversation/user/other worker
state and cannot spawn a nested swarm
(`internal/agent/tools/swarm_spawn.go:20-35`). This prevents accidental implicit
context bleed. It also means the parent must include all required constraints
and provenance in the goal; no hidden memory handoff should be assumed.

## Required target invariants

- Protected system prefix is byte-identical at the final send boundary.
- Latest active user/tool round either survives intact or the turn fails
  explicitly.
- Rich model-only content replaces visible content exactly once on the wire.
- Exact model tokenizer/capability calculation includes every message, schema,
  framing overhead, and output/reasoning reserve.
- Every post-assembly mutation re-runs structural and token validation.
- Memory items have bounded, model-visible IDs/provenance/validity.
- No provider request above the supported window reaches the client.
