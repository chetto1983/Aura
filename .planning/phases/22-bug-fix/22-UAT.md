# Phase 22 — Agent Perimeter Hardening: UAT

> **Phase:** 22-bug-fix · **Date:** 2026-06-15 · **Evidence HEAD:** `036575b5`
> **Operator:** Davide (dvdmarchetto@gmail.com)
>
> User-acceptance framing for the AG-001..064 hardening phase. The runtime behaviour
> is verified two ways: **(A) automated** (done — see Part A) and **(B) operator live
> sign-off** (pending — the runbook is below and in
> [`docs/audit/22-LIVE-SIGNOFF-2026-06-15.md`](../../../docs/audit/22-LIVE-SIGNOFF-2026-06-15.md)).
> Nothing here is a fabricated pass.

---

## What the operator asked for (the phase goal)

> "Harden the agent perimeter before exposing it to the web cockpit: a panicking
> tool must not crash the daemon, MCP/hook/secret/observability gaps must close, the
> skill self-extension story must be honest, and every audit finding must be either
> fixed-with-a-test, accepted-with-a-reason, or confirmed-and-routed — nothing
> silently dropped."

Mapped to HARDEN-01..12 in `.planning/REQUIREMENTS.md`; every AG-001..064 disposed in
[`docs/audit/22-finding-ledger.md`](../../../docs/audit/22-finding-ledger.md).

---

## Acceptance criteria

| # | Acceptance criterion | Requirement | How verified | Status |
|---|----------------------|-------------|--------------|--------|
| AC-1 | A panicking tool / swarm child / `shell_bg` reaper cannot crash `aura serve`; it surfaces as a model-visible per-call error | HARDEN-01 | A: `TestExecuteBatch*Panic`/`TestSwarm*Panic`/etc. (race A4). B: live host-chat tool trace. | A ✅ / B ⏳ |
| AC-2 | Dedup ring is concurrency-safe; race-clean under parallel dispatch | HARDEN-02 | A: `-race` dedup tests (A4). | A ✅ |
| AC-3 | A flapping/hung MCP server degrades gracefully (single-flight reconnect, backoff, breaker, sane `=0`/`-1` timeout) | HARDEN-03 | A: `bridge_reconnect_branches_test.go` (A4). B: flapping-server live probe. | A ✅ / B ⏳ |
| AC-4 | Credentials do not leak to shell children / hook subprocesses / the reasoning trace by default | HARDEN-04 | A: `envkey_test.go`, `shell_exec_test.go`, `reasoningtrace_test.go` (A3). B: live `cat $AURA_DB_URL` in a shell child. | A ✅ / B ⏳ |
| AC-5 | Production is observable (turn/LLM/error/token/hook metrics + `slog`); telemetry cannot crash the daemon | HARDEN-05 | A: `metrics_observability_test.go`, `TestMintSpanID` (A3). B: `/metrics` scrape. | A ✅ / B ⏳ |
| AC-6 | An embed-sidecar outage adds no per-turn latency cliff | HARDEN-06 | A: `llm_agent_reasoning_test.go` (A3). B: live router with sidecar down. | A ✅ / B ⏳ |
| AC-7 | A hook fault is contained, not turn-fatal | HARDEN-07 | A: `hooks_policy_test.go` (A3). B: live hook fault. | A ✅ / B ⏳ |
| AC-8 | Unknown-tool + swarm-child output is default-untrusted, cannot launder injection | HARDEN-08 | A: `trust_default_test.go` (A4). B: live swarm output rendering. | A ✅ / B ⏳ |
| AC-9 | Loop / budget / workflow are bounded and validated | HARDEN-09 | A: `loop_bounds_test.go`, `budget_test.go`, `workflow_edges_test.go` (A4). | A ✅ |
| AC-10 | Tool execution is memory-safe, evictable, consistent (fs cap, cycle guard, dedup bound) | HARDEN-10 | A: `fs_cap_test.go`, `tool_hardening_test.go` (A4). B: GLM-OCR fs-cap live pass. | A ✅ / B ⏳ |
| AC-11 | **Skill self-extension docs match behaviour; dead schema removed** | HARDEN-11 | A: `TestSkillSchemaIsHonestNotDishonest`, `TestSkillToolSchemaStatesActualAutoActivationPolicy`, `TestActionCreateActivates`, single-schema grep (A3/A4). B: live skill create + operator alert. | A ✅ / B ⏳ |
| AC-12 | **Every in-scope finding closed with its named test; ≥85% owned-surface coverage; nothing dropped** | HARDEN-12 | A: ledger 64/64 + cache gate (A5). B: `make coverage` ≥85%. | A ✅ (ledger) / B ⏳ (coverage) |

---

## Part A result — AUTOMATED (DONE)

`go build`, `go vet`, `go test ./...` (untagged), `go test -race
./internal/agent/... ./internal/swarm/...`, and `scripts/cache_invariant_audit.sh`
all pass at HEAD `036575b5` — **except** the single pre-existing, out-of-scope
`cmd/aura` compose-drift test (`TestProductionContainerArtifactsMatchFatImageContract`,
`:nitro`/`:exacto`), which is logged in `deferred-items.md` and untouched by any
plan-05 file. Exact commands + output: `docs/audit/22-LIVE-SIGNOFF-2026-06-15.md`
Part A.

The cache-invariant gate is the load-bearing UAT signal for AC-11: the 22-05 skill
schema edit (which feeds the skill tool Description in `messages[0]`) did **not** bust
the KV prefix cache — 22 identical `messages[0]` hashes.

## Part B result — OPERATOR LIVE SIGN-OFF (PENDING)

The operator runs, on a quiet machine with the live stack up:

1. **B1 coverage** — `make coverage` (destructive PG wipe), confirm ≥85% owned-surface.
2. **B2 quality** — `golangci-lint run ./...` (0), `govulncheck ./...` (0), mutation
   ≥70% on the three critical files.
3. **B3 live** — `aura serve` + the acceptance matrix (AC-1/3/4/5/6/7/8/10/11 live
   columns): host `aura chat` tool trace, `/metrics` scrape, CDP Telegram round-trip,
   GLM-OCR multimodal fs-cap, flapping-MCP reconnect, reasoning fallback, skill
   self-extension + operator alert, DSN secret boundary, tool-invocation ledger
   redaction.

For each live check the operator records a ground-truth assertion that does NOT read
`r.Reply` (DB row / `· <toolname>` trace / `/metrics` line / rendered body) + a visual
body print, then flips the matching `B ⏳` cells to `B ✅` with date + sign.

---

## Sign-off

| Gate | Owner | Status |
|------|-------|--------|
| Part A — automated floor | executor (22-05) | ✅ done @ `036575b5` |
| Part B — coverage / quality / live | operator | ⏳ pending |

Phase 22 is **automated-green**; Gate-3 close awaits the operator Part-B sign-off.
