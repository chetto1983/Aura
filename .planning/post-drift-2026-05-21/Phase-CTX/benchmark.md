# Phase-CTX Benchmark Contract

**Role:** benchmark and slice-QA contract for Phase-CTX closure.
**Status:** US-CTX-08 benchmark passed 2026-05-24.
**Ground-truth rule:** a row can pass only by asserting artifact bytes,
SQLite/API facts, provider/tool response fields, or committed fixture data.
Smoke checks are prechecks only.

## Bench Rows

| ID | Slice | Type | Command / Probe | Fixture / Data | Expected Ground Truth | Pass / Fail Threshold | PRD Gate | Current Result |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| B-CTX-06 | US-CTX-06 | completed code tests | `go test ./internal/conversation ./internal/agent ./internal/chat -count=1` | existing unit tests | Prefix messages preserved, hysteresis blocks repeated compaction, focus topic capped. | command passes and delta lint has 0 new issues | robustness closure | passed 2026-05-24 in commit `0742d3ac` |
| B-CTX-07-1 | US-CTX-07A | fixture parser | `go test ./cmd/bench_ctx -run TestLoadFixtures -count=1` | `internal/conversation/testdata/bench/*.json` | Exactly four fixture names load: `short_qa`, `tool_heavy_research`, `multimodal_visual`, `long_session`; expanded message counts match fixture metadata. | pass; malformed fixture negative test must fail with a useful error | repeatable dataset | passed 2026-05-24 |
| B-CTX-07-2 | US-CTX-07A | deterministic offline bench | `go run ./cmd/bench_ctx --offline --fixtures internal/conversation/testdata/bench --out .planning/post-drift-2026-05-21/Phase-CTX/bench-results-offline-2026-05-24.json` | committed bench fixtures | JSON artifact contains `model`, `fixture`, `pre_tokens`, `post_tokens`, `savings_pct`, `latency_ms`, `quality_keyword_retained`, and `recommended_threshold_pct` for every fixture row. | artifact exists; schema validates; at least one offline fixture has positive savings and retained keyword | bench artifact shape | passed 2026-05-24; rows=12, compacted_rows=3, gate_passed=true, best_savings_pct=99 |
| B-CTX-07-3 | US-CTX-07B | live model bench | `docker compose --profile test run --rm -e AURA_LLM_API_KEY -e LLM_API_KEY -e OPENROUTER_API_KEY -e OPENROUTER_KEY -e AURA_LLM_BASE_URL -e LLM_BASE_URL -e OPENROUTER_BASE_URL test go run ./cmd/bench_ctx --offline=false --fixtures internal/conversation/testdata/bench --models deepseek/deepseek-v4-flash,google/gemma-4-26b-a4b-it,anthropic/claude-sonnet-4 --out .planning/post-drift-2026-05-21/Phase-CTX/bench-results-2026-05-24.json` | same fixtures, live OpenRouter-compatible provider | JSON artifact has 12 model x fixture rows plus model context windows and provider usage fields when available. | command passes or explicitly skips/blocks when credentials are absent; no secret values printed | live validation | passed 2026-05-24 with `--build`; rows=12, errors=0, gate_passed=true |
| B-CTX-07-4 | US-CTX-07B | quality gate | inspect `.planning/post-drift-2026-05-21/Phase-CTX/bench-results-2026-05-24.json` | live bench JSON | At least one row has `savings_pct > 40` and `quality_keyword_retained=true`. | GO if true; HOLD if false | compaction earns production value | passed 2026-05-24; DeepSeek and Claude long_session have 99% savings with quality=true; Gemma long_session quality=false |
| B-CTX-07-5 | US-CTX-07B | threshold recommendation | inspect live bench JSON | live bench JSON | `recommended_threshold_pct` exists per model and records whether 50% should stay or be tuned. | all three active models have a recommendation; no code threshold change in same slice | tuning evidence | passed 2026-05-24; long_session recommends DeepSeek=50, Gemma=40, Claude=50 |
| B-CTX-07-6 | US-CTX-07B | quality snapshot | `rg -n "Phase-CTX Compaction Benchmark" docs/aura-quality-snapshot.md` plus table inspection | `docs/aura-quality-snapshot.md` | New dated section summarizes fixture/model cells as `savings_pct/latency_ms/quality_pass` and links the JSON artifact path. | append-only section added; prior rows unchanged | product quality matrix | passed 2026-05-24 |
| B-CTX-07-7 | US-CTX-07A/B | dedicated slice QA | `git diff -- cmd/bench_ctx internal/conversation/testdata/bench docs/aura-quality-snapshot.md .planning/post-drift-2026-05-21/Phase-CTX` plus targeted tests above | completed slice diff | QA packet records files inspected, artifact/schema fact, and negative fixture or missing-credential check. | PASS required before each atomic commit | slice QA | US-CTX-07A self-audited PASS 2026-05-24; US-CTX-07B live-prep self-audited PASS 2026-05-24; US-CTX-07B live snapshot self-audited PASS 2026-05-24 |
| B-CTX-08-1 | US-CTX-08 | storage/API | `go test ./internal/conversation ./internal/api -run Compaction -count=1` with handler-level `GET /conversations/{id}/compactions` probe | SQLite conversation row plus one `conversation_compactions` event | SQLite row and API response agree on conversation id, turn index, token fields, latency, timestamp, and redacted focus preview. | pass plus unauthorized/missing conversation negative check | debuggability | passed 2026-05-24; API 200/404/401 paths covered |

## Fixture Contract For US-CTX-07A

| Fixture | Required Shape | Expected Quality Keyword | Purpose |
| --- | --- | --- | --- |
| `short_qa.json` | 10 turns, no tool results | fixture-owned keyword | proves the harness does not over-compress small conversations |
| `tool_heavy_research.json` | 30 turns plus five 8000-character tool results, literal or repeat-expanded | fixture-owned keyword inside preserved task premise | proves savings on tool-result bloat |
| `multimodal_visual.json` | 10 turns plus eight `image_url` style messages | fixture-owned keyword in text around images | proves image token estimates are represented |
| `long_session.json` | 100 mixed turns | fixture-owned early premise keyword | proves long-session continuity |

Fixture JSON may use a small repeat-expansion field if the expanded message
counts and character counts are asserted by tests. The run artifact must report
expanded counts, not only compact fixture definitions.

## Live Credential Rule

US-CTX-07B may read keys from `AURA_LLM_API_KEY`, `LLM_API_KEY`,
`OPENROUTER_API_KEY`, or `OPENROUTER_KEY`. The harness must never print key
values. If no live key is present, the live row is `blocked`, not downgraded to
a smoke pass.

## Slice-QA Packet Template

```text
Reviewer mode: self-audited-slice-qa unless a fresh verifier is available
Files inspected:
Diff inspected:
Benchmark row:
Commands/probes:
Ground-truth fact:
Negative/adversarial check:
Verdict: PASS/HOLD
Residual risk:
```

## Residue Gate

Before any CTX closure commit:

- `git status --short --untracked-files=all` must be inspected.
- Only files owned by the current slice may be staged.
- Generated live JSON is kept under this phase folder only when it is the
  committed benchmark artifact; scratch output stays untracked or is removed.
- No Docker service started by the slice may be left in an unexpected state.
- No secrets, raw prompts, or private payloads may be committed.
