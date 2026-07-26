# Spike 106: adaptive Aura portability

This directory freezes the inputs and schemas for Task 10. It contains no
measured result yet. A report belongs under `artifacts/` only after the real
Aura binary completes all 52 turns and all 16 controls.

## Risk boundary

`qwen-portability` proves only that Aura can traverse its production
`Runner.Turn` path through llama.cpp. It never supplies quality, promotion, or
admission evidence. `production-shadow` is the only report mode that the
promotion-evidence constructor accepts, and the benchmark command itself does
not mutate serving policy. Even a passing production report becomes promotion
evidence only when it was finalized from core-verified scenario and control
runs; decoded or manually assembled pass records are structurally rejected.

The Qwen run uses the existing loopback service and only a reversible database
settings override:

- provider: `llamacpp`
- model alias: `qwen`
- OpenAI-compatible base URL: `http://127.0.0.1:18081/v1`
- source: `unsloth/Qwen3.5-2B-GGUF`
- artifact: `Qwen3.5-2B-Q4_K_M.gguf`
- required artifact SHA-256:
  `aaf42c8b7c3cab2bf3d69c355048d4a0ee9973d48f16c731c0520ee914699223`

The harness does not modify `.env`, Compose, container credentials, or a
non-loopback listener.

## Frozen corpus

`dataset.json` is the canonical JSON encoding of
`aura/adaptive-aura-benchmark-dataset`, revision 1. It contains exactly 52
synthetic scenarios:

- `r01..r12`: reasoning
- `t01..t12`: tool discovery
- `s01..s12`: queried skill routing
- `k01..k08`: document/knowledge retrieval
- `m01..m08`: owner-scoped long-term memory recall

The first 44 prompts and expected values are byte-equivalent to spike 097. The
eight memory scenarios use run-scoped synthetic facts and preferences. No user
conversation or private production content is part of this corpus.

The canonical dataset SHA-256, excluding the optional final LF, is:

`ab65400e241a9a000f206f0aa360acc9d43272acee9cd69906e14439fce06df0`

The production binary embeds a byte-identical runtime copy under
`internal/eval/testdata`. Unit tests compare the two artifacts byte for byte
and recheck this digest, so an installed binary never depends on a
repository-relative `.planning` path and the duplicate cannot drift.

Seed `10620260726` orders scenarios by ascending SHA-256 of
`uint64be(seed) || 0x00 || scenario_id`, with `scenario_id` as the tie-breaker.
The runtime executes one scenario at a time. The four-client control is
separate and excluded from latency percentiles.

## Registered action catalogs

The exact production catalog order is:

1. reasoning: `reasoning_high`, `reasoning_low`, `reasoning_none`, `static`
2. tool discovery: `bm25`, `semantic`, `static`
3. skill routing: `catalog`, `static`
4. knowledge retrieval: `sparse`, `static`, `vector`, `vector_rerank`,
   `vector_rerank_expand`
5. memory recall: `static`, `long_term_top_4`, `long_term_top_8`

The SHA-256 of the canonical ordered catalog array is:

`c6161a81f0e636d3fafb73a35fffb1665d0a70f69227b8db0051b8a8d8d34763`

## Deterministic checker

The evaluator is `aura/adaptive-deterministic-checker`, revision 1.

- Reasoning and knowledge answers are lowercased, outer
  `` ` ' " . , ; : ! ? $ € `` punctuation is trimmed, whitespace is collapsed,
  the whole tokens `seven`, `eighteen`, and `ninety` become `7`, `18`, and
  `90`, and the connector ` at ` is removed. Equality is exact after those
  transformations; explanations containing the expected answer do not pass.
- Tool and skill IDs require exact byte equality.
- Memory result IDs require exact array equality, including rank order.

The Go positive and negative goldens are the executable authority for every
normalization rule.

## Canonical report

`schemas/benchmark-report.schema.json` describes the registered JSON shape.
The Go decoder additionally enforces properties JSON Schema cannot express:
field order, duplicate-key rejection, exact float re-encoding, registered
scenario/control order, binary64 probability sum, exact static exposure,
summary recomputation, provenance groups, and unique fact linkage.

The report contains IDs, registered metadata, hashes, and aggregate metrics. It
has no prompt, expected answer, conversation, tool result, memory/document
content, owner attribute, endpoint URL, or secret. The subject driver receives
only scenario ID, domain, and prompt. The core retains the expected value,
evaluates the transient result, and only then asks the driver to persist that
binary evaluation; the outcome recorder never receives gold.

Latency is measured by the driver from immediately before the exact
`Runner.Turn` call through its terminal event. Wrapper setup, ledger
post-processing, deterministic evaluation, and outcome persistence are
excluded. Unknown/local cost is `null`; provider-reported and price-table
costs retain distinct transient provenance, and calculated costs bind the
exact price-table ID, revision, and digest.

Nearest-rank over positive integer nanosecond samples is used for p50, p95, and
p99. A zero latency is missing and is allowed only on an invalid scenario.

## Fixed controls

The report's `controls` array is positional and has exactly this order:

1. `negative_control`
2. `static_equivalence`
3. `restart_recovery`
4. `rollback_static`
5. `privacy_redaction`
6. `retention_cleanup`
7. `same_owner_concurrency`
8. `cross_owner_isolation`
9. `assignment_write_failure`
10. `delivery_write_failure`
11. `outcome_write_failure`
12. `model_transport_failure`
13. `memory_epoch_mismatch`
14. `settings_apply_failure`
15. `settings_restore_failure`
16. `stale_override_recovery`

`schemas/control-matrix.schema.json` freezes the C0..C3 identities, blocking
point, release order, and assertions. Every wait is bounded by the cmd/aura
adapter; a timeout is invalid rather than skipped.

The core control runner invokes all 16 cases in registered order. Its
transient concurrency evidence validates exact owner/conversation identities,
unique request/assignment/delivery IDs, assignment-before-exposure,
conversation blocking, release timing, one delivery per exposure, and static
champion probability `1.0`. Only the resulting content-free fact IDs enter the
canonical report.
