# Aura Codebase Quality Audit (Maintainability / Correctness / Architecture)

> **Historical source register.** The June finding text is preserved for
> traceability. Current `QA-*` disposition is authoritative only in
> [../definitive-closure-ledger-2026-07-31.md](../definitive-closure-ledger-2026-07-31.md).

**Date:** 2026-06-29
**Method:** 4 parallel read-only slice auditors (agent-core / persistence / transport-web / frontend-ops), each covering 6 dimensions (duplication, dead code, not-wired, antipatterns, architecture, test gaps) with Aura-specific false-positive controls (deferred-tool pattern, sqlc-generated code, `//go:embed`, build tags, MCP subprocess wiring, i18n dynamic keys). Synthesis cross-references the security/production audit (`../`, findings F-001..F-052) without re-reporting it.
**Lens:** maintainability/architecture — distinct from and complementary to the 2026-06-21 production-readiness/security audit in `docs/audit/`.

Per-slice detail: [slice-A-agent-core.md](slice-A-agent-core.md) · [slice-B-persistence.md](slice-B-persistence.md) · [slice-C-transport-web.md](slice-C-transport-web.md) · [slice-D-frontend-ops.md](slice-D-frontend-ops.md)

---

## A. Executive Summary

**Overall health: GOOD / production-shaped.** ~64 findings, **0 Critical, ~8 High, ~26 Medium, ~26 Low**. The architecture is sound: the PRD-mandated `agent ⇸ agui` import boundary holds; the no-god-class discipline is well applied (only **one** production file over the 600-LOC cap — `cmd/aura/serve_webui.go` at 628, already mid-refactor); the store/learn package "proliferation" is **intentional and correct** (the unified `semindex` substrate goal is realized); test discipline (goleak, no-skip-as-green, tagged tiers) is consistently applied; frontend quality gates (Vitest ≥85%, Stryker ≥70%, dist-freshness, Playwright e2e) are genuinely CI-enforced.

The debt is **not architectural** — it is **the same small pattern repeated**: helper functions copy-pasted across packages instead of extracted, and `AURA_*` env knobs read ad-hoc in hot paths instead of catalogued in config. These are individually low-risk but collectively a drift hazard (a fix in one copy silently doesn't reach the others). There is no rewrite here — the cleanup is a sequence of small, mostly mechanical, reversible changes.

### Top 5 risks (ranked by impact)

1. **[Operational — imminent CI breakage]** Two files exceed the 600-LOC cap (`cmd/aura/serve_webui.go` 628, `web/src/__tests__/LoginPage.test.tsx` 643) → the `file-size` pre-commit hook **blocks every commit**; and `internal/webui/dist` has ~50+ deleted asset files **not rebuilt** → the `web-dist-freshness` CI job **will fail on the next push**. (Slice A QA-A-04, Slice D QA-D-03 / XS-03.)
2. **[Duplication → behavior drift]** ~10 copy-pasted helpers across packages: store helpers (`hashText`/`asString`/`asFloats` ×3, `GraphClient` ×2, `numericFromFloat` ×2 — Slice B), env helpers (×3 — Slice C+A), agent canonical-args & transient-error classifiers (Slice A), `chat_render`↔`eval` (~80 LOC — Slice C), web `getJSON` (×3) & focus-trap (Slice D). A bug fixed in one copy silently doesn't reach the others.
3. **[Quality dup that amplifies security findings]** Two duplications are the quality face of open audit findings: MCP trust-normalization (×2 → **F-027**) and the 4 `decode*Body` helpers (→ **F-052** strict-decode). Unifying them *during* the v2.0.0 security phases kills quality + security debt together.
4. **[Uncatalogued env knobs in hot paths]** `AURA_LOOP_MAX_PARALLEL_TOOLS`, `AURA_FS_*`, `AURA_SHELL_*` are read via `os.Getenv` at call time, undiscoverable from the config module (relates **F-016**) — invisible ops tuning + minor per-call cost.
5. **[Targeted correctness / test gaps]** `askuser/store.go:231` int32 cast without overflow guard (CodeQL `go/incorrect-integer-conversion` candidate — Slice B QA-B-08); `bootChatEnvWithConfig` double-`Validate` + a possible pool leak on the overlay-failure path (Slice A QA-A-03); untested concurrency/ordering primitives (`web/throttle.go`, setup `InvalidateToken`-before-SSE, Telegram keyword fallback, Authula DSN parsing).

### Estimated cleanup opportunity
- **~12 zero-/low-risk deletions & stdlib swaps** (XS/S, High confidence) remove dead exports, placeholders, and reinvented stdlib.
- **~6 shared-helper extractions** (S/M) collapse the duplication theme into `internal/neostore`, `internal/envutil`, `internal/agentrender`, agent shared primitives, and web shared utils.
- Net: a meaningful maintainability lift achievable in small commits, **no rewrite**, with the heavier items naturally absorbed by v2.0.0's refactor-on-touch discipline.

---

## B. Findings — by slice (full tables in the slice files)

| Slice | Crit | High | Med | Low | Headline findings |
|-------|----:|----:|---:|---:|-------------------|
| A — Agent core/tools/runner | 0 | 3 | 5 | 4 | `canonArgs`/`canonicalArgs` dup (QA-A-01); transient-error classifier divergence (QA-A-02); double-`Validate`/pool-leak (QA-A-03); `serve_webui.go` 628 LOC (QA-A-04) |
| B — Persistence/learn/store | 0 | 3 | 5 | 8 | store-helper + `GraphClient` dup → `internal/neostore` (QA-B-01/02/03); `askuser` int32 overflow (QA-B-08); learn/store split is correct |
| C — Transport/web/MCP/channels | 0 | 0 | 8 | 9 | 4× `decode*Body` (QA-C-01→F-052); env-helper ×3 (QA-C-02); trust-norm ×2 (QA-C-03→F-027); dead settings/assets consts (QA-C-06/09) |
| D — Frontend/build/CI/ops | 0 | 2 | 8 | 5 | `getJSON` ×3 (QA-D-01); hand-rolled focus-trap vs shared (QA-D-02); `LoginPage.test.tsx` 643 LOC (QA-D-03); CI raw `./...` (QA-D-07→F-015); dist not rebuilt (XS-03) |

**Recurring cross-slice themes** (the real signal):
- **T1 — Copy-pasted helpers** → extract shared packages (the single highest-ROI cleanup).
- **T2 — Uncatalogued `AURA_*` knobs read in hot paths** → centralize in config (ties to Phase 31 profiles + F-016).
- **T3 — Dead exports / deferred placeholders** → delete or annotate (`assets.Status*`, `settings AURA_MEMORY_EMBED_*`, `qrSVG`, `indexByte`, `stringList`, `telebot` blank import, `AgentTier`).
- **T4 — Quality dup overlapping security findings** → unify inside the matching v2.0.0 phase (F-027→36, F-052→38).
- **T5 — Targeted test gaps on concurrency/ordering/parsing** → add focused unit tests.

---

## C. High-confidence quick wins (safe, minimal risk)

Pure deletions / stdlib swaps (XS–S, High confidence, do first):
- Replace `agui/governance_api.go:226` `indexByte` with `strings.Cut`; inline the `stringList` no-op (QA-C-10).
- Remove `channels/deps.go` redundant `_ "gopkg.in/telebot.v4"` blank import (verify `go mod tidy`) (QA-C-12).
- Delete dead `assets.StatusCreated/StatusEmbedding/StatusCanceled` (QA-C-09) after confirming no JSON-string consumer.
- Annotate or remove sidecar-only `settings AURA_MEMORY_EMBED_*` keys (QA-C-06).
- Fold the two `truncateRunes` copies (QA-C-13); remove the redundant `RequestID` re-stamp in `cmd/aura/agent.go:127` (QA-A-12).
- Collapse the discarded `Build()` call in `llm_agent.go:235` when `adaptiveTierOK` (QA-A-11) — one-liner, saves a `RenderToolDefs()` per turn.

Test-only additions (S, zero regression):
- `web/throttle.go` unit test (QA-C-07); setup `InvalidateToken`-before-SSE ordering test (QA-C-08); `truncateTailBytes`/`truncateBytesKeepingTail` test (QA-A-06).

---

## D. Risky / uncertain — verify before acting

- **QA-A-03 (pool leak):** the `bootChatEnvWithConfig` two-`loadConfig`/two-`Validate` flow needs an integration test on the *settings-overlay-then-Validate-fails* path before restructuring. Evidence missing: a failing-overlay-after-pool-open repro.
- **QA-B-08 (int32 overflow):** confirm `limit` cannot exceed int32 from any caller before deciding guard vs. type change (it's a CodeQL candidate, low real exploitability).
- **QA-A-07 (`AgentTier` dead field):** verify no caller in `internal/cron/handlers/` (out of slice A) sets `TaskArgs{AgentTier:...}` before treating it as dead.
- **QA-C-03 (trust-normalization unify):** security-relevant — do NOT merge casually; fold into Phase 36 with full trust tests (every call site's inference must match first).
- **QA-C-06 (dead settings keys):** confirm the agent-memory sidecar reads these from compose env independently of the Go overlay before removing.

---

## E. Refactoring plan (incremental, low-risk first — no rewrite)

**Wave 0 — Unblock (operational, do first):**
1. Split the two over-cap files (`cmd/aura/serve_webui.go` → `serve_webui_auth.go`/`serve_webui_routes.go`; `web/src/__tests__/LoginPage.test.tsx` → focused test files) to unblock the `file-size` hook.
2. Rebuild + commit `internal/webui/dist` to unblock `web-dist-freshness`.

**Wave 1 — Pure deletions / stdlib swaps** (Quick Wins §C) — zero/low regression, no behavior change.

**Wave 2 — Shared-helper extraction** (the duplication theme, S–M each, Low regression — add a parity test per extraction):
- `internal/neostore` ← `hashText`/`asString`/`asFloats`/`GraphClient`/`numericFromFloat` (Slice B QA-B-01/02/03/04).
- `internal/envutil` ← the 3 env-helper copies (Slice C QA-C-02) + adopt for the agent-tool knobs (Slice A QA-A-05/08).
- `internal/agentrender` ← the `chat_render`↔`eval` ~80-LOC set (Slice C QA-C-04).
- agent shared primitives: one `CanonicalArgs` (QA-A-01) + one `isTransientNetworkErr` (QA-A-02).
- web: single `getJSON` import (QA-D-01) + reuse shared `focusTrap.ts` (QA-D-02); pick one skeleton system (QA-D-08).

**Wave 3 — Correctness** (M, Medium regression — gate on integration tests):
- `askuser` int32 guard (QA-B-08); `bootChatEnvWithConfig` single-`Validate` + deferred pool close (QA-A-03); catalogue `AURA_*` hot-path knobs in config (QA-A-08, ties Phase 31 + F-016).

**Wave 4 — Test gaps** (S): throttle, setup ordering, keyword fallback, `truncateTail`, Authula DSN; decide the `memory_integration` CI matrix leg (QA-A-09).

**Security-overlapping items — route through the matching v2.0.0 phase, not here:**
- Trust-normalization unify → **Phase 36** (MCPH-01 / F-027). · `decode*Body` strict-decode → **Phase 38** (SEC-06 / F-052). · Authula DSN tests → **Phase 34** (MUSR-06). · CI raw `./...` → **Phase 38** (SEC-07 / F-015).

> **Preserve public APIs** unless a finding has explicit unused evidence. Prefer small reversible commits; each touched file clears its own quality findings (refactor-on-touch).

---

## F. Suggested validation (before/after each cleanup)

- **Build:** `make build` (or `go build $(bash scripts/go_packages.sh)`).
- **Tests:** `go test -race $(bash scripts/go_packages.sh)`; tagged tiers (`-tags 'db_integration neo4j_integration'`) for persistence/MCP changes; `cd web && npm test` + Stryker for frontend changes.
- **Static:** `golangci-lint run` (catches dead code/dup the audit may have missed); `govulncheck`; `make coverage` (owned-surface ≥85%); `cd web && npm run lint` (ESLint max-warnings=0 + `jscpd` + `knip`).
- **Dead-code confirmation:** run `deadcode ./...` and `knip` to corroborate every Dead-Code finding before deletion — do NOT delete on a single audit's say-so.
- **Manual checks:** for QA-A-03 (pool path) and QA-C-03 (trust) run the relevant integration tests; for any cross-package "dead" symbol, a repo-wide `rg` (including `_test.go` and build-tagged files) before removal.

---

## Relationship to v2.0.0

This audit is a v2.0.0 milestone artifact. Most cleanup rides **refactor-on-touch** inside the existing roadmap phases; the security-overlapping items are explicitly routed to Phases 34/36/38 above. The pure-quality waves (0–2) are tracked here as **`QUAL-*`** and folded into the milestone (see `.planning/REQUIREMENTS.md`). No new standalone phase is required — Wave 0 is urgent (unblocks CI), Waves 1–2 are opportunistic quick wins, Waves 3–4 attach to the phases that already touch those files.
