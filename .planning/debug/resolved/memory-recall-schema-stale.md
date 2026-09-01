---
status: resolved
trigger: "non sembra funzionare; niente da fare; le conversazioni le vedo; guarda"
created: 2026-09-01
updated: 2026-09-01T06:30:00+02:00
---

# Memory recall live schema is stale

## Symptoms

- expected: In the running Aura cockpit, `memory__memory_recall` accepts `mode=recent` and a natural request about prior conversations returns historical `ConversationTurn` evidence without claiming that no conversation archive exists.
- actual: The live tool exposes only `query`, `entity`, `predicate`, `limit`, and `as_of`; a direct request to use `mode=recent` is rejected because `mode` is not in the tool schema. A semantic call with query `Davide` returns only the durable fact that the user has a dog named Argo. A follow-up asking about conversations makes no tool call and emits obsolete prose claiming there is no historical text archive.
- errors: "lo strumento memory__memory_recall non dispone di un parametro chiamato mode"
- timeline: Observed in the running cockpit during Phase 49 execution on 2026-09-01. The user reports the expected historical conversation graph is present, but the cockpit has not demonstrated the new browse mode.
- reproduction: Open the running cockpit and send `usa memory recall con mode=recent`. Inspect the tool-call schema/arguments and final response. Separately ask `cosa vedi delle conversazioni precedenti?` and observe whether `memory_recall` is called with browse mode.

## Current Focus

- hypothesis: Confirmed root cause: Compose kept the independently pinned pre-Phase-49 `arcadedb-mcp` image, so the newer Aura process mounted the five-field schema that old binary genuinely advertises.
- test: Rebuilt both images from current HEAD, recreated `arcadedb-mcp` first, waited for health, recreated Aura second, confirmed its stamped commit and fresh memory mount, then the user repeated the cockpit flow.
- expecting: Satisfied. The mounted contract exposes the Phase 49 browse modes and the user's native cockpit `mode=recent` test works.
- next_action: none
- bug_class: bohrbug
- known_pattern_candidate: none; `.planning/debug/knowledge-base.md` is absent and MemPalace recall is unavailable in this runtime
- reasoning_checkpoint:
- tdd_checkpoint:
    test_file: scripts/agent_memory_eval_test.py
    test_name: MixedTierRecallEvaluatorTest.test_stale_memory_recall_schema_fails
    status: red
    failure_output: "AssertionError: True is not false (missing mode property; missing recent mode; observed five-field legacy schema)"

## Evidence

- timestamp: 2026-09-01T00:10:00+02:00
  checked: Phase 0 knowledge recall
  found: MemPalace tools are unavailable in this runtime and `.planning/debug/knowledge-base.md` does not exist.
  implication: There is no known-pattern candidate to privilege; continue from direct runtime evidence.

- timestamp: 2026-09-01T00:15:00+02:00
  checked: Current `cmd/arcadedb-mcp` source and tool registration
  found: `MemoryRecallInput` contains `mode`, conversation selectors, cursor, direction, and trace fields; `addMemoryRecallTool` constrains mode to semantic/recent/open/scroll/reasoning and `newServer` registers that tool.
  implication: The checked-out source contract is newer than the schema observed in the cockpit.

- timestamp: 2026-09-01T00:15:01+02:00
  checked: `compose.yaml` and `docker/arcadedb-mcp/Dockerfile`
  found: The Dockerfile builds current repository source, but Compose defaults `arcadedb-mcp` to immutable image `3c1723cc...@sha256:621438...` with `pull_policy=missing`; a local rebuild must explicitly replace/recreate that runtime artifact.
  implication: A pre-Phase-49 immutable default image is a concrete environment candidate for the stale tool contract.

- timestamp: 2026-09-01T00:20:00+02:00
  checked: Live Docker Compose state and container/image metadata
  found: `aura-arcadedb-mcp` is healthy but runs image digest `sha256:621438...` with OCI revision `3c1723cc...`; the image was built 2026-08-26 and the container started 2026-08-31. It has no source bind mounts. Aura itself was rebuilt later as local image `sha256:f1f27...` and restarted at 2026-08-31T21:26:55Z.
  implication: New Aura bridge code is talking to an independently old memory sidecar binary. Source changes cannot reach that sidecar without a rebuild and recreation.

- timestamp: 2026-09-01T00:30:00+02:00
  checked: Full `cmd/arcadedb-mcp/tool_memory.go` at live image revision `3c1723cc5d3c4a23c9585c96b9c35d3d79416050`
  found: That revision defines `MemoryRecallInput` with exactly `query`, `entity`, `predicate`, `limit`, and `as_of`; it has no mode or conversation-browse selectors, exactly matching the cockpit's enumerated schema.
  implication: The sidecar image alone is sufficient to cause the observed stale contract. The Aura bridge did not need to remove or cache any new fields.

- timestamp: 2026-09-01T00:30:01+02:00
  checked: New-schema string search in the running stripped binary
  found: None of the new mode/recent/conversation selector description literals were present. The negative binary search is consistent with, but weaker than, the exact OCI-revision source comparison.
  implication: No evidence contradicts the stale-binary diagnosis.

- timestamp: 2026-09-01T00:35:00+02:00
  checked: Git history for the unified recall schema
  found: The mixed-tier `mode` contract landed in commits `dee0cad4`/`a1cd8080`/`ceb67eb1` on 2026-09-01, after image revision `3c1723cc`; current explicit reasoning and exclusion changes landed later still.
  implication: The image/source chronology directly explains why the running binary cannot expose Phase 49 browse modes.

- timestamp: 2026-09-01T00:35:01+02:00
  checked: `.planning/phases/49-memory-tiers/49-11-PLAN.md`
  found: The unexecuted final plan explicitly requires a stale-container-image failure fixture and a no-skip conversation against Aura built from the current Phase 49 tree; the supplied workspace context confirms Plan 49-11 made no commits.
  implication: The missing live-image guard is an acknowledged but not-yet-implemented acceptance seam, so an in-process MCP test could pass while the cockpit remained stale.

- timestamp: 2026-09-01T00:35:02+02:00
  checked: SBFL eligibility and common bug patterns
  found: Before this regression is added there is no failing per-test coverage spectrum for the deployed-container symptom, so SBFL is skipped. The symptom matches Environment/Config (immutable old image plus missing runtime rebuild) and Data Shape/API Contract (old advertised schema), and is deterministic.
  implication: Classify as Bohrbug and use deterministic differential/runtime-layer testing; no flaky-spectrum revocation is needed.

- timestamp: 2026-09-01T00:45:00+02:00
  checked: Isolated RED regression `python -m unittest scripts.agent_memory_eval_test.MixedTierRecallEvaluatorTest.test_stale_memory_recall_schema_fails`
  found: The test failed in all three subcases with `AssertionError: True is not false`; the evaluator accepted a schema missing `mode`, one missing enum member `recent`, and the exact five-field legacy schema observed in the cockpit.
  implication: The regression reproduces the missing stale-runtime guard and is RED before any production/evaluator fix.

- timestamp: 2026-09-01T06:24:34+02:00
  checked: Rebuilt and recreated live services in dependency order
  found: `aura-arcadedb-mcp:local` and `aura:local` were built from HEAD `6d4cd866e933df0040a68062e2a64b4d1e3512ee`; the sidecar reached healthy before Aura was recreated and reached healthy.
  implication: Aura mounted the new server contract instead of freezing the old schema again at boot.

- timestamp: 2026-09-01T06:24:58+02:00
  checked: Fresh mounted memory tool inventory
  found: `aura mcp tools memory` describes semantic, recent, open, scroll, and reasoning on `memory_recall`; Aura reports commit `6d4cd866e933df0040a68062e2a64b4d1e3512ee`.
  implication: The stale binary and stale boot mount are both replaced.

- timestamp: 2026-09-01T06:30:00+02:00
  checked: Native cockpit UAT by the user
  found: The user reports `mode=recent` now works after the container update.
  implication: The original end-to-end symptom is resolved on the real operator path.

- timestamp: 2026-09-01T00:00:00+02:00
  finding: The user supplied a graph screenshot containing many `Conversation` and `ConversationTurn` vertices and relations, excluding an empty projection as the primary explanation.
- timestamp: 2026-09-01T00:00:01+02:00
  finding: The cockpit executed `memory__memory_recall` with semantic query `Davide` and returned only the Argo fact; a follow-up about conversations did not call a tool.
- timestamp: 2026-09-01T00:00:02+02:00
  finding: A direct cockpit request for `mode=recent` was rejected, and the model enumerated the live schema as `query`, `entity`, `predicate`, `limit`, and `as_of`, proving that the mounted live tool contract predates the Phase 49 mode field.

## Eliminated

- hypothesis: No historical conversation data was projected for the identity.
  reason: The user directly showed populated `Conversation` and `ConversationTurn` graph data. Counts and ownership still need measurement, but absence cannot explain the missing live `mode` parameter.

## Resolution

- root_cause: The running `aura-arcadedb-mcp` container used immutable image revision `3c1723cc`/digest `sha256:621438...`, whose compiled `MemoryRecallInput` has only query/entity/predicate/limit/as_of. Phase 49 added mode and conversation-browse selectors afterward, but the sidecar was neither rebuilt nor recreated; the newer Aura process therefore mounted the old server contract.
- fix: Built the current repository into `aura-arcadedb-mcp:local` and `aura:local`, recreated the memory sidecar first, waited for health, then recreated Aura so its boot-time MCP registry remounted the new schema. Added a RED evaluator regression for the exact five-field legacy surface for Plan 49-11 to complete.
- verification: Sidecar and Aura health are green; Aura is stamped with HEAD `6d4cd866e...`; the fresh memory mount advertises all Phase 49 modes; the user confirmed the real cockpit `mode=recent` flow works.
- files_changed:
  - scripts/agent_memory_eval_test.py
- oracle_type: derived (the current checked-in `MemoryRecallInput` and Plan 49 live acceptance define the required properties and enum)
