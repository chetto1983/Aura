---
phase: 22-bug-fix
plan: "05"
subsystem: agent-skills-audit-closeout
tags: [skills, self-extension, dead-code, finding-ledger, audit, validation, observability]

requires:
  - phase: 22-bug-fix (22-01..22-04)
    provides: the AG-### hardening waves (panic firewall, secrets/observability, MCP/reasoning/budget, hooks/provenance/tools/workflow) the ledger folds in
provides:
  - single honest skill schema (skillParamsSchemaHonest) with the dead duplicate removed
  - confirmed/routed dispositions for the four NEEDS-CONFIRMATION findings (AG-028 dead code removed; AG-034 redaction routed to the store layer; AG-041 WithDeadline confirmed; AG-043 leak-free proven)
  - docs/audit/22-finding-ledger.md naming every AG-001..064 with a constrained disposition + evidence
  - docs/audit/22-LIVE-SIGNOFF-2026-06-15.md (Part A automated evidence + Part B operator runbook)
  - 22-VALIDATION.md / 22-UAT.md phase close-out artifacts
affects: [phase-22 Gate-3 close, web-cockpit exposure gate, future Slice 1.7 capability gate]

tech-stack:
  added: []
  patterns:
    - "single source-of-truth schema (no dead duplicate const)"
    - "confirmed+routed disposition for cross-package fixes (D-09): confirm at the correct boundary, do not silently fix outside scope"
    - "two-part validation doc: automated-done vs pending-operator, never a fabricated pass"

key-files:
  created:
    - docs/audit/22-finding-ledger.md
    - docs/audit/22-LIVE-SIGNOFF-2026-06-15.md
    - .planning/phases/22-bug-fix/22-VALIDATION.md
    - .planning/phases/22-bug-fix/22-UAT.md
  modified:
    - internal/agent/tools/skill.go
    - internal/agent/tools/skill_test.go
    - internal/agent/mcptools/mount.go
    - internal/agent/mcptools/managed_mount_test.go
    - internal/agent/event_test.go
    - docs/audit/README.md
    - docs/audit/audit-index.json
    - .planning/phases/22-bug-fix/deferred-items.md

key-decisions:
  - "AG-044: skillParamsSchemaHonest was ALREADY wired in Spec(); the fix is deleting the dead skillParamsSchema duplicate (it claimed an approval gate the code never enforced) and rewriting the surviving comment to state the real single-operator boundary."
  - "AG-051: the dormant skill writeAction pending-pause path cannot pause the turn — the pause is name-gated to ask_user in llm_agent_pause.go; TestAskUserOnlyPauseConstraint proves it. No code change needed beyond confirming the invariant."
  - "AG-028: deadcode confirmed openManagedServer unreachable (test-only callers). Removed it; MountManagedServer inlines the identical branches, re-covered by two MountManagedServer tests (zero coverage loss)."
  - "AG-034: redaction + byte cap are purely a DB/projection concern (internal/toolinvocations.RedactForLedger at store toParams), already implemented + tested. Per D-09, confirmed+routed — no event.go shape change; added an agent-side test pinning the raw-forward + store-redact contract."
  - "AG-041/AG-043: confirmed closed by prior waves (22-03 WithDeadline @ cmd/aura/agent.go:99; 22-04 goleak break-at-every-index)."
  - "Task 4 destructive coverage + WSL quality + full live stack are operator-coordinated and recorded PENDING with exact commands; no fabricated pass (CLAUDE.md no-skip-as-green)."

patterns-established:
  - "Disposition vocabulary is constrained to exactly fixed+test | accepted+rationale | confirmed+routed."
  - "Every NEEDS-CONFIRMATION finding is resolved to one of the three, never left ambiguous."

requirements-completed: [HARDEN-11, HARDEN-12]

duration: ~15min
completed: 2026-06-15
---

# Phase 22 Plan 05: Phase-22 Close-out (Skill Honesty + Finding Ledger + Validation) Summary

**Reconciled the skill self-extension schema to one honest source of truth (AG-011/044/051), resolved the four NEEDS-CONFIRMATION findings (AG-028 dead code removed, AG-034 redaction routed to the store layer, AG-041/043 confirmed closed), produced the AG-001..064 finding ledger, and authored the two-part automated/pending validation evidence — closing HARDEN-11 and HARDEN-12 with zero audit residue.**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-06-15T18:46:24Z
- **Tasks:** 4 (Task 1+2 TDD; Task 3+4 docs/validation)
- **Files:** 8 modified/created code+docs + 4 new validation docs
- **Commits:** 4 atomic task commits, each naming its AG-### findings (D-11)

## Accomplishments

- **Task 1 — Skill honesty (AG-011/044/051):** deleted the dead duplicate `skillParamsSchema` const (it advertised "you cannot approve your own changes" — an approval gate the code never enforced), leaving the single honest `skillParamsSchemaHonest` already wired in `Spec()`. Rewrote the surviving doc comment to state the real single-operator boundary (`always:false` create/update auto-activates in-container after validate+audit; `always:true`+delete stay approval-gated). Confirmed AG-051: the dormant `writeAction` pending-pause cannot pause the turn (name-gated to `ask_user`). Added `TestSkillSchemaIsHonestNotDishonest`.
- **Task 2 — Confirm/route (AG-028/034/041/043):** `deadcode ./...` confirmed `openManagedServer` unreachable → removed it, re-covered its branches through the live `MountManagedServer` with two new tests. Confirmed AG-034 redaction is purely in the DB store layer (`internal/toolinvocations.RedactForLedger`, D-09) and added an agent-side contract test. Confirmed AG-041 (`Budget.WithDeadline` @ `cmd/aura/agent.go:99`, 22-03) and AG-043 (goleak break-at-every-index, 22-04) closed.
- **Task 3 — Finding ledger:** `docs/audit/22-finding-ledger.md` names every AG-001..064 exactly once with a constrained disposition (52 fixed+test, 3 confirmed+routed, 9 accepted+rationale), code+test evidence, the closing wave commit, and a HARDEN-01..12 traceability table. Updated `docs/audit/README.md` + reconciled `docs/audit/audit-index.json` (incl. the 63→64 canonical-count note).
- **Task 4 (bounded) — Automated evidence + live runbook:** ran the non-destructive automated bar (build/vet/test/race/cache) and authored `22-LIVE-SIGNOFF-2026-06-15.md`, `22-VALIDATION.md`, `22-UAT.md` with (A) real automated output + (B) the operator-coordinated coverage/quality/live runbook marked `pending`.

## Task Commits

1. **Task 1: Skill self-extension honesty** — `5d8f070e` (fix) — AG-011, AG-044, AG-051
2. **Task 2: Confirm/route NEEDS-CONFIRMATION** — `657b438b` (fix) — AG-028, AG-034, AG-041, AG-043
3. **Task 3: AG-001..064 finding ledger + close-out** — `036575b5` (docs) — HARDEN-12 (AG-### coverage)
4. **Task 4: Automated evidence + live sign-off runbook** — `fa298087` (docs) — HARDEN-12

## Automated Evidence (Task 4 Part A — DONE @ HEAD 036575b5)

| Command | Result |
|---------|--------|
| `go build ./...` | PASS (exit 0) |
| `go vet ./...` | PASS (exit 0) |
| `go test ./...` (untagged) | all `internal/...` PASS; only `cmd/aura` `TestProductionContainerArtifactsMatchFatImageContract` fails (pre-existing `:nitro`/`:exacto` compose drift, out of scope, in `deferred-items.md`) |
| `go test -race ./internal/agent/... ./internal/swarm/...` | PASS (exit 0) — race-clean across 7 packages |
| `bash scripts/cache_invariant_audit.sh` | PASS (exit 0) — 22 identical `messages[0]`/`messages[1]`/skill-manifest hashes (prefix stable after the skill-schema edit) |

## Pending Operator Sign-Off (Task 4 Part B — NOT run)

Recorded with exact commands + acceptance in `docs/audit/22-LIVE-SIGNOFF-2026-06-15.md`:

- **B1 coverage** — `make coverage` (≥85% owned-surface; **wipes shared PG** — deferred so parallel Codex/superpowers sessions are not corrupted).
- **B2 quality** — `golangci-lint run ./...`, `govulncheck ./...`, `go-mutesting` ≥70% on `internal/agent/llm_agent_parallel.go`, `internal/agent/budget_dedup.go`, `internal/agent/mcptools/bridge_reconnect.go` (WSL `~/go/bin` toolchain).
- **B3 live** — `aura serve` full stack + the acceptance matrix (metrics scrape, CDP Telegram, GLM-OCR fs-cap, MCP reconnect, reasoning fallback, skill self-extension, DSN boundary, ledger redaction).

## Deviations from Plan

None requiring code beyond the plan's named findings. Notable scope decisions:

- **AG-034 routed, not patched in `event.go`:** the plan asked to confirm whether an `event.go` shape change is needed; it is not — redaction lives at the persistence boundary (already implemented + tested). Per D-09 the correct disposition is `confirmed+routed` with a test pinning the contract, not a silent in-agent fix.
- **AG-028 tests swapped, not deleted-only:** rather than just deleting the two dead-code-only tests, I replaced them with `MountManagedServer`-driven tests covering the identical stdio/HTTP branches, so removing the dead function loses no branch coverage.

## Out of Scope (logged to deferred-items.md, NOT fixed)

- `cmd/aura` `TestProductionContainerArtifactsMatchFatImageContract` — pre-existing compose `:nitro`/`:exacto` drift (`136325dc`); no plan-05 file touches `compose.yaml`/`cmd/aura`.
- `internal/skills` dead funcs (`BM25Corpus`, `SnippetInvocation`, `ValidateNameAgainstDir`) flagged by `deadcode` — outside the Phase-22 audited surface; not AG-### findings.

## Known Stubs

None. The skill self-extension behaviour is wired and tested; the only "pending" items are the operator-coordinated destructive coverage gate, WSL quality bar, and full live-stack sign-off — all documented with exact commands + acceptance, not stubs.

## Next Phase Readiness

- Phase 22 is **automated-green** (Part A) with the operator Part-B sign-off pending. Once B1–B3 are signed off, Phase 22 is Gate-3 closed and the web cockpit (Phase 23+) can build on a hardened perimeter.
- The MCP timeout migration note (`=0`→default 60s, `-1`→infinite) is recorded in the live sign-off (D-06) for operators with `AURA_MCP_CALL_TIMEOUT_SEC=0` set.

## Self-Check: PASSED

All created files exist on disk (`22-finding-ledger.md`, `22-LIVE-SIGNOFF-2026-06-15.md`, `22-VALIDATION.md`, `22-UAT.md`, `22-05-SUMMARY.md`) and all 4 task commits are in git history (`5d8f070e`, `657b438b`, `036575b5`, `fa298087`). The ledger names all 64 AG IDs (verified: 64 distinct, none missing) with the three constrained dispositions. Automated bar green (build/vet/race/cache); the only `go test ./...` failure is the documented pre-existing out-of-scope `cmd/aura` compose-drift test.

---
*Phase: 22-bug-fix*
*Completed: 2026-06-15*
