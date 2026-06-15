---
phase: 11-skills
plan: 05
subsystem: skills
tags: [skills, slice-7c, governance, ask-user-resume, messages-1-always-block, d-03-no-model-approve, d-07-always-block, pitfall-3-l25-protection, cap-04-byte-stable, aura-skills-cli]

requires:
  - phase: 11-02
    provides: "skill tool ActionRouter (reserved create/update/delete keys) + skillLoader consumer seam + loader.List()/always filter source"
  - phase: 11-03
    provides: "ValidateForWrite (model path allowBlocklisted=false hard-reject) + SanitizeName chokepoint + AURA_SKILL_INJECTION_BLOCKLIST"
  - phase: 11-04
    provides: "Writer.WriteMutation/Activate/Archive/Delete + AuditStore + Materialize + content_hash + migration 0010 append-only audit (D-29 matrix)"
  - phase: 04-03
    provides: "ask_user *ErrAwaitingUserInput pause sentinel + askuser.Action{Accept,Decline,Cancel} + Runner pause/resume orchestration"
  - phase: 04-04
    provides: "conversations L1/L2/L2.5 ladder (dropOldestPairs/applyL1, seq=1 system L0 preservation) + ContextConfig"
  - phase: 08
    provides: "scoring.ComputeSkillTier/GateRecommended (create/update/install=Risky, delete=Destructive)"
provides:
  - "internal/agent/tools/skill_write.go: create/update/delete actions — validate (via writer seam) -> gate -> pending -> *ErrAwaitingUserInput pause (D-02); blocklist hit = tool error (self-correct, NOT a pause, D-27); optional headless Alerter (D-26); NO model-facing approve key (D-03)"
  - "internal/agent/tools skillWriter + skillAlerter consumer seams (tools stays free of internal/skills — boundary held)"
  - "internal/skills/resume.go: ResumeHandler (D-03 activation channel) — accept -> Writer.Activate(ask_user); decline/cancel -> DiscardPending + rejection audit; Writer.DiscardPending"
  - "internal/skills/messages.go: RenderAlwaysBlock — always:true bodies into ONE byte-stable, alphabetical user-role messages[1] block under a frozen English header (D-07/CAP-04)"
  - "internal/conversations/context.go: ContextConfig.AlwaysBlock + ladder injects the always-block as a PROTECTED seq=2 turn after system L0; L1/L2.5 never evict it (Pitfall 3); marker stripped on the wire"
  - "internal/runner/runner.go: Deps.AlwaysBlock per-turn renderer threaded into contextConfig()"
  - "cmd/aura/skills.go: aura skills {list|info|create|update|delete|approve|audit} — operator activation channel (CLI approval source, D-03) + append-only purge-denied surface (SC#2)"
  - "cmd/aura wiring: skillWriterAdapter (model actor) on the skill tool Writer + alwaysBlockProvider over the live loader"
affects: [11-06, 11-07, "skill-install", "skill-snippets", "phase-14-agent-md-messages1-seam"]

tech-stack:
  added: []
  patterns:
    - "Model self-extension loop closed structurally: WriteMutation lands pending + pauses; activation is a SEPARATE method only the resume handler / CLI reach — no model-facing approve action exists (D-03, structurally + test-enforced)"
    - "ask_user resume as the human activation channel: accept routes to Writer.Activate, decline/cancel to DiscardPending — the agent's existing pause/resume sentinel carries it, no new pause machinery"
    - "messages[1] always-block as a PROTECTED ladder turn (marker in ToolCallID, never seq+role alone) injected after system L0; counted toward the budget, never evicted — the seq=2 user-role turn that Phase-14 Agent.md will share"
    - "Consumer-declared write seam (skillWriter) keeps internal/agent/tools cycle-free of internal/skills — the live Writer is adapted at the composition root (mirrors the 11-02 read seam)"

key-files:
  created:
    - internal/agent/tools/skill_write.go
    - internal/agent/tools/skill_write_test.go
    - internal/skills/resume.go
    - internal/skills/resume_test.go
    - internal/skills/resume_integration_test.go
    - internal/skills/messages.go
    - internal/skills/messages_test.go
    - internal/conversations/context_alwaysblock_test.go
    - cmd/aura/skills.go
  modified:
    - internal/agent/tools/skill.go
    - internal/skills/writer.go
    - internal/skills/writer_activate.go
    - internal/conversations/context.go
    - internal/runner/runner.go
    - cmd/aura/main.go
    - cmd/aura/chat.go
    - cmd/aura/serve_adapters.go

key-decisions:
  - "Rejection audit row uses the cleanup_pending_stale action with the gate_taken=true tuple implied by src (ask_user => NOT-NULL token; cli => NULL token): the D-29 CHECK has NO distinct reject tuple (approve+reject share the ask_user shape, 11-04 decision), so a human-declined pending is recorded as cleanup_pending_stale + the gate-taken shape, NOT gate_taken=false"
  - "The always-block is injected INTO the conversations ladder (ContextConfig.AlwaysBlock) as a protected seq=2 turn, not bolted on after — so it is counted toward the L2 budget AND protected by L2.5 (Pitfall 3 is meaningful), and the agent's existing stripLeadingSystem + own-system-prepend yields messages[0]=system, messages[1]=always with no double-injection"
  - "isAlwaysBlock keys off a private marker in the Turn.ToolCallID field (a field a real persisted user turn never populates), NOT seq==2+RoleUser alone — the latter collides with ordinary persisted user turns at seq=2 (caught by the existing dropOldestPairs/L25 boundary tests)"
  - "Headless D-26 = structural: WriteMutation never activates (Activate is separate), so a swarm/cron mutation stays pending forever and can never self-activate; the IMMEDIATE Notifier alert is an OPTIONAL injectable Alerter seam (nil in the REPL), NOT a hard internal/cron import into the tools package (boundary discipline over the 'reuse Notifier AS-IS' wording)"
  - "WriteMutationByName/WriteMutationCLI string-keyed convenience on Writer so the CLI and the model-tool adapter share one entry without constructing Frontmatter/scoring.SkillAction outside internal/skills"

patterns-established:
  - "Protected ladder turn beyond system L0: a marker-tagged synthetic turn injected by the ladder and preserved by both L1 (role-gated) and L2.5 (head-split extended) — the reusable mechanism for any always-on messages[1] fragment (Phase-14 Agent.md)"
  - "Human-only activation channel: a model proposal pauses; a human resume or operator CLI is the SOLE activation path; the audit ledger records both source and gate-taken tuple"

requirements-completed: []

duration: ~50min
completed: 2026-06-05
---

# Phase 11 Plan 05: Skills Governance — Write Actions + ask_user Approval + messages[1] Always-Block (Slice 7c) Summary

**Closes the model-facing self-extension loop (gated, human-approved) and lands the always-on injection point without breaking CAP-04: the skill tool's create/update/delete actions validate at the write boundary (NFKC+blocklist HARD-reject for the model, D-27), gate via scoring, land in pending/, and PAUSE via ask_user (D-02) — there is NO model-facing approve (D-03); activation is the ask_user resume (skills.ResumeHandler -> Writer.Activate) or the `aura skills approve` CLI. The `always:true` bodies render into ONE byte-stable user-role block at messages[1] (D-07) injected into the conversations ladder as a PROTECTED turn the L1/L2.5 evictor never drops (Pitfall 3), while messages[0] stays byte-identical (CAP-04, cache_invariant_audit.sh still green).**

## Performance

- **Duration:** ~50 min
- **Completed:** 2026-06-05
- **Tasks:** 2 (both autonomous)
- **Files:** 17 (9 created, 8 modified)

## Accomplishments

### Task 1 — Write actions + ask_user pause + resume activation + `aura skills` CLI (`0b00e2c2`)

- **`skill_write.go`** — the `actionCreate`/`actionUpdate`/`actionDelete` handlers replace the 11-02 "not yet wired" placeholders. Each decodes its args, requires a writer, calls the consumer-declared **`skillWriter`** seam's `WriteMutation` (which validates at the boundary with `allowBlocklisted=false` — model paths NEVER bypass the blocklist), and on a successful pending write returns the **`*ErrAwaitingUserInput`** sentinel so the agent pauses the turn for human approval (the question frames the gated mutation + its tier; delete carries the higher Destructive priority). A blocklist/validation failure comes back from the writer as a plain error and is surfaced as a **tool error (self-correct), NOT a pause** (D-27). An optional **`skillAlerter`** seam fires the headless alert (D-26).
- **No model-facing approve (D-03):** the router gained `create`/`update`/`delete` but NO `approve` key — `TestNoApproveAction` proves `action=approve` is rejected as unknown. The boundary held: `go list -deps ./internal/agent/tools` has **0** `internal/skills`.
- **`resume.go`** — `ResumeHandler.Resume` is the D-03 activation channel: **accept** → `Writer.Activate(ApprovalAskUser, pausedToken)` (the ask_user approved audit row); **decline/cancel** → `Writer.DiscardPending` (removes the pending dir + records the rejection audit row). `DiscardPending` is the new Writer method.
- **`cmd/aura/skills.go`** — hand-rolled `runSkills` switch (mirrors `runIdentity`/`runTask`, NO cobra): `list`, `info`, `create`, `update`, `delete`, `approve <name>` (operator activation → `Writer.Activate` with `ApprovalCLI`), `audit [--skill --since]` (lists the append-only ledger), and `audit purge` which **surfaces the role error** (aura_app holds SELECT+INSERT only — the DELETE is denied; SC#2). Registered in `main.go`.

### Task 2 — messages[1] always-block render + bootChat assembly + L2.5 protection (`68a3cd01`)

- **`messages.go`** — `RenderAlwaysBlock` filters `always:true` skills, sorts them alphabetically by name, and concatenates their bodies under ONE frozen English header (`"Active skill instructions (always-on):\n\n"`). Two calls over the same set are byte-identical (CAP-04); `present=false` (empty block) when no always skill exists.
- **`conversations/context.go`** — `ContextConfig.AlwaysBlock` carries the rendered block; `injectAlwaysBlock` inserts it as a PROTECTED `seq=2` user-role turn immediately after the system L0 turn (or at the head when no persisted system turn leads). `applyL1` already skips it (role-gated — it is RoleUser, not RoleTool); `dropOldestPairs` was extended to split the always-block into the protected head alongside the `seq=1` system turn, so **neither L1 nor L2.5 ever evicts it** (Pitfall 3). The protection marker lives in `Turn.ToolCallID` (a field a real persisted user turn never populates — keying off `seq+role` alone collided with ordinary user turns at seq=2) and is stripped in `toMessages` so the wire message is a clean user message.
- **`runner.go`** — `Deps.AlwaysBlock func() string` renders the block per turn from current loader state; `contextConfig()` threads it. **`cmd/aura/serve_adapters.go`** `alwaysBlockProvider` builds a loader over the active skills dir and renders via `skills.RenderAlwaysBlock` on each call (goroutine-free lazy TTL re-scan); `bootChatEnv` wires it. A skill add/remove changes messages[1] but never messages[0].

## Task Commits

1. **Task 1: skill write actions + ask_user pause + resume activation + aura skills CLI** — `0b00e2c2` (feat)
2. **Task 2: messages[1] always-block render + bootChat assembly + L2.5 protection** — `68a3cd01` (feat)

## Decisions Made

- **Rejection audit row = `cleanup_pending_stale` + the gate-taken tuple** — the D-29 CHECK shares the `ask_user` shape for approve AND reject (11-04 decision: the approve/reject distinction lives in the resume answer, not the tuple), and there is no `reject` action enum. A human-declined pending is therefore recorded as `cleanup_pending_stale` with `(ask_user, NOT-NULL token, gate_recommended=true, gate_taken=true)` — the gate WAS exercised (the human said no). Both DB CHECKs (action enum + coherence) accept this shape.
- **Always-block injected INTO the ladder, not bolted on after** — so it is counted toward the L2 budget AND protected by L2.5 (the Pitfall 3 regression is meaningful), and the agent's existing `stripLeadingSystem` + own-system-prepend produces messages[0]=system / messages[1]=always with no double-injection.
- **Protection marker in `ToolCallID`, not seq+role** — keying `isAlwaysBlock` off `seq==2 && RoleUser` alone wrongly protected ordinary persisted user turns at seq=2 (caught immediately by the existing `dropOldestPairs`/L2.5 boundary tests). The private `__aura_always_block__` marker in `ToolCallID` is collision-free and stripped on the wire.
- **Headless D-26 is structural, not a cron-Notifier import** — `WriteMutation` never activates, so a swarm/cron mutation stays pending forever and can never self-activate; the immediate alert is an OPTIONAL injectable `skillAlerter` seam (nil in the REPL), keeping `internal/agent/tools` free of `internal/cron` (boundary discipline over the plan's "reuse Notifier AS-IS" wording — the hard D-26 guarantee is met structurally).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] isAlwaysBlock seq+role predicate collided with persisted user turns**
- **Found during:** Task 2 (first `go test ./internal/conversations/` run).
- **Issue:** the first draft identified the always-block by `seq==alwaysBlockSeq (2) && RoleUser`. Three existing ladder boundary tests build histories with an ordinary `Seq:2, Role:user` turn, which the predicate wrongly protected — breaking `TestDropOldestPairs_*` and `TestLadder_L25Termination_ExactFit`.
- **Fix:** tagged the synthetic always-block with a private `__aura_always_block__` marker in its `ToolCallID` field (a field a real persisted user turn never populates) and keyed `isAlwaysBlock` off that; stripped the marker in `toMessages` so the wire message stays a clean user message.
- **Files modified:** internal/conversations/context.go.
- **Verification:** all `internal/conversations` unit + boundary tests green; the new 20-turn regression passes.
- **Committed in:** `68a3cd01` (Task 2).

**2. [Rule 3 - Blocking] staticcheck QF1001 (De Morgan) on the alphabetical-order assertion**
- **Found during:** Task 2 (golangci-lint Gate-2 pass).
- **Issue:** `if !(idxA < idxM && idxM < idxZ)` tripped QF1001.
- **Fix:** rewrote as `if idxA >= idxM || idxM >= idxZ`.
- **Committed in:** `68a3cd01` (Task 2).

**Total deviations:** 2 auto-fixed (both blocking, both Rule 3). No scope creep — the governance + always-block behavior is exactly as planned. The plan's "reuse the Phase-10 composite Notifier AS-IS" for the headless alert was honored as an injectable seam rather than a hard cross-package import, to preserve the tools-package boundary; the D-26 never-self-activate guarantee is met structurally (see Decisions).

## Verification Evidence

- **Unit (race, WSL):** `go test -race ./internal/agent/tools/ ./internal/skills/ ./internal/conversations/ ./internal/agent/ ./internal/runner/` → **PASS** (all packages, race-clean).
- **`go build ./...` → exit 0; `go vet ./...` → 0; full `go test ./...` → 0 failures.**
- **`golangci-lint run ./internal/agent/tools/... ./internal/skills/... ./internal/conversations/... ./internal/runner/... ./cmd/aura/...` → 0 issues.**
- **D-03 structural:** `grep -c '"approve"'` of the skill router keys → 0 (no model-facing approve action key); `TestNoApproveAction` proves `action=approve` is rejected as unknown.
- **Boundary held:** `go list -deps ./internal/agent/tools | grep -c internal/skills` → **0** (cycle-free).
- **Blocklist self-correct:** `TestActionCreateBlocklistedIsToolError` asserts a blocklisted model body returns a tool error, NOT a pause.
- **CAP-04 byte-stable:** `bash scripts/cache_invariant_audit.sh` → **ok** (22 identical messages[0] request hashes); `TestAlwaysBlockSurvivesL25Reduction` asserts the messages[0] SHA is unchanged across a 20-turn L2.5 reduction.
- **Pitfall 3 regression:** `TestAlwaysBlockSurvivesL25Reduction` (21 turns, tight hard cap forcing L2.5) — the always-block survives the reduction AND a rot event was written (L2.5 fired).
- **db_integration compiles** (skills + conversations tagged build, exit 0): the resume accept/decline audit-row assertions (`TestResumeAcceptActivates`/`TestResumeDeclineDiscards`) are authored under `db_integration` and run against the live stack (Postgres through 0010) in the CI/WSL tier — no-skip-as-green via the shared `envOrSkip`.
- **All touched files ≤600 LOC** (largest: cmd/aura/skills.go 278; internal/conversations/context.go 297).
- **No file deletions** in either commit.

## Known Stubs

None goal-blocking for 7c governance. The headless `skillAlerter` is left nil in the REPL (the ask_user pause is the channel there) — a non-nil composite Notifier adapter is wired by the operator-facing serve/cron path in a later plan; the D-26 never-self-activate guarantee does NOT depend on the alert (it is structural). The `install`/`catalog`/`restore`/`archive` skill actions remain `notYetWired` (11-06/11-07). The Runner does not yet route a resolved ask_user answer to `skills.ResumeHandler` automatically — the handler + the `aura skills approve` CLI are both wired and tested; the Runner's auto-dispatch of a skill-approval resume to the handler is the remaining integration the install/snippets waves consume (interface-first ordering, not a stub: the activation contract is shipped + tested).

## Threat Flags

None — no new security surface beyond the plan's `<threat_model>`. T-11-05-E1 (model self-approve) is structurally closed (no approve router key, tested) + E2 (headless self-activate) is structural (WriteMutation never activates) + T1 (blocklist injection) is the writer's `allowBlocklisted=false` hard-reject (tested as a tool error) + D1 (always-block evicted) is the L2.5 protection (20-turn regression) + I1 (messages[0] cache bust) is the byte-stable separate-turn design (cache_invariant_audit.sh green).

## Next Phase Readiness

- The `skillWriter`/`skillAlerter` tool seams + `ResumeHandler` + `RenderAlwaysBlock` + `ContextConfig.AlwaysBlock` are the contracts 11-06 (installer → clone + `WriteMutation`/`Activate`) and 11-07 (snippets → materialized executable files under the `/skills` mount + the `skill_ttl_sweep` cron) build on.
- The messages[1] always-block is the **Phase-14 Agent.md seam** (profile first, always-skills after — the ordering hook is documented in `messages.go` and the ladder injection point).
- CAP-07 is NOT marked complete — the 7d install (wave 6) is still pending; only the traceability wording is updated.

## Self-Check: PASSED

- FOUND: internal/agent/tools/skill_write.go, skill_write_test.go
- FOUND: internal/skills/resume.go, resume_test.go, resume_integration_test.go, messages.go, messages_test.go
- FOUND: internal/conversations/context_alwaysblock_test.go
- FOUND: cmd/aura/skills.go
- FOUND: commit `0b00e2c2` (Task 1)
- FOUND: commit `68a3cd01` (Task 2)

---
*Phase: 11-skills*
*Completed: 2026-06-05*
