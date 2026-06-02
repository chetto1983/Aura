---
phase: 6
slug: kv-cache-builder
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-02
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
| SC#1 | SHA-256(messages[0]) constant across 20-turn replay, printed to stdout | 0 | CAP-04 | T-06-01 (prefix poisoning) | invariant hash on real `runner.Turn` loop | smoke (runtime-faithful) | `bash scripts/cache_invariant_audit.sh` | ❌ W0 | ⬜ pending |
| SC#1u | `PromptBuilder.Build` byte-identical [0] over N turns + monotonic history growth + no in-place mutation | 0 | CAP-04 | T-06-01 | — | unit | `go test ./internal/agent/prompt/ -run TestBuildPrefixStable` | ❌ W0 | ⬜ pending |
| SC#2 | `usage.prompt_cache_hit_tokens` (CachedTokens) rises from turn 2 (provider-side, NOT CI-gated) | — | CAP-04 | — | N/A | manual-only (live DeepSeek) | manual `aura chat send` ×3 + `aura cache-stats` | ❌ W0 | ⬜ pending |
| SC#3 | Anthropic-provider build carries `cache_control` on system+tools, NOT history; OpenRouter build carries none | 0 | CAP-04 | T-06-03 (secret leak via wire) | dormant no-op seam under OpenRouter | unit (wire-shape) | `go test ./internal/agent/prompt/ -run TestCacheControlSeam` | ❌ W0 | ⬜ pending |
| SC#4 | `aura cache-stats --since=1h` returns the window; hit-rate ≥80% is *measured* not gated | 0 | CAP-04 | T-06-02 (SQLi via --since) | `time.ParseDuration` + sqlc parameterized | integration (query) + manual (the 80% number) | `go test -tags db_integration ./internal/db/... -run TestCacheMetrics` | ❌ W0 | ⬜ pending |
| SC#5 | CI fails with explicit "messages[0] mutated at <site>" on a poisoning PR | 0 | CAP-04 | T-06-01 | negative-path proof the gate is not silently green | smoke (negative) | `scripts/cache_invariant_audit.sh` exits 1 + explicit message when a fixture mutates [0] | ❌ W0 | ⬜ pending |

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
| Mutation spot-check (CLAUDE.md gate) | CAP-04 | `go-mutesting` runs on WSL, not CI | `go-mutesting` on `internal/agent/prompt/builder.go` + `hash.go` — ≥70% killed (cache-load-bearing files) |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (incl. the SC#5 **negative** test — without it SC#5 is unproven)
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
