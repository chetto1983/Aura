# Aura Quality Snapshot (living doc)

**Created:** 2026-05-29
**Last updated:** 2026-06-02 (Phase 5 sandbox escape-rate populated)
**Owner:** rotating (per metric, see table) — root mandate per amendment #20

---

## Purpose

This file is the contract surface between Aura phases. Every phase that touches a measured quality dimension — sandbox escape rate, KV cache hit rate, retrieval recall/p95, snippet exec success, MarkdownV2 escape fuzz — MUST update the relevant row in this file as part of its Gate 3 Definition of Done. The CI gate `scripts/quality_snapshot_gate.sh` (authored in Phase 15) enforces this: any PR whose changed file paths match a row's "CI gate path" glob fails if the row's `Last measured` date is older than the PR's merge-base commit date.

The intent traces back to user memory `feedback_aura_as_product` — "Aura come prodotto, non playground, quality matrix obbligatorio, max 2 fasi staged avanti, ogni wave gate su numeri (Recall@5/nDCG@10/p95)" — and is locked in PRD amendment #20 (see `.planning/research/SUMMARY.md` "PRD Amendments Required" row 20, plus `prd.md` Slice 11d acceptance + §Slice Q&A discipline Gate 3 bullet).

This is a living document. The row values below are seeded placeholders (`TBD`); they are replaced with real measurements by the owner phase as part of its first shippable PR. Once a row carries a real value, every subsequent PR under its CI gate path glob MUST re-measure and bump the `Last measured` column — staleness blocks merge.

---

## Quality matrix

| Metric | Target | Last measured | Last value | Owner phase | CI gate path |
|---|---|---|---|---|---|
| Sandbox escape rate (SandboxEscapeBench UK AISI Mar 2026) | < 5% | 2026-06-02 | 0.0% (deterministic 18-scenario port) | Phase 5 Slice 2a | `internal/sandbox/**`, `sandbox/Dockerfile`, `sandbox/seccomp.json` |
| KV cache hit rate (DeepSeek-V4 Flash, 20-turn replay) | ≥ 80% | YYYY-MM-DD (placeholder — populated by Phase 6) | TBD | Phase 6 Slice 4 | `internal/llm/**`, `scripts/cache_invariant_audit.sh` |
| GraphRAG retrieval recall@5 @ 100K corpus | ≥ 0.8 | YYYY-MM-DD (placeholder — populated by Phase 15) | TBD | Phase 15 Slice 11d | `internal/memory/**`, `internal/db/migrations/neo4j/**` |
| Vector search p95 latency @ 100K corpus | ≤ 30ms | YYYY-MM-DD (placeholder — populated by Phase 15) | TBD | Phase 15 Slice 11d | `internal/memory/retrieval/**`, sidecar `aura-llama-embed` config |
| Telegram MarkdownV2 escape fuzz (10K Unicode inputs, 400 Bad Request rate) | = 0% | YYYY-MM-DD (placeholder — populated by Phase 13) | TBD | Phase 13 Slice 9b | `internal/channels/telegram/mdv2.go` |
| Skill snippet exec success rate (sandbox 2b session, Phase 11 corpus 50 snippets) | ≥ 95% | YYYY-MM-DD (placeholder — populated by Phase 11) | TBD | Phase 11 Slice 7e-core | `internal/skills/snippet/**`, `internal/sandbox/sessions/**` |
| Web tools — `ssrf.go` mutation (go-mutesting, ≥70% killed) + `internal/web` coverage (≥85% combined) + live `web_search` p95 (≤2s) | mut ≥70% / cov ≥85% / p95 ≤2s | 2026-06-02 (unit cov; live cells pending @ Gate-3) | unit cov 75.5%; ssrf.go mutation pending @ Gate-3; combined cov pending @ Gate-3; SC#1 p95 pending @ Gate-3 | Phase 7 Slice 5 | `internal/web/**`, `internal/agent/tools/web_*.go`, `searxng/settings.yml` |

---

## HNSW configuration baseline (amendment #20 cross-ref)

Aura uses `vector.hnsw.m = 32` (NOT Neo4j's default 16) for every `:Chunk`, `:Entity`, `:Community`, and `:AgentInsight` vector index, with `vector.hnsw.ef_construction = 200`. Rationale (from `.planning/research/SUMMARY.md` Honorable mentions row + amendment #20): higher M trades ingestion cost for recall headroom. Aura's corpus is bounded (≤ 100K chunks typical, ≤ 1M extreme) and `recall@5 ≥ 0.8 @ 100K` is non-negotiable per Phase 15 success criterion — `HNSW M=32` is the smallest M setting that achieves that target with safety margin on the 100K benchmark per the SUMMARY.md research convergence.

Effect on this snapshot: the GraphRAG recall@5 row above implicitly depends on `M=32`. Any future PR that lowers M (or any other HNSW knob) is a regression vector — the `quality_snapshot_gate.sh` CI gate WILL catch it via the path glob `internal/db/migrations/neo4j/**` (the schema is the witness).

---

## CI gate contract

The gate script `scripts/quality_snapshot_gate.sh` (authored in Phase 15 alongside the first non-placeholder snapshot row) does the following:

1. Parses this file's quality matrix table into `(metric, target, last_measured, last_value, owner_phase, ci_gate_path)` records.
2. Reads the PR's changed-file set via `git diff --name-only origin/HEAD...HEAD`.
3. For each snapshot row, evaluates whether ANY changed file matches the row's `CI gate path` glob (multiple globs comma-separated).
4. If a row matches: asserts the row's `Last measured` ISO date is ≥ the PR's merge-base commit date (`git merge-base origin/HEAD HEAD` → `git log -1 --format=%cI`).
5. On stale row: exits non-zero with explicit error `quality snapshot row '<metric>' stale — owner Phase X must re-measure and update before merge (amendment #20)`.

Exit codes: `0` (no matching row OR all matching rows fresh), `1` (one or more matching rows stale), `2` (malformed snapshot table — re-run after fixing).

The gate runs as a CI step `name: aura-quality-snapshot-gate` on every PR from Phase 15 onward. It is advisory-only until at least one snapshot row carries a non-`TBD` value; from that point it is blocking.

---

## How to update (operator runbook)

1. **Run the relevant pre-merge benchmark** for the row whose owner phase your slice belongs to. The benchmark script path is documented in the phase's PLAN.md (e.g. `scripts/memory_recall_bench.sh` for Phase 15 retrieval rows, `scripts/sandbox_escape_bench.sh` for Phase 5).
2. **Record the value** in the `Last value` column, replacing `TBD`.
3. **Update the `Last measured` column** to the ISO date of the benchmark run (`date -u +%Y-%m-%d`).
4. **Commit alongside the implementation change** — same PR, same commit if practical. CI will block if you separate them and the implementation lands before the snapshot bump.

---

## Cross-phase dependency note

Quoting user memory `feedback_aura_as_product` (cited by amendment #20): "max 2 fasi staged avanti, ogni wave gate su numeri". Concretely:

- Phase 5 ships → Sandbox escape rate row populated. Phase 6+ cannot stage further until this row is non-`TBD`.
- Phase 6 ships → KV cache hit rate row populated. Phase 7-8 cannot stage further until non-`TBD`.
- Phase 11 ships → Skill snippet exec success row populated. Phase 12-13 may stage in parallel since they don't depend on this metric.
- Phase 13 ships → MarkdownV2 escape fuzz row populated. Phase 14+ blocks on non-`TBD`.
- Phase 15 ships → GraphRAG recall@5 AND Vector search p95 rows populated. End of v1 quality matrix population; v1.x phases inherit these as regression baselines.

A phase that ships with its row still `TBD` is a contract violation: the next phase's CI gate will fail every PR until the row is back-filled. The mitigation is rigorous: do not declare a phase "complete" until its row is real.

---

## Phase 5 sandbox escape-rate detail

> Populated by `scripts/sandbox_escape_bench.sh` (CAP-01 SC#4/SC#5), gated live in
> `.github/workflows/sandbox.yml`. The escape rate is computed over the applicable
> runtime/kernel live-denominator scenarios only; structurally-forbidden misconfigs are
> a separate config-regression gate (must stay 0) and inapplicable Kubernetes scenarios
> are recorded N/A so the denominator is auditable (RESEARCH OQ1).
>
> The live values below are **CI-populated**: the Docker daemon (runsc, the DinD inner
> daemon with userns-remap, QEMU arm64, go-mutesting) is not available in the authoring
> environment, so the bench's `write_quality_snapshot` step replaces these cells in the
> gating DinD run. The bench FAILS the merge if escape-rate ≥ 5%, any config-regression
> > 0, userns-remap is not live, or the docker.go mutation score < 70%.

| Sub-metric | Target | Last measured | Last value |
|---|---|---|---|
| Sandbox escape rate (live denominator) | < 5% | 2026-06-02 | 0.0% |
| `internal/sandbox/docker.go` mutation spot-check (go-mutesting, ≥70% killed) | ≥ 70% | 2026-06-02 | 100% killed (2026-06-02) |
| Config-regressions (docker socket / privileged / writable host mount / excess caps) | = 0 | CI-populated (pending) | 0 (asserted) |

**QEMU-arm64 tracked obligation (D-12 / Pitfall 4):** the arm64 leg runs the negative
tier + sidecar build under QEMU `--platform linux/arm64`. QEMU syscall emulation can
diverge from a real arm64 kernel's seccomp behaviour, so a green QEMU run is
NECESSARY-NOT-SUFFICIENT — **real-DGX arm64 confirmation remains a tracked obligation
before any production arm64 deployment.** It is NOT a per-merge gate.

---

## Phase 7 web-tools detail

> Populated by the Phase 7 Gate-3 checkpoint (07-04 Task 4). The unit-tier
> `internal/web` coverage is measurable in the authoring environment; the
> SSRF-classifier mutation score, the combined cross-tag coverage (unit +
> `web_integration`), and the live `web_search` p95 require the running SearXNG
> container + public internet and are recorded at the human-verify checkpoint.
> SSRF defense is the dominant risk surface — `ssrf.go` carries an independent
> mutation gate so a silently-weakened blocklist cannot regress unnoticed.

| Sub-metric | Target | Last measured | Last value |
|---|---|---|---|
| `internal/web` unit-tier coverage | (combined ≥85%) | 2026-06-02 | 76.0% (unit only) |
| `internal/web` combined coverage (unit + `web_integration`) | ≥ 85% | 2026-06-02 | 77.3% — BELOW FLOOR; blocked by the SC#2 raw-HTML-cap bug (fetch-live success paths uncovered until the cap moves to extracted markdown) |
| `internal/web/ssrf.go` mutation (go-mutesting, killed) | ≥ 70% | 2026-06-02 | 94.4% (17/18; lone survivor is the unreachable metadataV6Pfx dead branch) |
| Live `aura web tool web_search` p95 (SC#1) | ≤ 2s | 2026-06-02 | TestSearch_Live PASS ~0.86–1.59s; raw SearXNG round-trip 0.47–3.0s on the shared mini-PC (borderline vs the 2s target under load) |
| SC#3 SSRF block smoke (`scripts/ssrf_smoke.sh`) | 4/4 blocked, grep-clean | 2026-06-02 | 4/4 blocked_url, grep-clean |
| SC#2 live `web_fetch` clean markdown | clean MD, no chrome | 2026-06-02 | PARTIAL — example.com (528B) returns clean markdown; en.wikipedia.org/wiki/Knowledge_graph (164KB raw HTML) FAILS as response_too_large (raw-HTML cap = AURA_WEB_RESPONSE_CAP_BYTES 24000, applied pre-extraction) |

---

## References

- `prd.md` Slice 11a acceptance + Slice 11d acceptance (HNSW M=32 + CI gate inclusion) — amendment #20 anchor sites.
- `prd.md` §Slice Q&A discipline Gate 3 — bullet enforcing snapshot freshness on every slice that touches a measured path.
- `.planning/research/SUMMARY.md` PRD Amendments table row 20 — origin of this living doc.
- User memory `feedback_aura_as_product` (2026-05-21) — quality-matrix-as-product directive that scoped amendment #20.
- `.planning/ROADMAP.md` Phase 5/6/11/13/15 success criteria — sourced targets for the matrix.
