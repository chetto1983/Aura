---
phase: 29-governance-write-mcp-configuration-skills-install
plan: 03
subsystem: api
tags: [skills, governance, askuser, paused-states, approval-queue, capability, supply-chain, npx, audit-ledger]

# Dependency graph
requires:
  - phase: 29-governance-write-mcp-configuration-skills-install (plan 01)
    provides: governanceWriteCapability const + the append-only audit foundation + D-09 container-isolation amendment
  - phase: 29-governance-write-mcp-configuration-skills-install (plan 02)
    provides: governance_write_seam.go GovernanceWriteProviders bundle + SetGovernanceWriteProviders + registerGovernanceWriteRoutes + the cmd/aura concrete-provider+serve.go wiring idiom
  - phase: 11-skills (Slice 7)
    provides: the Writer gate (WriteInstallPending/WriteMutationByName/Archive/Restore/Activate) + ValidateForWrite + HashSkillDir + ComputeSkillTier + ResumeHandler + skill_audit ledger
  - phase: 25-approval-center
    provides: askuser.Store (Insert/ListPendingAll) + Runner.SubmitAnswers + the /api/approvals cross-thread queue + the source-agnostic resume bridge + newSkillResumeHook
  - phase: 24-serve-auth
    provides: RequireCapability + principalIdentityID in internal/agui/auth.go
provides:
  - "internal/agui/governance_write_skills_api.go — POST /api/governance/skills/install + .../{name}/restore|archive + create/update/delete + GET .../catalog thin handlers (SKW-01/02/03)"
  - "internal/agui SkillsWriteProvider seam + the SkillsInstallInfo/SkillsCatalogResult wire types + ErrSkillActiveExists/ErrSkillInvalidInput sentinels, added to the existing GovernanceWriteProviders bundle (one setter)"
  - "cmd/aura/serve_governance_write_skills.go — skillsWriteAdapter: Install stages via the Task-1 Installer then MINTS an operator-origin ask_user pause (D-13) that surfaces in /api/approvals and resolves through the existing Runner.SubmitAnswers -> newSkillResumeHook -> Writer.Activate bridge"
  - "T-04-19 widening (D-13): the Runner AND the capability-gated operator-origin governance-write path are the writers of aura.paused_states (interfaces.go + llm_agent_pause.go comments widened)"
  - "internal/skills/writer_activate.go Writer.ActiveExists — the restore-collision stat chokepoint"
  - "cmd/aura/serve_webui.go — seven RequireCapability(governance.write) skills write mounts + serve.go buildSkillsWriteProvider wiring"
affects: [29-04, 29-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Operator-origin ask_user pause: a capability-gated cockpit write mints a paused_states row via askuser.Store.Insert (Kind=approval + ResumeContext={type:skill_approval}) so it reuses the EXISTING unified /api/approvals queue + source-agnostic resume bridge — no second queue, no new activation logic (D-13 Option A)"
    - "Deterministic governance conversation (provisioned idempotently) parents the operator-origin pause so the resume-side Conv.AppendTurn FK to aura.conversations is satisfied"
    - "Restore-collision 409 guard at the HTTP-handler/provider layer (Writer.ActiveExists stat) BEFORE Writer.Restore's os.RemoveAll — the active skill is never silently overwritten"

key-files:
  created:
    - internal/agui/governance_write_skills_api.go
    - internal/agui/governance_write_skills_api_test.go
    - cmd/aura/serve_governance_write_skills.go
    - cmd/aura/serve_governance_write_skills_integration_test.go
  modified:
    - internal/agui/governance_write_seam.go
    - internal/agui/governance_write_api.go
    - internal/runner/interfaces.go
    - internal/agent/llm_agent_pause.go
    - internal/skills/writer_activate.go
    - cmd/aura/serve_webui.go
    - cmd/aura/serve.go
    - .planning/phases/29-governance-write-mcp-configuration-skills-install/29-SPEC.md
    - .planning/REQUIREMENTS.md

key-decisions:
  - "D-13 (Option A, operator-chosen): the cockpit install MINTS an operator-origin ask_user pause via askuser.Store.Insert rather than routing through the agent's ErrAwaitingUserInput (a cockpit REST call has no agent loop). This widens T-04-19 (Runner-sole-writer) to a capability-scoped second writer. PRD-first: the SPEC/REQUIREMENTS amendment landed FIRST as its own atomic docs commit (69e68b07) before any Task 2/3 code."
  - "The operator-origin pause is parented to a deterministic governance conversation (00000000-...-0000000900a1), provisioned idempotently on first install, so the resume-side AppendTurn FK (conversation_turns -> conversations) is satisfied — paused_states.conversation_id is plain text (no FK) but the resume path writes a conversation_turns row."
  - "The SkillsWriteProvider was added to the EXISTING GovernanceWriteProviders bundle (one bundle/setter total); the agui handler stays decoupled from internal/skills via agui-side mirror wire types (SkillsInstallInfo/SkillsCatalogResult)."
  - "Restore-collision is guarded HTTP-side via the new Writer.ActiveExists stat — the provider returns ErrSkillActiveExists -> 409 BEFORE Writer.Restore runs (the os.RemoveAll landmine)."

patterns-established:
  - "Operator-origin paused_states mint behind RequireCapability(governance.write): the only askuser.Store.Insert call outside the Runner, capability-scoped, surfacing in the unified approval queue"
  - "Provider mirror-type seam: agui declares its own SkillsInstallInfo/CatalogResult/CheckItem so the handler never imports internal/skills (the cmd/aura concrete adapter translates)"

requirements-completed: [SKW-01, SKW-02, SKW-03]

# Metrics
duration: ~45min
completed: 2026-06-21
---

# Phase 29 Plan 03: Skills Install & Lifecycle Write Backend Summary

**The cockpit skills write backend (install/restore/archive/create/update/delete/catalog) over the existing Phase-11 Writer gate, with the D-13 Option-A bridge: a cockpit install mints an operator-origin `ask_user` pause via `askuser.Store.Insert` that surfaces in the SAME unified `/api/approvals` queue and activates only on the operator resume — widening the T-04-19 "Runner is the sole writer of paused_states" invariant to a capability-gated second writer.**

## Performance

- **Duration:** ~45 min (including the architectural-checkpoint resolution + the PRD-first amendment)
- **Started:** 2026-06-21T11:23:54Z
- **Completed:** 2026-06-21T12:08:00Z (approx)
- **Tasks:** 2 + 3 (Task 1 was already complete from a prior session — commit `a7bf6d18`)
- **Files modified:** 13 (4 created, 9 modified, incl. the 2 docs amendment files)

## Accomplishments
- **D-13 install→approval bridge (the resolved architectural checkpoint):** the cockpit install stages through the Task-1 Installer to `pending/` (one `skill_audit` install row, never self-activated), then mints an OPERATOR-ORIGIN `ask_user` pause via `askuser.Store.Insert` carrying `Kind=approval` + `ResumeContext={type:"skill_approval", skill_name}`. It surfaces in the EXISTING `/api/approvals` cross-thread queue (source-agnostic `ListPendingAll`) and resolves through the EXISTING `Runner.SubmitAnswers` → `newSkillResumeHook` → `ResumeHandler.Resume` → `Writer.Activate` (accept) / `DiscardPending` (decline) bridge — no new queue, no new approval/activation logic.
- **PRD-first amendment FIRST:** the SPEC/REQUIREMENTS D-13 amendment landed as its own atomic docs commit (`69e68b07`) before any Task 2/3 code (precedent 29-01 D-09), recording the T-04-19 widening + the security envelope (mintable only behind `governance.write`; no model/agent/unauthenticated mint; operator-only resolution; never auto-activates).
- **The skills write surface:** `SkillsWriteProvider` (added to the existing `GovernanceWriteProviders` bundle — one setter) + the thin install/restore/archive/create/update/delete/catalog handlers; install renders RISKY + the five-item checklist + source/hash/preview/destination + the approval token (never "safe", no `--ignore-scripts`); restore 409s on a name collision BEFORE `Writer.Restore`'s `os.RemoveAll` (the new `Writer.ActiveExists` stat guard); the catalog GET is flag-gated (`AURA_SKILLS_EXTERNAL_DISCOVERY`).
- **Routes mounted behind `RequireCapability(governance.write)`** as method+path-specific siblings (never a bare `/api/`); the concrete provider wired best-effort at the composition root (nil pool / missing skills dir → 503, never aborts boot) alongside the MCP provider in one `SetGovernanceWriteProviders` call.

## Task Commits

1. **Task 1: the npx skills install transport** — `a7bf6d18` (feat) — *pre-existing from a prior session; verified, not re-done.*
2. **Docs amendment (FIRST, atomic):** SPEC/REQUIREMENTS — operator-origin pause widens T-04-19 (D-13, Option A) — `69e68b07` (docs)
3. **Task 2: skills write handlers + SkillsWriteProvider + operator-origin approval pause** — `77c8d7cd` (feat)
4. **Task 3: mount skills write routes behind governance.write + wire concrete provider** — `229cb9d5` (feat)

_Note: a parallel `graphify`/audit tooling process interleaved two unrelated "update audit" commits (`0c00bf7c` was where my staged amendment files were swept — I amended its message to the D-13 subject `69e68b07`; `a0f319c1` landed between the amendment and Task 2). Neither touches this plan's code; my explicit per-commit path staging kept each 29-03 commit clean._

## Files Created/Modified
- `internal/agui/governance_write_skills_api.go` — the seven thin skills write handlers + sentinels→status mapping (409 restore-collision, 400 invalid input, sanitized 502)
- `internal/agui/governance_write_seam.go` — `SkillsWriteProvider` + the bundle field + `SkillsInstallInfo`/`SkillsCatalogResult` wire types + `ErrSkillActiveExists`/`ErrSkillInvalidInput`
- `internal/agui/governance_write_api.go` — `registerGovernanceWriteRoutes` now also calls `registerGovernanceSkillsWriteRoutes`
- `internal/agui/governance_write_skills_api_test.go` — httptest: install RISKY+five-checklist+token+pending-not-active; restore-409 (Restore NOT invoked); archive; create/update/delete; catalog flag-gated; 503/401; sanitized 502; the production RequireCapability mount gate (200/403/403)
- `cmd/aura/serve_governance_write_skills.go` — `skillsWriteAdapter` (the pause mint + the governance-conversation provisioning + restore-collision guard + lifecycle wraps) + `buildSkillsWriteProvider`
- `cmd/aura/serve_governance_write_skills_integration_test.go` — db_integration: the live mint→queue→approve→Activate round-trip (accept activates; decline discards)
- `internal/runner/interfaces.go` + `internal/agent/llm_agent_pause.go` — the T-04-19 widening comments (capability-scoped second writer; agent stays ask_user-name-gated)
- `internal/skills/writer_activate.go` — `Writer.ActiveExists` (restore-collision stat chokepoint)
- `cmd/aura/serve_webui.go` — seven `governanceSkill*WriteRoute` consts + mounts behind `governance.write`
- `cmd/aura/serve.go` — `buildSkillsWriteProvider` wired into the bundle alongside the MCP provider
- `.planning/.../29-SPEC.md` + `.planning/REQUIREMENTS.md` — the D-13 amendment

## Decisions Made
See `key-decisions` frontmatter. The load-bearing one is D-13 (Option A): the cockpit install mints the pause directly (an honest second `paused_states` writer, capability-scoped) rather than threading a synthetic agent loop, because a cockpit REST call has no `ask_user` tool call to name-gate on. This reuses the entire Phase-25 queue + Phase-11 resume bridge unchanged.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] `Writer.ActiveExists` added for the restore-collision 409 guard**
- **Found during:** Task 2 (the restore handler)
- **Issue:** The plan requires a 409 BEFORE `Writer.Restore` (which does `os.RemoveAll` on the active dir), but no read-only "is this name active?" primitive existed on the Writer — without it the provider could not check the collision without risking the destructive path.
- **Fix:** Added `Writer.ActiveExists(name)` (a SanitizeName-guarded `os.Stat` of `active/<name>/SKILL.md`); the concrete adapter calls it and returns `ErrSkillActiveExists` → 409 before any restore work.
- **Files modified:** internal/skills/writer_activate.go, cmd/aura/serve_governance_write_skills.go
- **Verification:** `TestGovernanceWriteSkillsRestoreCollision409` (Restore NOT invoked on a collision) + the db_integration round-trip exercise the active path.
- **Committed in:** `77c8d7cd` (Task 2)

**2. [Rule 3 - Blocking] Governance conversation provisioning for the resume-side FK**
- **Found during:** Task 2 (minting the pause)
- **Issue:** `aura.paused_states.conversation_id` is plain text (no FK), but the resume path (`Runner.SubmitAnswers` → `injectAnswer` → `Conv.AppendTurn`) writes a `conversation_turns` row, which DOES carry an FK to `aura.conversations(id)`. A purely synthetic conversation id would fail the resolve-side append.
- **Fix:** The adapter provisions a deterministic governance conversation (idempotent: Get-or-Create, tolerating a concurrent unique-violation) parented to the seeded `local` identity, and mints the pause against it.
- **Files modified:** cmd/aura/serve_governance_write_skills.go
- **Verification:** the db_integration accept round-trip resolves through the real `SubmitAnswers` and activates — proving the resume-side append succeeds.
- **Committed in:** `77c8d7cd` (Task 2)

**3. [Rule 1 - Plan imprecision] Task-3 verify grep target file**
- **Found during:** Task 3 (the verify command)
- **Issue:** The plan's Task-3 `<verify>` greps `SetGovernanceWriteProviders` in `cmd/aura/serve_webui.go`, but (per the 29-02 precedent) that wiring lives in `cmd/aura/serve.go` (the composition root), not `serve_webui.go` (which holds only the route consts + mounts). The grep as written would fail.
- **Fix:** Followed the 29-02 wiring location (serve.go for the provider, serve_webui.go for the mounts) — the intent (concrete provider wired at the composition root) is satisfied; the grep file target in the plan was imprecise.
- **Files modified:** cmd/aura/serve.go (provider wiring), cmd/aura/serve_webui.go (consts+mounts)
- **Verification:** `grep -n SetGovernanceWriteProviders cmd/aura/serve.go` shows the bundle wiring; `go build ./... && go vet ./...` clean.
- **Committed in:** `229cb9d5` (Task 3)

---

**Total deviations:** 3 (1 missing-critical, 1 blocking, 1 plan-imprecision). All necessary for correctness; no scope creep — every addition was named in the resolved-decision's approved scope (the new cmd/aura provider, the invariant-comment widening, the resume-side activation).
**Impact on plan:** The architectural checkpoint (the install→paused_states bridge) was resolved per the operator's Option-A decision; the implementation reuses the entire existing queue + resume infrastructure, adding only the capability-gated mint + its governance conversation.

## Issues Encountered
- **Parallel `graphify`/audit tooling commits on master:** an automated process committed twice on master mid-run (`0c00bf7c`/`a0f319c1`, "update audit"), once sweeping up my already-staged amendment files. Resolved by amending the racing commit's message to the correct D-13 subject (`69e68b07`) — the content was exactly my two amendment files. Per-commit explicit-path staging kept every 29-03 code commit clean.
- **Transient `EnsureRoles: tuple concurrently updated (SQLSTATE XX000)`** in the shared-PG skills db_integration suite (a concurrent session ran the role-ensure DDL simultaneously). Confirmed a flake: the failing test (`TestSetAlwaysPersistsFlag`) passes in isolation and the full suite re-ran green. Not a regression in this plan.

## User Setup Required
None - no external service configuration required. (The `governance.write` capability is held implicitly by the seeded `local` identity via its `*` wildcard; `AURA_SKILLS_EXTERNAL_DISCOVERY` is an optional operator toggle, off by default.)

## Next Phase Readiness
- The skills write backend is live: plan 29-04 (the React skills write UI — the install panel with source field + skills.sh catalog search, the RISKY checklist render, the restore/archive controls, the approval-queue deep-link) can call `POST/PATCH/DELETE /api/governance/skills[...]` + `GET .../catalog` and render the `SkillsInstallInfo` (RISKY + five checklist items + approval token).
- Plan 29-05 (the security backstops) can assert the "no model approve" property: the install endpoint stages to pending only, and activation flows exclusively through the operator resume — the held-out backstop should confirm a pending skill is never in the active loader and the operator-origin pause is unreachable without `governance.write`.

## Self-Check: PASSED

All created files present on disk (`governance_write_skills_api.go`, `governance_write_skills_api_test.go`, `serve_governance_write_skills.go`, `serve_governance_write_skills_integration_test.go`, `29-03-SUMMARY.md`) and all three commits present in git history (`69e68b07` docs amendment, `77c8d7cd` Task 2, `229cb9d5` Task 3). `go build ./...` + `go vet ./...` clean; `golangci-lint` 0 issues on the touched packages; unit + `-race` (skills + agui) green; `db_integration` (live PG) skills suite + the install→approval bridge round-trip (`TestSkillsInstallApprovalBridgeAccept`/`Decline`) PASS; the plan's Task-2/Task-3 `<verify>` greps pass (the Task-3 `SetGovernanceWriteProviders`-in-serve_webui.go grep is the one plan imprecision documented in Deviations — the wiring is correctly in serve.go per the 29-02 precedent).

---
*Phase: 29-governance-write-mcp-configuration-skills-install*
*Completed: 2026-06-21*
