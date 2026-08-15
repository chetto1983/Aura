---
phase: 45-harness-correctness
plan: 07
subsystem: agent-harness
tags: [mcp, arcadedb, memory, supersede, fact-key, canonical-subject, skills, tdd]

# Dependency graph
requires: ["45-06"]
provides:
  - "cmd/arcadedb-mcp/tool_memory.go: MemorySearchHit.FactKey published on every recall hit (`fact_key`), MemoryUpsertFactInput.SupersedesFactKey (`supersedes_fact_key`), and MemoryUpsertFactOutput.{Refused, Reason, Candidates} — the model-facing half of 45-06's Go contract"
  - "cmd/arcadedb-mcp/tool_memory.go: canonicalSubject(subject, identityID, displayName) — collapses the operator's two names (identity UUID / display name) to one canonical subject at the MCP boundary (MEM-04, D-19)"
  - "cmd/arcadedb-mcp/main.go: newServer threads the operator display name (AURA_MEMORY_OPERATOR_DISPLAY_NAME) to the upsert handler, mirroring the existing withMemoryUserIdentifier bridge"
  - "internal/skills/embed/memory-aura/SKILL.md: the refuse-or-close semantics and fact_key/supersedes_fact_key documented at both ends (Recall and Correcting)"
affects: [45-08]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "A refusal is a SUCCESSFUL tool call (nil error) carrying refused/reason/candidates — never an mcp.ToolCallError, because an error routes through execTool's RejectOperation/MarkOperationIndeterminate path and would durably record a failed mutation for what is a clean, effect-free refusal (D-17, Pitfall 4)"
    - "Contract and its published description ship in the SAME plan: the model-facing tool schema and the skill text the model reads before calling it cannot be allowed to disagree across a follow-on"
    - "Canonicalize toward the form the graph already prefers, measured — not toward the form that looks more canonical in the abstract"
    - "RED-phase compiling stub for a whole-tree-linted repo: canonicalSubject landed as an identity passthrough wired at its call site, so the RED commit fails on BEHAVIOUR rather than on build (go vet ./... and golangci-lint both run pre-commit over the whole tree)"

key-files:
  created:
    - cmd/arcadedb-mcp/tool_memory_subject_test.go
  modified:
    - cmd/arcadedb-mcp/tool_memory.go
    - cmd/arcadedb-mcp/tool_memory_test.go
    - cmd/arcadedb-mcp/main.go
    - cmd/arcadedb-mcp/memory_live_integration_test.go
    - cmd/arcadedb-mcp/tool_graph_schema_test.go
    - cmd/arcadedb-mcp/tool_manifest_test.go
    - docs/arcadedb-mcp-live-tools.json
    - internal/skills/embed/memory-aura/SKILL.md

key-decisions:
  - "HUMAN GATE (Task 1, checkpoint:decision, blocking): the operator selected `ship-as-specified` — ship the D-15/D-16/D-17 field shapes verbatim with no renames, AND update internal/skills/embed/memory-aura/SKILL.md in THIS plan rather than deferring it to a STATE.md todo. Rationale recorded at the gate: Phase 45 exists because the harness told the model things that were not true; a skill file asserting the pre-D-16 unconditional-close behaviour would reproduce that same defect one layer up, in the text the model reads before acting."
  - "The SKILL.md edit knowingly widens this plan past the blast radius CONTEXT.md lists (which does not include internal/skills/). That widening is the human decision above, not an accident — recorded here as an approved scope deviation."
  - "canonicalSubject canonicalizes to the DISPLAY NAME, not the identity UUID. Measured 2026-08-13 against the live operator database (mem_b130c94d_a213_463a_a797_ec124104363a): 10 FACT edges touch the entity \"Davide\" against 2 touching the identity UUID — onboarding writes profile-entity facts off the operator's name, and only the preference facts use identityID directly. Canonicalizing toward the prevalent form is the direction that does not deepen the existing split; choosing the rarer form would have rewritten the majority of future writes away from where nine-tenths of the graph already sits."
  - "With AURA_MEMORY_OPERATOR_DISPLAY_NAME unset, the identity UUID stays canonical. A UUID-named subject still normalizes (TrimSpace + case) — it just has nothing more human to become until an operator configures a name."
  - "canonicalSubject is deliberately narrow: TrimSpace + case-insensitive equality against exactly the two known identifiers. No substring matching, no fuzzy matching, no alias table. A general Entity alias mechanism is Phase 49's job (D-19), and shipping a weaker one here would be something that phase then has to undo. Proven by two negative tests: a subject merely CONTAINING the display name, and one merely containing the UUID, both pass through untouched."
  - "A blank or whitespace-only subject is never canonicalized — Fact.validate rejects it downstream, and inventing a subject here would hide that rejection behind a silent rewrite (errors should never pass silently)."
  - "Idempotent by construction: the returned value is always exactly identityID or displayName, both already trimmed, so a second pass matches trivially and returns the same value. Asserted directly by TestCanonicalSubjectIsIdempotent rather than argued."

requirements-completed: [HARN-04, MEM-04]

# Metrics
duration: ~95min across 3 dispatches (2 executor stalls, see Issues Encountered)
completed: 2026-08-15
---

# Phase 45 Plan 07: Expose the fact-identity contract at the MCP boundary Summary

**Everything 45-06 made possible client-side is now reachable by the model: a recall hands back a `fact_key`, a correction can name exactly one fact with `supersedes_fact_key`, an ambiguous or empty correction comes back as a refusal carrying its `candidates` instead of a silent broad close, the operator's two names collapse to one canonical subject, and the skill text the model reads before calling any of it now describes what the tool actually does.**

## Performance

| Metric | Value |
|---|---|
| Tasks | 3/3 (Task 1 was the human decision gate — no artifacts) |
| Commits | 5 (2 RED, 2 GREEN, 1 docs) |
| `tool_memory.go` | 406/600 LOC — no split needed |
| Executor dispatches | 3 (2 stalls, recovered) |

## Accomplishments

- **`fact_key` published on every recall hit** (`MemorySearchHit.FactKey`), so the model receives the identifier it needs to name one fact later. Surfaced through both `memory_search` and `memory_facts_about`.
- **`supersedes_fact_key` accepted on upsert.** When supplied, the close targets exactly that edge and 45-06's subject+predicate candidate resolution is skipped entirely.
- **Refusal payload** — `refused`, `reason`, `candidates`, with `superseded: 0` — returned as a **successful** tool call. This is the load-bearing detail: an `mcp.ToolCallError` would route through `execTool`'s `RejectOperation`/`MarkOperationIndeterminate` path and durably record a failed mutation for what is a clean, effect-free refusal (D-17, Pitfall 4).
- **`canonicalSubject`** collapses the operator's identity UUID and display name to one subject at the boundary (MEM-04, D-19), so a fact written under one name is recallable under the other.
- **`memory-aura/SKILL.md` corrected** — the sentence claiming `supersedes` "closes every still-valid fact sharing that subject and predicate" was false as of 45-06 and is now replaced with the refuse-or-close contract; `fact_key`/`supersedes_fact_key` documented in Recall (where the model receives one) and Correcting (where it passes one back).

## Task Commits

| Task | Commit | What |
|---|---|---|
| 1 | — | Human decision gate (`checkpoint:decision`, blocking). No artifacts by design. Resolved `ship-as-specified`. |
| 2 | `71d593a79` | RED — failing tests for `supersedes_fact_key`, `fact_key` on hits, and the refusal payload |
| 2 | `62d11a67a` | GREEN — `supersedes_fact_key`, `fact_key` on hits, refusal payload (D-15, D-17) |
| 3 | `0509027ca` | RED — failing tests for `canonicalSubject` and its call site (MEM-04, D-19) |
| 3 | `047a07562` | GREEN — canonicalize the operator subject to one form (MEM-04, D-19) |
| gate | `ec70b17ee` | docs — skill text tells the model what `supersedes` actually does now |

## Files Created/Modified

**Created:** `cmd/arcadedb-mcp/tool_memory_subject_test.go` (91 lines — 12 subtests incl. two negative containment cases and idempotence)

**Modified:** `cmd/arcadedb-mcp/tool_memory.go`, `tool_memory_test.go`, `main.go` (`newServer` threads the display name), `memory_live_integration_test.go`, `tool_graph_schema_test.go`, `tool_manifest_test.go`, `docs/arcadedb-mcp-live-tools.json`, `internal/skills/embed/memory-aura/SKILL.md`

## Deviations from Plan

1. **SKILL.md widening (approved).** `internal/skills/` is not in CONTEXT.md's listed blast radius. Included on the explicit human decision at Task 1's gate — see key-decisions.
2. **`newServer` signature change (in-flight repair).** Threading the display name to the upsert handler changed `memoryUpsertFactHandler` from `(*tenants, clock)` to `(*tenants, clock, string)`, which required updating `newServer` and three test call sites. An executor stalled mid-way through this edit, leaving the tree test-broken for one dispatch; the repair is folded into `0509027ca`.

## Issues Encountered

**Two executor stalls.** The plan's executor stalled twice (600s watchdog, no recovery) — once mid-edit on the `newServer` signature change, once after landing Task 3's RED. Neither lost committed work, and `go build ./...` stayed clean throughout (both breakages were test-only). Task 3's GREEN, the SKILL.md update and this SUMMARY were completed inline by the orchestrator after the second stall, rather than risking a third dispatch.

**Shared working tree.** A concurrent session committed to this same branch during execution (`e106f0897`, `a88ddd6ea` landed *between* this plan's commits) and repeatedly staged files under `internal/db/` and `internal/documents/`. Those were unstaged before each commit; `git diff --cached --name-only` was checked before every commit in this plan, and no foreign path was ever included. Verified after the fact: no 45-07 commit touches `internal/db/`, `internal/documents/`, `internal/assets/` or `cmd/aura/`.

## User Setup Required

`AURA_MEMORY_OPERATOR_DISPLAY_NAME` — optional. Unset, canonicalization still works and resolves to the identity UUID; set, it resolves to the display name, which is the form 10 of 12 measured live FACT edges already use.

## TDD Gate Compliance

Both behaviour-adding tasks landed RED before GREEN (`71d593a79`→`62d11a67a`, `0509027ca`→`047a07562`). Task 1 was a decision gate with no artifacts and Task 3's docs commit is text-only — both exempt.

RED commits used compiling stubs (identity passthrough) rather than undefined symbols, because the pre-commit hook runs `go vet ./...` and `golangci-lint` over the whole tree — an undefined-symbol RED cannot be committed here at all. Each RED was verified to fail on behaviour before its GREEN.

## Verification

| Check | Result | Where |
|---|---|---|
| `go build ./...` | clean | Windows |
| `go vet ./...` | clean | Windows |
| `go test -race ./cmd/arcadedb-mcp/ ./internal/arcadedb/ ./internal/skills/` | **ok** (1.328s / 2.102s / 1.867s) | **WSL** (`/mnt/d/Repo/Aura`) — Windows lacks CGO for `-race` |
| `TestCanonicalSubject` + idempotence + blank-subject + call-site | 12 subtests PASS | Windows |
| pre-commit hooks (gofmt, file-size, vet, lint) | pass, **0 lint issues** | Windows |

Not claimed: no `arcadedb_integration` tier was executed in this plan's final dispatch, so the live-stack proof of these MCP shapes is 45-08's job, not something this SUMMARY asserts.

## Next Phase Readiness

45-08 (live E2E + phase close) is unblocked and is the plan that must exercise these shapes against the running stack. Two things it should carry: the `aura:local` image must be rebuilt from current HEAD before the E2E — three plans' worth of agent-loop changes (45-03, 45-04) and this MCP surface all landed after any existing image — and the run should drive a real correction through `fact_key`/`supersedes_fact_key` rather than asserting the schema.

---

## Self-Check: PASSED

- [x] Both behaviour-adding tasks committed RED→GREEN, verified failing before passing
- [x] `fact_key`, `supersedes_fact_key`, `refused`/`reason`/`candidates` all present with the D-15/D-17 names, unrenamed
- [x] Refusal returns nil error (successful call), never `mcp.ToolCallError`
- [x] `canonicalSubject` narrow: no substring/fuzzy/alias matching — proven by two negative tests
- [x] `tool_memory.go` 406/600 LOC
- [x] `go build`/`go vet` clean; `-race` green under WSL on all three touched packages
- [x] Human gate honoured: shapes shipped verbatim, SKILL.md updated in this plan
- [x] No foreign path in any commit — index verified before each
