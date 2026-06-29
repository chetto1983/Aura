# Phase 22: bug-fix - Context

**Gathered:** 2026-06-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Remediate every actionable finding in the 2026-06-15 `internal/agent`
production-readiness audit (1 P0 + 12 P1 + 26 P2 + 24 P3 = 63 findings,
`docs/audit/`) to the project Gate-3 done-bar — each fix lands with its named
regression test, the ≥85% owned-surface coverage floor holds, full CI is green
— moving the blended production-readiness score **6.5 → ≥8.0**.

This is a **remediation phase, not a feature**. No new product capability. The
core agent loop is verified-correct; the operational perimeter (crash safety,
secrets, observability, MCP/embed resilience, hook reliability, provenance,
budget/loop bounds, tool hardening) is what gets hardened. The three
multi-tenant-flavored security findings contribute only their **single-operator
slices** here; full multi-tenant gating is deferred.

</domain>

<spec_lock>
## Requirements (locked via SPEC.md)

**12 requirements are locked.** See `22-SPEC.md` for full requirements,
boundaries, and acceptance criteria.

Downstream agents MUST read `22-SPEC.md` before planning or implementing.
Requirements are not duplicated here.

**In scope (from SPEC.md):**
- All 63 actionable `internal/agent` findings (AG-001..AG-064) from
  `docs/audit/bug-report.md`, P0 through P3.
- The single-operator-relevant slices of the three multi-tenant-flavored
  security findings (hook minimal-env + fail-soft, DSN secret stripping, skill
  dead-schema/honest-docs/alert, `slog.Warn` on MCP `Mutating`-flip).
- Per-finding regression tests (`testing-strategy.md` §4) + the ≥85% coverage
  floor + green CI.
- A finding-coverage ledger recording the disposition of every `AG-###`.
- Confirmation of the four NEEDS-CONFIRMATION items, landing the
  `internal/agent`-side change where the fix lives there.

**Out of scope (from SPEC.md):**
- **AG-007 full `capability_grants` per-call wiring** — multi-tenant gate.
  *(Only the `slog.Warn`-on-flip slice lands here.)*
- **AG-003 hook exec-by-fd TOCTOU close** — already-compromised-host model.
  *(Only minimal-env + fail-soft land here.)*
- **AG-011 full multi-tenant skill-activation gating** — *(only the
  dead-schema/honest-docs/alert slice lands here.)*
- **Prior-cycle findings in other packages** — B-01 (`internal/runner`), B-03
  (`internal/agui`/`runner`), M-01 (`internal/conversations`).
- **Production container, `/readyz`, per-thread in-flight guard, multi-tenant
  re-rating, dependency breakers beyond MCP/embed** — OPS/deployment scope.
- **AG-034 ledger redaction** if it proves to live entirely in the persistence
  layer — confirm, then route (see D-09).

</spec_lock>

<decisions>
## Implementation Decisions

### Carried forward (from Phase 19, the analogous audit-bug phase)
- **D-00a — Fix everything, zero audit residue.** Every `AG-###` is closed
  (fixed+test) or explicitly documented as accepted/routed in the ledger.
  Consistent with [[user_finishes_what_starts]].
- **D-00b — Build minimal-real, no doc-downgrade.** Where the audit offered a
  "just fix the comment" cop-out, build the real machinery in its minimal
  industrial shape — not its over-engineered form
  ([[feedback_no_atomic_bombs_minimal_industrial_shape]]).

### Validation bar
- **D-01 — Full live sign-off pass (Phase 19/20 parity), on TOP of the
  automated per-finding gate.** The automated done-bar (named regression test
  per finding + `-race`/`goleak` + `make coverage` ≥85% + mutation spot-check +
  green CI + `cache_invariant_audit.sh`) is the **per-finding gate**. A full
  live real-agent E2E sign-off is the **phase-close gate** on top.
  Rationale: [[feedback_probe_must_verify_artifact_not_reply]] + the
  "tested-but-not-wired" failure class ([[project_phase13_10_live_signoff_resume]]).
- **D-02 — Live harness = full parity.** `aura serve` on the live stack
  (PG + Neo4j + MCP + embed sidecar + SearXNG via socat bridge) + host
  `aura chat` tool-trace ([[reference_live_tool_selection_trace]]) +
  `/metrics` scrape + the **CDP Telegram** round-trip
  ([[reference_cdp_telegram_live_test_harness.md]]).
- **D-03 — The live pass drives the FULL tool surface, incl. GLM OCR
  multimodal** — not just the observable-finding subset. This is also where the
  `AURA_FS_MAX_READ_BYTES` cap is empirically validated against real tool
  behavior (see D-05).
- **D-04 — Strict fail-before/pass-after for ALL findings.** For deterministic
  bugs the named test is shown red pre-fix, green post-fix. For concurrency
  findings (AG-002 dedup race; goroutine-leak findings) the "fail-before" form
  is **`go test -race` reporting the data race / `goleak` reporting the leak
  pre-fix, clean post-fix** — the race detector flags the unsynchronized access
  deterministically, so these are not artificial.
- **D-04a — Artifacts in `docs/audit/`** (parity with 19's `19-LIVE-SIGNOFF`):
  finding-coverage ledger at `docs/audit/22-finding-ledger.md`; live evidence
  at `docs/audit/22-LIVE-SIGNOFF-<date>.md`. Co-located with `bug-report.md`.

### Operational defaults
- **D-05 — `AURA_FS_MAX_READ_BYTES` = 10 MiB, stat-then-reject + paging hint**
  (error suggests `offset`/`limit`). OOM-safe on the shared 16-core mini-PC
  ([[feedback_minipc_cpu_budget]]); ample for code/config. **Exact value is
  E2E-validated/tunable** — confirmed against the full tool surface (incl. GLM
  OCR) during the live pass (D-03), not frozen blind. Per-tool (read/write/edit)
  caps follow the same shape.
- **D-06 — `AURA_MCP_CALL_TIMEOUT_SEC` semantic flip: `0`→"default",
  `-1`→"infinite".** Apply the flip (the old `0`=unbounded WAS the AG-006 hang
  bug); **boot-log the resolved per-server timeout**; one-line migration note in
  the close-out. The bounded-hang behavior (`0` server hangs → bounded by the
  default deadline, no leaked goroutine) is **proven in the E2E pass**. User
  owns the `.env` (single operator) so silent-surprise risk is near-zero.
- **D-07 — Reasoning-router stays ON by default; bound it, don't disable it.**
  Add the embed-sidecar circuit breaker + cap the router timeout ~2s; first call
  on an embed outage may fire, then the breaker opens (subsequent turns skip).
  **This REFINES SPEC R6's "opt-in / abstain→`ReasoningTierLow`" target
  wording** — it still satisfies R6's acceptance ("with the embedder forced to
  error, no router LLM call … or one then breaker-open, and turn latency is
  unaffected") and preserves the router's routing quality (a deliberate,
  self-improving feature — [[project_adaptive_reasoning_router_latency]]).
  **No SPEC amendment needed (acceptance unchanged); planner MUST NOT implement
  "default OFF".**
- **D-08 — MCP breaker/reconnect defaults:** breaker opens after **3**
  consecutive failures; **30s** cooldown; exponential backoff **500ms → 30s**
  cap; dedicated **10s** reconnect timeout; reconnect is **single-flight,
  off-lock** (`context.WithoutCancel`). All env-tunable (`AURA_<DOMAIN>_<UNIT>`),
  resolved+validated once at mount/boot.

### Cross-package boundary
- **D-09 — Stay within `internal/agent` + explicitly-named one-liners.**
  - AG-041: the `Budget.WithDeadline` threading lives in `internal/agent`; the
    one-line composition-root wiring at **`cmd/aura/agent.go` lands here** (R9's
    "total wall-time actively bounded" is inert without it).
  - AG-034 (reasoning-trace ledger redaction): **confirm-then-split** — land any
    `internal/agent`-side `event.go` shape change here; if the redaction is
    purely in the persistence/DB-projection layer, **route THAT part out** to a
    separate package phase with a ledger entry + rationale.
  - AG-028 (dead code) + AG-043 (parallel-closer leak edge): both land here
    (deadcode-clean; AG-043 proven NOT-a-leak — or fixed — by a `goleak` stress
    test).
  - Any genuinely other-package finding surfaced by investigation gets a
    **ledger entry (accepted/routed), not an in-phase fix.** B-01/B-03/M-01 stay
    out per SPEC.

### Execution strategy
- **D-10 — Full GSD `/gsd-execute-phase`** (wave-based `gsd-executor`
  subagents), end-to-end inside the GSD workflow — not the external-Codex hybrid
  this time. Rationale: the strict per-finding gate + finding-ledger discipline
  fits the GSD wave executor; [[feedback_follow_full_gsd_procedure]].
- **D-11 — One finding (or one tight finding-cluster) per atomic commit**, with
  the `AG-###` ref in the message. Best ledger traceability + per-fix rollback
  (matches SPEC commit discipline).
- **D-12 — Master-direct** — commit remediation directly on `master`; no feature
  branch/PR unless explicitly asked ([[feedback_master_direct_workflow]]).
- **D-13 — Automated-green per wave; live sign-off once at close.** Each
  risk-first wave (W1 crash → W2 secrets+obs → W3 MCP+reliability → W4 P2 → W5
  P3+ledger) must be `-race`/`goleak`/coverage/CI green before the next; the full
  live pass runs once at phase close (single live-stack session). `/gsd-plan-phase`
  owns final intra-wave composition.

### Claude's Discretion
- Exact `AURA_FS_MAX_READ_BYTES` number within the 10 MiB starting point (D-05),
  tuned during the E2E pass.
- Final intra-wave parallelization and the precise tight-cluster commit grouping
  (D-11/D-13), subject to `/gsd-plan-phase`.
- The exact MCP breaker env-var names (within the `AURA_<DOMAIN>_<UNIT>`
  convention).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase spec + audit (authoritative finding source)
- `.planning/phases/22-bug-fix/22-SPEC.md` — **locked requirements** (12); read first.
- `docs/audit/bug-report.md` — **authoritative finding list** AG-001..AG-064 (P0–P3) with file:line anchors.
- `docs/audit/action-plan.md` — engineering backlog A1..A21 (owner role + acceptance per task).
- `docs/audit/proposed-patches.md` — **PP-1..PP-10** design guidance (affected files/functions, before/after, approach, tests, rollback) for the major findings.
- `docs/audit/testing-strategy.md` §4 — the required per-finding regression tests (panic firewall, dedup race, MCP resilience, secret-boundary, tree-cycle, budget validation, router-fallback, cache-prefix drift).
- `docs/audit/audit-index.json` — machine-readable finding index (priority/file/status).
- `docs/audit/risk-register.md` — risk tiering behind the findings.

### Validation precedent (live sign-off parity)
- `docs/audit/19-LIVE-SIGNOFF-2026-06-10.md` — the Phase 19 live sign-off doc this phase's `22-LIVE-SIGNOFF-<date>.md` parallels.
- `.planning/phases/19-audit-bug-resolution-e2e-live-test/19-CONTEXT.md` — carried decisions D-00a/D-00b precedent.

### Project truth-source + invariants
- `prd.md` — project truth-source; KV-cache stable-prefix invariant, SSRF defense, nonce untrusted-output envelope, env catalog.
- `scripts/coverage_gate.sh` — the ≥85% owned-surface floor gate.
- `cache_invariant_audit.sh` — the `messages[0]` KV-cache stable-prefix CI gate (must stay green — no-regression).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`internal/db/tx.go:28`** — the ONLY existing `recover()` in the repo; its
  shape is the reference for the new `safeGo`-style panic-firewall idiom (D-01 /
  PP-1).
- **`internal/agent/metrics.go`** — already uses a promauto factory on a
  non-default registry; extend it with the new counters/histograms (PP-6) rather
  than introducing a new metrics substrate.
- **`internal/agent/tools/fs_read.go`** — already supports `offset`/`limit`
  paging; the fs-cap error (D-05) points the model at it instead of failing hard.
- **`internal/agent/trust.go`** — existing `untrustedSource` + nonce
  untrusted-output envelope; AG-052 inverts the default and reuses the envelope
  (PP-8), no new mechanism.
- **`internal/semindex` / reasoning classifier** — the validated embed-classifier
  replacement for the LLM router; D-07 bounds the (retained) router around it.

### Established Patterns
- **Mutation spot-check targets:** `llm_agent_parallel.go`, `budget_dedup.go`,
  `mcptools/bridge_reconnect.go` (≥70% killed — SPEC R12).
- **Env convention** `AURA_<DOMAIN>_<UNIT>`; third-party sidecar env keeps
  upstream naming.
- **No file >600 LOC; deep-refactor-on-touch** (dead-code removal + dupl-folding)
  in the same commit.
- **Non-regression invariants to preserve:** non-empty terminal contract,
  bounded recovery counters, KV-cache stable-prefix, shared-atomic budget, SSRF
  hardening, nonce untrusted-output envelope.

### Integration Points (affected files, from PP-1..PP-10)
- Panic firewall: `internal/agent/llm_agent_parallel.go` (`executeBatch`),
  `internal/agent/workflow/parallel.go` (`runSub`), `internal/swarm/swarm.go`
  (`runWave`), `internal/agent/tools/shell_bg.go` (reaper) + a Runner-level
  per-turn backstop.
- Dedup race + hardening: `internal/agent/budget_dedup.go`.
- MCP resilience: `internal/agent/mcptools/{bridge_reconnect.go,bridge.go,timeout.go}`.
- Secret boundary: `internal/secret/envkey.go`,
  `internal/agent/tools/shell_exec_env.go`; trace PII in `internal/reasoningtrace`.
- Observability: `internal/agent/{metrics.go,tracing.go,llm_agent.go}`.
- Hooks: `internal/agent/{hooks_command.go,hooks.go}`.
- Provenance: `internal/agent/trust.go`, `internal/swarm/runner_adapter.go`.
- Budget/loop: `internal/agent/budget.go` (`NewBudget`, `WithDeadline`) + the
  **`cmd/aura/agent.go`** composition-root wiring (D-09).
- Skill reconcile: `internal/agent/tools/{skill_write.go,skill.go}`.
- fs cap: `internal/agent/tools/{fs_read.go,fs_write.go,fs_edit.go}`.

</code_context>

<specifics>
## Specific Ideas

- "Test on E2E" (D-03/D-05/D-06): the user explicitly wants the fs-cap value and
  the MCP-timeout bound proven empirically during the live pass exercising GLM
  OCR + all tools, rather than frozen as guessed constants.
- "Keep router ON, just bound it" (D-07): the user values the reasoning router's
  routing quality and chose bounding over disabling — an informed refinement of
  SPEC R6's target wording.

</specifics>

<deferred>
## Deferred Ideas

- **Full multi-tenant security** (AG-007 `capability_grants` per-call wiring,
  AG-003 hook exec-by-fd TOCTOU, AG-011 full skill-activation gating) — a future
  security/hardening phase; only the single-operator slices land here.
- **Other-package prior-cycle findings** — B-01 (`internal/runner`), B-03
  (`internal/agui`/`runner`), M-01 (`internal/conversations`) — their own phases.
- **OPS/deployment** — production container (non-root, read-only rootfs),
  `/readyz`, per-thread in-flight guard, dependency breakers beyond MCP/embed.
- **AG-034 DB-projection redaction** — routed out if the redaction proves to live
  entirely in the persistence layer (D-09).

</deferred>

---

*Phase: 22-bug-fix*
*Context gathered: 2026-06-15*
