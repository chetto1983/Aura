# Phase 32: Quality Cleanup — Dead Code + Shared Helpers - Context

**Gathered:** 2026-06-29
**Status:** Ready for planning

<domain>
## Phase Boundary

Maintainability cleanup of the v2.0.0 codebase **before** feature phases 33+ build on these
surfaces, driven by the 2026-06-29 quality audit (`docs/audit/quality/`). Three requirement groups:

- **QUAL-02 — dead code / placeholders.** Triage + remove (or wire) the audit-named items:
  `assets.Status{Created,Embedding,Canceled}`, sidecar-only `settings AURA_MEMORY_EMBED_*` keys,
  `agui.indexByte`/`stringList`, redundant `channels/deps.go` telebot blank import, redundant
  `RequestID` re-stamp (`cmd/aura/agent.go:127`), `truncateRunes` dup fold, discarded `Build()` in
  `llm_agent.go:235`.
- **QUAL-03 — shared-helper extraction** to kill cross-package duplication: `internal/neostore`,
  `internal/envutil`, `internal/agentrender`, agent `CanonicalArgs` + `isTransientNetworkErr`
  primitives, web single `getJSON` + shared `focusTrap` — **plus** QA-D-08 frontend skeleton-system
  unification (folded in this phase, see D-08).
- **QUAL-05 — targeted test gaps:** `web/throttle.go`, setup `InvalidateToken`-before-SSE ordering,
  Telegram `answersFromText` keyword fallback, `truncateTailBytes`, Authula `ensureAuthulaSearchPath`
  DSN parsing, plus the `memory_integration` CI matrix leg (QA-A-09, see D-12).

All changes are small, reversible, behavior-preserving — **no rewrite**.

**Explicitly OUT of scope** (routed elsewhere by the roadmap — do NOT pull forward):
- QUAL-04 correctness (`askuser/store.go:231` int32 guard QA-B-08; `bootChatEnvWithConfig`
  double-`Validate`/pool-leak QA-A-03; `AURA_*` hot-path env catalogue) → **Phases 33/34**.
- MCP trust-normalization unify (QA-C-03 / F-027) → **Phase 38**.
- `decode*Body` strict-decode unify (QA-C-01 / F-052) → **Phases 38/40**.

</domain>

<decisions>
## Implementation Decisions

### Scope & dead-code triage
- **D-01 — Triage, not blind deletion.** Every flagged "dead" symbol is triaged first:
  *genuinely dead* → delete; *intended-but-unwired* → the bug is the **missing wiring**, not the code.
  A deadcode/knip flag is a question, not a verdict.
- **D-02 — Wire intended-but-unwired symbols here.** When triage finds a symbol is not dead, just
  never connected to its intended consumer, **wire it in this phase** (close the latent bug rather
  than defer). **Guardrail:** wiring is bounded to connecting the *already-existing* flagged symbol
  to its intended consumer. If a single wiring turns non-trivial (new feature behavior, a migration,
  a cross-phase surface) → **escalate to the operator**, do not expand scope. Likely bite:
  `assets.Status*` (status transitions never emitted) and possibly the `AURA_MEMORY_EMBED_*` keys.
- **D-03 — Reach = audit's named items + refactor-on-touch; no repo-wide sweep.** Operate on the
  audit's enumerated items; within any file already touched, also fold dead code / dup that
  `deadcode`/`knip`/`golangci-lint` flag (standing CLAUDE.md "deep refactor on touch"). Do **not**
  run a whole-tree removal sweep beyond the audit list — that risks bleeding into feature surfaces
  phases 33+ will rewrite. *(Operator reframed the scope question toward per-item dead-vs-unwired
  triage rather than picking a reach band; refactor-on-touch is the CLAUDE.md default.)*

### Dead exported symbols
- **D-04 — Case-by-case on exports.** Unexported confirmed-dead symbols are deleted freely. Each
  **exported** confirmed-dead symbol is surfaced to the operator for a keep/kill call before removal.
  "Confirmed" = `deadcode` **and** `knip` **and** a repo-wide `rg` (including `_test.go` and
  build-tagged files) all agree. Never delete on a single audit's say-so.

### `AURA_MEMORY_EMBED_*` (dead settings keys, QA-C-06)
- **D-05 — Remove-from-Go + document when sidecar-owned.** If triage confirms the keys are genuinely
  sidecar-owned (read from compose env, nothing in-process reads them), **remove them from the Go
  settings struct** and document in `.env.example`/a comment that the agent-memory sidecar owns them
  (no phantom in-process knobs). If instead they are a Go→sidecar wiring gap, wire per D-02.

### Shared-package extraction
- **D-06 — Extraction depth decided per-package by Claude** from what the code shows. Expected calls
  (planner may adjust): `neostore` likely **canonical** (funnel Neo4j store helper access through it);
  `envutil` and `agentrender` likely **minimal** (extract the duplicated helper + migrate only the
  call sites that had copies, don't force unrelated access through them).
- **D-07 — Target packages/primitives:** `internal/neostore` ← `hashText`/`asString`/`asFloats`/
  `GraphClient`/`numericFromFloat` (Slice B QA-B-01/02/03/04); `internal/envutil` ← the 3 env-helper
  copies (QA-C-02) + adopt for agent-tool knobs (QA-A-05/08); `internal/agentrender` ← `chat_render`↔
  `eval` ~80-LOC set (QA-C-04); agent: one `CanonicalArgs` (QA-A-01) + one `isTransientNetworkErr`
  (QA-A-02); web: single `getJSON` import (QA-D-01) + reuse shared `focusTrap.ts` (QA-D-02).
- **D-08 — Fold in QA-D-08 (frontend skeleton unification).** Pick one skeleton/loading system and
  unify the duplicates during the web dedup pass (we're already in those files). This is an **explicit
  scope addition** beyond QUAL-03's named items.

### Parity / test strategy
- **D-09 — Characterization/golden parity per extraction.** Each extraction gets a table test that
  feeds the extracted helper the **union of inputs the old copies handled** and asserts identical
  output — proving behavior is preserved across the merge (not merely "the new helper has tests").
- **D-10 — Test-first sequencing.** For each extraction: write the characterization/parity test
  against the **current duplicated code first** (confirm green) → extract → confirm still green
  (Michael Feathers' "tests before refactor" safety net). The dead-code triage/deletions run first,
  as the clean-slate wave.

### Commits & process
- **D-11 — Per-item atomic commits.** One commit per deletion / wiring / extraction / test-gap.
  Bisectable history, small reversible changes (matches audit's "small reversible commits" + the
  operator's per-concept commit habit).
- **D-14 — Sequential, no worktrees.** Run waves/plans sequentially without git worktrees. Cleanup
  edits touch shared files (neostore callers, envutil adopters) so parallel worktrees would conflict +
  churn; plus the known GSD worktree cleanup-wave Windows bug. Per-item atomic commits already give
  reversibility. (Overrides `config.use_worktrees`/`parallelization` for this phase.)

### CI & coverage
- **D-12 — Add the `memory_integration` CI matrix leg.** Wire it so memory-tagged tests actually run
  in CI (no-skip-as-green), closing QA-A-09 during the test-gap wave. If inspection shows memory tests
  already run with no real gap, document that instead of adding a redundant leg.
- **D-13 — New packages each ≥85%, registered in the gate.** Add `internal/neostore`,
  `internal/envutil`, `internal/agentrender` to `scripts/coverage_gate.sh` owned-surface; each must
  independently clear the ≥85% floor (CLAUDE.md per-package rule). The characterization tests should
  carry them there.

### Claude's Discretion
- Per-symbol dead-vs-unwired triage verdicts (D-01); per-package extraction depth (D-06); remove-vs-
  wire-vs-document for `AURA_MEMORY_EMBED_*` once in-process readers are checked (D-05); wave/plan
  ordering within the test-first principle (D-10). Escalate to the operator only when a wiring (D-02)
  proves non-trivial or an exported deletion needs sign-off (D-04).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Quality audit (primary source — read all 5)
- `docs/audit/quality/README.md` — synthesis, recurring themes (T1–T5), Wave plan (§E), high-confidence
  quick wins (§C), risky-verify-first items (§D), and the validation procedure (§F:
  `deadcode`+`knip`+repo-wide `rg` confirmation, `make coverage`, `golangci-lint`). **MANDATORY.**
- `docs/audit/quality/slice-A-agent-core.md` — `CanonicalArgs` (QA-A-01), `isTransientNetworkErr`
  (QA-A-02), `RequestID` re-stamp (QA-A-12), `truncateTailBytes` test (QA-A-06), `AURA_*` knobs
  (QA-A-05/08), `memory_integration` leg (QA-A-09), `llm_agent.go:235` Build (QA-A-11), `AgentTier`
  dead-field caveat (QA-A-07). *(QA-A-03 double-Validate/pool-leak is OUT → Phase 33/34.)*
- `docs/audit/quality/slice-B-persistence.md` — `neostore` members: `hashText`/`asString`/`asFloats`
  (QA-B-01), `GraphClient` (QA-B-02), `numericFromFloat` (QA-B-04). *(QA-B-08 int32 is OUT → 33/34.)*
- `docs/audit/quality/slice-C-transport-web.md` — `envutil` (QA-C-02), `agentrender` (QA-C-04),
  `indexByte`/`stringList` (QA-C-10), telebot blank import (QA-C-12), `assets.Status*` (QA-C-09),
  `AURA_MEMORY_EMBED_*` settings (QA-C-06), `truncateRunes` fold (QA-C-13), setup ordering (QA-C-08),
  `web/throttle` test (QA-C-07). *(QA-C-03 trust-norm → 38; QA-C-01 decode-body → 38/40.)*
- `docs/audit/quality/slice-D-frontend-ops.md` — `getJSON` ×3 (QA-D-01), focus-trap (QA-D-02),
  skeleton unify (QA-D-08). *(QA-D-03 LoginPage split + QA-D-07 CI `./...` already closed in Phase 31.)*

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` §QUAL-02 / QUAL-03 / QUAL-05 (lines ~114–117) — the locked requirement
  definitions. Note QUAL-04 (line ~116) is OUT of this phase.
- `.planning/ROADMAP.md` §Phase 32 (success criteria C1–C3) — the machine-checkable acceptance bar.

### Discipline (standing project rules these decisions inherit)
- `CLAUDE.md` — per-package coverage ≥85% (hard floor), no-skip-as-green CI, deferred-tool pattern,
  refactor-on-touch + ≤600-LOC cap, 3-strike/scope-control, commit discipline (1 concept = 1 commit).
- `scripts/coverage_gate.sh` — owned-surface coverage gate (register the 3 new packages, D-13).
- `scripts/go_packages.sh` — CI package-list source (Phase 31; no raw `./...`).

### ⚠ Source conflict to reconcile (planner)
- **Authula `ensureAuthulaSearchPath` DSN test:** ROADMAP §Phase 32 C3 + REQUIREMENTS QUAL-05 place it
  **IN** Phase 32; audit README §E routes it to **Phase 34 (MUSR-06)**. Default: keep it IN Phase 32
  (it's a low-risk test-only addition). If it turns out to require Authula-cutover infrastructure not
  yet present, the planner may defer it to 34 with a note. Not a blocker.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **Dead-code confirmation tooling:** `deadcode ./...`, `knip` (web), `golangci-lint` (with `dupl`
  threshold 100, `_test.go` excluded), repo-wide `rg` incl. build-tagged + `_test.go`. Run all before
  any removal (audit §F + D-04).
- **Tagged integration tiers:** existing `db_integration`, `neo4j_integration` build tags; add a
  `memory_integration` leg modeled on them (D-12), with skip-helpers that `t.Fatal` under `$CI`.
- **Coverage gate:** `scripts/coverage_gate.sh` owned-surface allowlist (`AURA_COVERAGE_MIN`-tunable;
  generated sqlc + agenttest excluded). New packages must be added (D-13).
- **Exact duplication inventory:** each slice file lists the copied helpers with `file:line` — the
  planner reads those to locate every call site before extracting.

### Established Patterns
- **Characterization testing** (golang-testing skill): table-driven, `goleak.VerifyNone` in `TestMain`,
  race detector — the parity-test vehicle for D-09/D-10.
- **`internal/*` package boundaries:** all extraction targets are `internal/` → Go forbids external
  import → "public API surface" concern does not apply to confirmed-dead exports (informs D-04).
- **PRD-mandated `agent ⇸ agui` import boundary** holds (audit confirms) — extractions must not
  introduce a back-edge across it.

### Integration Points
- `neostore` callers across the persistence/learn/store packages (B); `envutil` adopters across
  transport/web + agent-tool knob readers (C+A); `agentrender` consumed by `chat_render` and `eval`;
  web `getJSON`/`focusTrap`/skeleton consumed across frontend feature components.

</code_context>

<specifics>
## Specific Ideas

- Operator's framing of the scope question — *"check if it's really dead or needs wired"* — is the
  spine of this phase: treat every dead-code flag as a triage question (D-01/D-02), because deleting
  an intended-but-unfinished surface (e.g. `assets.Status*`) would destroy real work.
- Test-first (D-10) + characterization parity (D-09) is the operator's chosen safety net — the tests
  exist and pass against the OLD code before any extraction moves it.

</specifics>

<deferred>
## Deferred Ideas

- **QUAL-04 correctness** (int32 overflow guard QA-B-08; `bootChatEnvWithConfig` single-`Validate` +
  deferred pool-close QA-A-03; `AURA_*` hot-path env catalogue) → **Phase 33 (profiles) / Phase 34**.
- **MCP trust-normalization unify** (QA-C-03 / F-027) → **Phase 38** (with full trust tests).
- **`decode*Body` strict-decode unify** (QA-C-01 / F-052) → **Phases 38/40**.
- **Whole-tree dead-code sweep** beyond the audit + refactor-on-touch — intentionally not done here
  (D-03); if `deadcode`/`knip` surface large untouched-file findings, log them as a follow-up, don't
  act this phase.

</deferred>

---

*Phase: 32-Quality Cleanup — Dead Code + Shared Helpers*
*Context gathered: 2026-06-29*
