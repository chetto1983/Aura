# Phase 19: Audit Bug Resolution + E2E Live Test - Context

**Gathered:** 2026-06-10
**Status:** Ready for planning

<domain>
## Phase Boundary

Resolve the confirmed correctness/robustness findings from the **2026-06-10 deep
parallel audit** (`docs/audit/deep-correctness-audit-2026-06-10.md`) and prove the
fixes hold end-to-end on the live stack. **No new product capability** — this is a
correctness + validation phase. The audit already prescribes the mechanical `fix:`
for each finding; this phase decides the open judgment calls (scope, contract
posture, validation bar, test strategy) and executes.

**In scope:** every finding in the audit — 10 HIGH (H1–H10), 10 MEDIUM (M-a…M-j),
6 LOW (L1–L6), and the INFO triage. Each fix lands with a regression test that fails
before / passes after, plus a live real-agent E2E confirmation for every
user-observable finding.

**Out of scope:** new features, refactors beyond refactor-on-touch, and any audit
finding's "downgrade the docs instead of building it" option (explicitly rejected —
see D-02).
</domain>

<decisions>
## Implementation Decisions

### Scope / triage (Area: Finding scope)
- **D-01 — Fix EVERYTHING, zero audit residue.** All 10 HIGH + all 10 MEDIUM + all 6
  LOW are in scope. No tier is deferred. Rationale: user wants the audit fully cleared
  ("Everything — HIGH+MED+all LOW"), consistent with [[user_finishes_what_starts]] and
  [[feedback_aura_as_product]].
- **D-01a — INFO item = logged/accepted, no code change.** The INFO finding
  (self-installed skill *bundled scripts* not blocklist-scanned, `loader.go:213-220`)
  is confirmed-deliberate per the full-host-terminal trust model (PRD amendment #50 /
  D-15c, [[feedback_aura_full_host_terminal_primary]]). Resolution = document the trust
  boundary explicitly; do NOT add a scanner.
- **D-01b — Free-rider LOWs ride existing work.** M-i's central `godotenv.Load()` at
  `main()` auto-fixes the env-ordering LOWs (`aura mcp doctor` whatsapp-URL, `agent
  dry-run`/`swarm-demo` `AURA_LOOP_*`/`AURA_SWARM_MAX_DEPTH`). L3 (opaque cancel status)
  lands in the same `shell_exec.go` edit as H4/H5. Plan accordingly to avoid double-touch.

### Contract posture (Area: Contract build vs downgrade)
- **D-02 — Build minimal-real, all four contract findings. NO doc-downgrade.** User
  directive: "i want aura full functional no stupit quik win." For H6, H7, H10, M-g the
  audit offered a "just correct the comments" cop-out — that option is **rejected**.
  Build the real machinery. "Minimal" governs the *shape* (smallest thing that genuinely
  works, no supervisor/ping, no over-engineering — [[feedback_no_atomic_bombs_minimal_industrial_shape]]),
  not the *ambition* (it must actually function).
  - **H6** (`internal/cron/dispatch.go:154-156`): persist a notification-defer state
    (`notify_after` / equivalent) + a tick sweep that flushes deferred notifications at
    quiet-hours window end. The user must actually learn the job ran.
  - **H7** (`internal/cron/dispatch.go:158-160` + `notify.go:90`): persist
    undelivered-state for a failed MCP self-send + bounded re-attempt on a later tick.
    The "bound-retry on a later tick" (D-22) contract must become real.
  - **H10** (`internal/agent/mcptools/bridge.go:35-60` + `internal/mcp/client.go`): build
    the **lazy reconnecting Server wrapper** — re-open + `initialize` once on transport
    error, then retry; clean tool error on a second failure. This is the ALREADY-DECIDED
    design ([[reference_mcp_sidecar_lifecycle_and_openclaw_host]] — "fail-soft boot +
    reconnect-on-use, NO supervisor/ping"), so it was always build, not downgrade. Refresh
    the dead tool's boot-time description on reconnect.
  - **M-g** (`internal/cron/recover.go:55-82`): wire `ReschedulesOnRecovery` into
    `catchUpMissed` so the PRD recovery invariant ("never auto-re-execute committed
    side-effects when the flag is false") is actually consulted.

### Validation bar (Area: Live E2E validation)
- **D-03 — Two-layer proof: regression for ALL, live real-agent repro for user-visible.**
  - **Layer 1 (every finding):** a fails-before / passes-after regression test = the
    committed CI proof. Honors [[feedback_integration_tests_must_run_in_ci_not_skip]] and
    the no-skip-as-green gate.
  - **Layer 2 (user-observable findings only):** a live before/after repro driven by the
    **real paid agent with a real user prompt — no babysitting / no canned scripted
    inputs** (user directive: "we must debug with live agent (paid) with user prompt no
    baby sisttin"). Reproduce the bug live, fix, re-confirm live. Surfaces: `aura chat`
    host loop (shell never-answer H4/H5/M-b, SSE answer-truncation H9), Telegram CDP
    (H2 error render, H3 doc-conversion silence, M-e /cancel-during-pause), cron tick
    (H6/H7 scheduler notify), AG-UI SSE (H1 frame-drop with a deliberately-slow client).
  - **Non-observable findings → regression-only** (e.g. M-j unbounded stderr buffer, L5
    inert `SKIP LOCKED`, L4 search status filter, M-h shutdown-ctx terminal write).
  - **Live pass = required operator sign-off gate, NOT CI-automated.** The live stack is
    paid + manually driven (CDP harness, real OpenRouter calls). Mirror the prior
    Phase 13-10 / Phase 8 live sign-off pattern: record the live evidence in the phase's
    VALIDATION/sign-off doc; CI runs only Layer 1.

### Test strategy (Area: False-green tests)
- **D-04 — Rewrite the named false-green tests as broken + wire the orphan fixture.** A
  test that is green while asserting nothing is a *broken test* — CLAUDE.md's
  "never modify tests to make them pass" rule explicitly permits rewriting broken tests
  with justification in the commit message. Targets:
  - `TestFanoutSlowSubscriberDropped` — must re-validate the surviving frame sub-sequence
    via the AG-UI SDK `ValidateSequence`, not just `len <= want` + first/last lifecycle (H1).
  - `TestShellExecTimesOut` — must assert the child PID is actually dead, not just the
    timeout marker (H4).
  - `context_boundary_test.go` fixtures — must cover the `assistant(tool_calls) → tool →
    tool → assistant` round that the 2-stride drop corrupts, not only user/assistant
    bodies (H8).
  - Orphaned `testdata/premature_close.sse` — the H9 regression test must **consume** it
    (the anticipated-but-unwired mid-stream-failure case), not leave it referenced by zero
    `.go` files. Do NOT delete it.
  - Stale comment `serve_channels.go:145` ("the user sees a generic ❌ Errore") — correct
    or remove; that string renders nowhere (H2).

### Claude's Discretion (planner/researcher settle these — captured, not decided)
- **H9 streaming-contract approach:** add `Err error` to `llm.Chunk` and emit-before-close
  vs. treat a stream that ended with no `finish_reason` + non-nil parse error as a
  retryable infra failure. Both are real fixes; pick during planning (the `Err`-field
  option also cleanly covers the `sse.go:114-122` clean-EOF-no-finish_reason case).
- **Wave sequencing:** the audit names **H4 + H5 + M-b** as the single highest-leverage
  bundle (the "shell never answers" root cause). Strong candidate for wave 1 / earliest
  ship.
- **Commit granularity:** one commit per finding, or per tight cluster (e.g. H4+H5+M-b
  together, M-i + its free-rider env LOWs together) — atomic per CLAUDE.md / one-slice-one-commit.
- **M-a / M-b stream-retry consolidation:** route completion-critic, finalize, and
  reasoning-router stream-opens through the shared `streamWithOpenRetry` helper.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The audit (primary source — drives all requirements)
- `docs/audit/deep-correctness-audit-2026-06-10.md` — the full finding list with exact
  `file:line`, class, mechanism, and prescribed `fix:` for every H/M/L/INFO item. THIS is
  the requirement source for the phase. The "Confirmed RESOLVED / refuted" section is the
  negative-result boundary (e.g. `send_file` IS wired, swarm max-depth IS correct — do not
  re-touch).
- `.planning/ROADMAP.md` §"Phase 19: Audit Bug Resolution + E2E Live Test" — phase goal +
  priority order (cluster → error-swallowing → not-wired → env-ordering).

### Decided designs the fixes must honor
- `docs/research/mcp-sidecar-lifecycle-study.md` §43 — "lazy reconnect-on-use" decided
  design for H10 (only the fail-soft-boot half ships today).
- `prd.md` — D-22 (scheduler bound-retry contract, H7), the recovery invariant (M-g),
  amendment #50 / D-15c (full-host-terminal trust model, INFO triage).

### Live validation harness (Layer 2 / D-03)
- `D:/tmp/tg_cdp.py` + [[reference_cdp_telegram_live_test_harness]] — drive
  web.telegram.org via Chrome `--remote-debugging-port=9222` + Playwright `connect_over_cdp`;
  DB is ground truth. For H2/H3/M-e Telegram-surface repros.
- [[reference_live_tool_selection_trace]] / [[reference_run_aura_binary_live_env_loading]]
  — `aura chat` host-loop tool-trace + `.env` loading gotchas for the shell/SSE repros.
- [[reference_e2e_full_matrix_invocation]] — full live tier invocation + DSN/env gotchas.

### External
- AG-UI SDK `ValidateSequence` (public) — the conformance check H1 must satisfy; reused in
  the rewritten `TestFanoutSlowSubscriberDropped`.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`sanitizeErr` (HTTP path)** — reuse for H2's new `RunErrorEvent` case in the Telegram
  renderer and M-c's Fanout sanitization (one sanitization chokepoint at the translator
  boundary).
- **`redactEvent` (HTTP path)** — the existing redaction H1/M-c's Fanout path bypasses;
  apply it on the in-process Fanout path too.
- **`Ping` MCP primitive** — exists, wired only to `aura mcp doctor`; H10's reconnect
  wrapper can reuse the transport-health signal.
- **`context.WithoutCancel` + short-deadline pattern at `serve.go:119`** — the model for
  M-h's terminal-state write on a signal-cancelled root ctx.
- **`pg_try_advisory_lock` per-task claim** — already holds scheduler correctness; L5's
  `SKIP LOCKED` is inert defense-in-depth on the autocommit pool (drop or fold into one tx).
- **Telegram CDP harness + `aura chat` tool-trace** — the live rig already exists
  (Phase 13 sign-off); no greenfield validation infra.

### Established Patterns
- **Host-primary `shell_exec` posture** (Phase 18, D-01, [[feedback_aura_full_host_terminal_primary]])
  — `shell_exec` is THE primary execution surface; that's why H4 (orphan-child hang) and
  H5 (head-first truncation) are the highest-leverage findings.
- **pgx lazy-error discipline** (`rows.Err()` after loops, SQLSTATE re-classification) is
  already applied — keep it on the new scheduler-persistence code (H6/H7).
- **No package-level / `init()` env reads** in `internal/` — env is read after
  `config.Load*` / godotenv. M-i's single `_ = godotenv.Load()` at `main()` closes the
  operator-subcommand gap without violating this.
- **Deferred-tool BM25 registry** — every tool is registered + indexed; do not regress this
  while touching H10's bridge (the dead tool keeps its boot-time description today — refresh
  it on reconnect).

### Integration Points
- `internal/agent/tools/shell_exec.go` + `result.go` — H4 (process group + `cmd.Cancel` +
  `WaitDelay`), H5 (reserve tail bytes for status+footer through `NewResult` truncation),
  L3 (ctx-cancel status branch), M-f (single synchronized stdout/stderr writer).
- `internal/cron/{dispatch,notify,recover,store_runs}.go` — H6, H7, M-g, M-h, L5 (the whole
  scheduler-contract cluster; H6/H7 need a new persisted notification state — likely a new
  migration / column).
- `internal/agui/{server,fanout}.go` — H1 (lifecycle-frame classification), M-c (sanitize
  Fanout `RUN_ERROR`), M-d (`lastUserMessage` multimodal rejection).
- `internal/channels/telegram/{renderer,bot_dispatch,commands}.go` — H2, H3, M-e.
- `internal/llm/openai_compat/{client,sse}.go` — H9 (stream-error reportability), M-a.
- `internal/agent/llm_agent{,_completion,_finalize,_reasoning}.go` — M-a, M-b (never-answer
  contributors).
- `internal/mcp/managed_config.go` + `cmd/aura/main.go` — M-i central `godotenv.Load()`.
- `internal/knowledge/client.go` — M-j (bounded stderr ring buffer).
- `cmd/aura/skills_snippet.go` (L1), `internal/skills/{writer_activate,resume}.go` (L2),
  `internal/conversations/store.go` (L4), `internal/agent/tools/{web_search,web_fetch}.go` (L6).

**Likely new migration:** H6/H7 durable notification state probably needs the next
sequential Postgres migration (current head is 0011 `tool_invocations`). Planner confirms.

</code_context>

<specifics>
## Specific Ideas

- "i want aura full functional no stupit quik win" — the governing directive for D-02:
  every contract finding gets real, working machinery, not honest-but-unimplemented comments.
- "we must debug with live agent (paid) with user prompt no baby sisttin" — Layer-2
  validation is real-agent + real-user-prompt repro, not scripted fixtures; the operator
  drives it and signs off.
- "Everything — HIGH+MED+all LOW" — total scope, zero residue.

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope. The phase deliberately absorbs the entire
audit (D-01), so there is no audit-finding backlog to carry forward. Any net-new capability
that surfaces during planning (vs. a fix to existing behavior) belongs in its own phase.

</deferred>

---

*Phase: 19-audit-bug-resolution-e2e-live-test*
*Context gathered: 2026-06-10*
