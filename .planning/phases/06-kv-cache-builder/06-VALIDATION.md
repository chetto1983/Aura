---
phase: 6
slug: kv-cache-builder
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-02
validated: 2026-06-02
---

# Phase 6 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution. Derived from `06-RESEARCH.md` §Validation Architecture. The 5 ROADMAP success criteria are the requirement set (CAP-04).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + table-driven (`golang-testing` skill); `goleak` + `-race` per project discipline |
| **Config file** | none (Go convention) — build tag `db_integration` for the metrics tier |
| **Quick run command** | `go test ./internal/agent/prompt/...` |
| **Full suite command** | `go test -race -count=1 ./... && go test -tags db_integration -race ./internal/db/... ./internal/runner/... && bash scripts/cache_invariant_audit.sh` |
| **Estimated runtime** | ~30s unit/race; +integration tier needs Postgres up |

---

## Sampling Rate

- **After every task commit:** `go test ./internal/agent/prompt/... && go vet ./... && go build ./...`
- **After every plan wave:** full unit `-race` + `db_integration` metrics tier + `bash scripts/cache_invariant_audit.sh`
- **Before `/gsd-verify-work`:** full suite green + new CI "cache invariant gate" step green + combined coverage ≥85% (CLAUDE.md floor, full tag matrix)
- **Max feedback latency:** ~30 seconds (unit/quick path)

---

## Per-Task Verification Map (5 ROADMAP Success Criteria → tests)

| SC | Behavior | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|----|----------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| SC#1 | SHA-256(messages[0]) constant across 20-turn replay, printed to stdout | 0 | CAP-04 | T-06-01 (prefix poisoning) | invariant hash on real `runner.Turn` loop | smoke (runtime-faithful) | `bash scripts/cache_invariant_audit.sh` | ✅ | ✅ green |
| SC#1u | `PromptBuilder.Build` byte-identical [0] over N turns + monotonic history growth + no in-place mutation | 0 | CAP-04 | T-06-01 | — | unit | `go test ./internal/agent/prompt/ -run TestBuildPrefixStable` | ✅ | ✅ green |
| SC#2 | `usage.prompt_cache_hit_tokens` (CachedTokens) rises from turn 2 (provider-side, NOT CI-gated) | — | CAP-04 | — | N/A | manual-only (live DeepSeek) | manual `aura chat send` ×3 + `aura cache-stats` | n/a (manual) | ⏸ manual-only |
| SC#3 | Anthropic-provider build carries `cache_control` on system+tools, NOT history; OpenRouter build carries none | 0 | CAP-04 | T-06-03 (secret leak via wire) | dormant no-op seam under OpenRouter | unit (wire-shape) | `go test ./internal/agent/prompt/ -run TestCacheControlSeam` | ✅ | ✅ green |
| SC#4 | `aura cache-stats --since=1h` returns the window; hit-rate ≥80% is *measured* not gated | 0 | CAP-04 | T-06-02 (SQLi via --since) | `time.ParseDuration` + sqlc parameterized | integration (query) + manual (the 80% number) | `go test -tags db_integration ./internal/db/... -run TestCacheMetrics` | ✅ | ✅ green |
| SC#5 | CI fails with explicit "messages[0] mutated at <site>" on a poisoning PR | 0 | CAP-04 | T-06-01 | negative-path proof the gate is not silently green | smoke (negative) | `scripts/cache_invariant_audit.sh` exits 1 + explicit message when a fixture mutates [0] | ✅ | ✅ green |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/agent/prompt/builder_test.go` — SC#1u + SC#3 (byte-identity, monotonic growth, no-mutation, cache_control seam, index-set hash)
- [ ] `scripts/cache_invariant_audit.sh` + `scripts/fixtures/cache_invariant/turn-{01..20}.json` — SC#1 (smoke) + SC#5 (negative); fixtures MUST include tool-call turns and be deterministic
- [ ] `internal/db/.../cache_metrics_integration_test.go` (build tag `db_integration`) — SC#4 (INSERT + `--since` window + aggregate)
- [ ] `cmd/aura/cache_test.go` — `cache-stats` flag parsing (`--since` → duration), `cache-audit` exit-code contract (0/1/2)
- [ ] CI wiring: `.github/workflows/ci.yml` step `name: cache invariant gate` invoking the wrapper (Postgres-free job)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Cache warming — `CachedTokens` rises from turn 2 (SC#2) | CAP-04 | Provider-dependent + flaky; PRD OQ4 says hit-rate is measured, not CI-gated | Run `aura chat send "hello"` ×3 on DeepSeek-V4 Flash; observe `usage.prompt_cache_hit_tokens` monotonically rising turn 2+ |
| Hit-rate ≥80% target (SC#4 number) | CAP-04 | Same provider-dependence; the *query* is integration-tested, the *80%* is a live read | After a typical session, `aura cache-stats --since=1h`; observe hit rate ≥80% |
| Mutation spot-check (CLAUDE.md gate) | CAP-04 | `go-mutesting` runs on WSL, not CI | `go-mutesting` on `internal/agent/prompt/builder.go` + `hash.go` — ≥70% killed (cache-load-bearing files). **Recorded 2026-06-02 (table below).** |

### Mutation scores (recorded 2026-06-02 — WSL go-mutesting, avito fork, go1.26)

| Critical file | Score | Killed / Total | Gate ≥0.70 |
|---------------|-------|----------------|-----------|
| `internal/agent/prompt/builder.go` | **1.000** | 1 / 1 | ✅ |
| `internal/agent/prompt/hash.go` | **0.714** | 5 / 7 | ✅ |

> `builder.go` carries only struct-assembly logic (1 mutable point, killed). `hash.go` 2 accepted-equivalent survivors on the canonicalisation byte-walk (no test can distinguish; output hash is byte-identical). Both cache-load-bearing files clear the gate without any production change.

---

## Validation Audit 2026-06-02

Retroactive `/gsd-validate-phase` — all automated SC commands re-run live (git-bash + WSL,
stack up). No stale `-run` names. Evidence:

- **SC#1u** `TestBuildPrefixStable` — 3 sub-tests PASS (byte-identity, monotonic growth, no-mutation).
- **SC#3** `TestCacheControlSeam` — 4 sub-tests PASS (anthropic marker / openrouter none / never history / no-op seam).
- **SC#4** `TestCacheMetrics_{WindowAndAggregate,StoreInsert}` (`db_integration`) — PASS against live Postgres (0.11s / 0.06s, genuine DB hits, not skip).
- **SC#1** `cache_invariant_audit.sh` — 22 identical `messages[0]` request hashes, exit 0.
- **SC#5** `TestCacheAudit_Mutation_Exit1` PASS + script carries explicit `messages[0] mutated at request N` wording (line 99) + `AURA_CACHE_AUDIT_BIN` negative-test seam; exit-code contract 0/1/2 all proven (`TestCacheAudit_*`).
- **SC#2** correctly manual-only (provider-side cache warming, PRD OQ4 — measured not gated).
- **Mutation gate** met: `builder.go` 1.000, `hash.go` 0.714 (both ≥0.70).

| Metric | Count |
|--------|-------|
| Success criteria audited | 6 (SC#1, SC#1u, SC#2, SC#3, SC#4, SC#5) |
| COVERED (automated, live-green) | 5 |
| Manual-only (provider-dependent) | 1 (SC#2) |
| Gaps found | 0 |
| Resolved | 0 |
| Escalated | 0 |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (incl. the SC#5 **negative** test — proven via `TestCacheAudit_Mutation_Exit1` + script wording)
- [x] No watch-mode flags
- [x] Feedback latency < 30s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** validated 2026-06-02 (Nyquist-compliant — 0 gaps; mutation gate met)
