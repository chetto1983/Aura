# Phase 32: Quality Cleanup — Dead Code + Shared Helpers - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-29
**Phase:** 32-Quality Cleanup — Dead Code + Shared Helpers
**Areas discussed:** Cleanup scope boundary, Dead exported-symbol policy, Extraction depth, Parity-test strategy, Wave sequencing, Commit granularity, Sidecar env handling, Frontend skeleton (QA-D-08), memory_integration CI leg, New-package coverage, Parallelization

---

## Cleanup scope boundary

| Option | Description | Selected |
|--------|-------------|----------|
| Refactor-on-touch | Audit items + new dead/dup the tools flag within touched files; no repo-wide hunt | |
| Strict to audit list | Only the exact named items; log anything else as follow-up | |
| Full repo-wide sweep | Run deadcode/knip across whole tree, remove all confirmed dead | |

**User's choice:** *(free text)* "check if is really dead or need wired" — reframed the question.
**Notes:** Operator reframed scope from a reach-band to a per-item **triage**: every flagged symbol is
checked for *genuinely dead* vs *intended-but-unwired*. Follow-up question asked on what to do when a
symbol is unwired-but-intended → operator chose **"Wire it up here"** (option 1 of 3: wire now vs
wire-if-trivial-else-flag vs flag-only). Reach defaults to refactor-on-touch (CLAUDE.md standing rule),
no repo-wide sweep. → CONTEXT D-01/D-02/D-03.

---

## Dead exported-symbol policy

| Option | Description | Selected |
|--------|-------------|----------|
| Delete if tool-confirmed | Remove confirmed-dead incl. exported (internal/ → no external API) | |
| Preserve exports, annotate | Keep unused exports with intent comment | |
| Case-by-case on exports | Unexported deleted freely; each exported surfaced for keep/kill | ✓ |

**User's choice:** Case-by-case on exports.
**Notes:** "Confirmed" = deadcode + knip + repo-wide rg agreement. → CONTEXT D-04.

---

## Extraction depth

| Option | Description | Selected |
|--------|-------------|----------|
| Minimal + migrate dups | Extract dup helper, update only copied call sites | |
| Canonical home, migrate all | New package is single home; migrate all related access | |
| You decide per package | Claude picks minimal vs canonical per extraction from the code | ✓ |

**User's choice:** You decide per package.
**Notes:** Expected: neostore canonical (funnel Neo4j access), envutil/agentrender minimal. → CONTEXT D-06/D-07.

---

## Parity-test strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Characterization/golden | Table test over union of old-copy inputs, assert identical output | ✓ |
| Unit-test new helper | Standard unit tests on representative inputs | |
| Call-site regression only | Rely on existing call-site tests passing post-extraction | |

**User's choice:** Characterization/golden. → CONTEXT D-09.

---

## Wave sequencing

| Option | Description | Selected |
|--------|-------------|----------|
| Test-first per extraction | Parity test vs current code first (green) → extract → green; deletions first | ✓ |
| Audit wave order | Wave 1 deletions → Wave 2 extractions → Wave 4 tests last | |
| You sequence it | Claude orders for safety + parallelism | |

**User's choice:** Test-first per extraction (Feathers safety net). → CONTEXT D-10.

---

## Commit granularity

| Option | Description | Selected |
|--------|-------------|----------|
| Per-item atomic | One commit per deletion/extraction/test-gap | ✓ |
| Per QUAL group | One commit each for QUAL-02 / QUAL-03 / QUAL-05 | |
| Executor default | GSD executor's default atomic-per-task | |

**User's choice:** Per-item atomic. → CONTEXT D-11.

---

## Sidecar env handling (AURA_MEMORY_EMBED_*)

| Option | Description | Selected |
|--------|-------------|----------|
| Remove from Go + document | Drop keys from Go settings; document sidecar ownership in .env.example | ✓ |
| Keep as documented pass-through | Leave in Go settings with intent comment | |
| You decide on inspection | Choose after seeing if anything in-process reads them | |

**User's choice:** Remove from Go + document (when triage confirms sidecar-owned). → CONTEXT D-05.

---

## Frontend skeleton (QA-D-08)

| Option | Description | Selected |
|--------|-------------|----------|
| Defer | Out of named scope; only touch via refactor-on-touch | |
| Fold in now | Unify the skeleton system during the web dedup pass | ✓ |

**User's choice:** Fold in now (explicit scope addition to QUAL-03). → CONTEXT D-08.

---

## memory_integration CI leg (QA-A-09)

| Option | Description | Selected |
|--------|-------------|----------|
| Add the CI leg | Wire memory_integration matrix leg (no-skip-as-green) | ✓ |
| Defer to a later phase | Log as follow-up for a memory-touching phase | |
| You decide on inspection | Add only if a real no-skip gap exists | |

**User's choice:** Add the CI leg. → CONTEXT D-12.

---

## New-package coverage

| Option | Description | Selected |
|--------|-------------|----------|
| Each ≥85% + register in gate | Add 3 packages to coverage_gate.sh, each clears 85% | ✓ |
| Aggregate only | Count toward owned-surface aggregate, no per-package enforce | |

**User's choice:** Each ≥85% + register in gate. → CONTEXT D-13.

---

## Parallelization

| Option | Description | Selected |
|--------|-------------|----------|
| Sequential, no worktrees | Run waves sequentially; per-item commits give reversibility | ✓ |
| Hybrid | Parallelize independent items, serialize overlapping-file edits | |
| Parallel worktrees per plan | Full worktrees + parallelization | |

**User's choice:** Sequential, no worktrees (shared-file conflict risk + Windows cleanup-wave bug). → CONTEXT D-14.

---

## Claude's Discretion

- Per-symbol dead-vs-unwired triage verdicts; per-package extraction depth (minimal vs canonical);
  remove-vs-wire-vs-document for AURA_MEMORY_EMBED_* on inspection; wave/plan ordering within the
  test-first principle. Escalate on non-trivial wiring or exported-symbol deletions.

## Deferred Ideas

- QUAL-04 correctness (int32 guard, double-Validate/pool-leak, AURA_* env catalogue) → Phases 33/34.
- MCP trust-normalization unify (F-027) → Phase 38.
- decode*Body strict-decode unify (F-052) → Phases 38/40.
- Whole-tree dead-code sweep beyond audit + refactor-on-touch → log-only follow-up.
