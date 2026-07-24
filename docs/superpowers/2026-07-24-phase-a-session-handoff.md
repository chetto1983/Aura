# Session handoff — Phase A (channel-approval consolidation) + dark-code enforcement — continue Monday 2026-07-27

**Date:** 2026-07-24 (Fri) · **Resume:** Mon 2026-07-27
**Branch:** `master` (master-direct). **origin/master == local HEAD == `8ebc07170`** (pushed, in sync).

---

## TL;DR status board

| Workstream | State | Blocker |
|---|---|---|
| **Phase A — runner seam (Tasks 1-2)** | ✅ DONE, committed, **pushed clean** | — |
| **Phase A — Tasks 3-10** (Telegram collapse + enrichment, WebUI Option B, verify+close) | ⏸ PAUSED | the spike + no-commit-possible (see below) |
| **Dark-code enforcement** (CLAUDE.md "DARK CODE IS FORBIDDEN") | ⏸ DEFERRED | spike churn makes `deadcode` unreliable |
| **CI on `8ebc07170`** | 🟡 running at handoff time (CI / Skills / CodeQL) | verify green Monday: `gh run list --limit 3` |
| **The spike (PRD #93.2 adaptive decommission)** | 🔴 in-flight, **140 uncommitted files / 41 deletions** | operator's parallel work — DO NOT TOUCH |

---

## What shipped this session (all on origin/master @ 8ebc07170)

Phase A brainstorm → spec → plan → executed Tasks 1-2:
- `6579fdcc6` design spec — [docs/superpowers/specs/2026-07-24-channel-approval-consolidation-phase-a-design.md](specs/2026-07-24-channel-approval-consolidation-phase-a-design.md)
- `050b3bb59` implementation plan (10 tasks) — [docs/superpowers/plans/2026-07-24-channel-approval-consolidation-phase-a.md](plans/2026-07-24-channel-approval-consolidation-phase-a.md)
- `adad718a5` **Task 1** — `runner.ResolveDirective` + `classifyResolve` + `isScheduledTaskApproval` (`internal/runner/runner_resume_directive.go`). Pure decision fn; verified vet + `golangci-lint ./internal/runner/...` 0 + `-race`.
- `58b3c678a` **Task 2** — `SubmitAnswer` → `(ResolveDirective, error)`, all callers migrated, `SubmitAnswers` unchanged. Verified in a **clean worktree at HEAD**: vet 0 + `-race` PASS (runner+telegram). The main-tree LSP diagnostics were STALE (spike churn) — do not trust them.
- `79a88512a` plan-doc fix (Task 1 test reuses the existing `schedApprovalPending` helper).
- `fc18afdfb` dark-code enforcement handoff — [docs/superpowers/2026-07-24-dark-code-enforcement-handoff.md](2026-07-24-dark-code-enforcement-handoff.md)
- `b512a0021` **CLAUDE.md** rule updates (operator's: "DARK CODE IS FORBIDDEN" + CODE BASE RULES / Zen).
- `8ebc07170` quality-snapshot Telegram-row re-attestation (metric-neutral, for the pre-push gate).

**The runner decision seam is complete and correct.** Tasks 3-10 build on it.

---

## HARD CONSTRAINTS learned this session (read before doing anything Monday)

1. **`--no-verify` is FORBIDDEN — absolute.** Operator revoked all prior standing authorization. Never bypass pre-commit or pre-push hooks. If a hook fails, make it pass legitimately or wait. (Memory: `feedback_no_verify_forbidden`.)
2. **The spike blocks COMMITS, not the push.** Precise mechanics:
   - **pre-commit `golangci-lint`** exits 1 on the spike's `internal/adaptive` revive findings (~13, missing doc-comments) → **any `.go` commit is blocked**; a **docs-only commit** skips lint (glob `*.go`) and runs **only `go vet ./...`**, which currently passes → docs commits CAN land clean.
   - **pre-push `deadcode -test`** is **INFORMATIONAL** — it prints `internal/adaptive` dead-code findings but **exits 0**, so it never blocks a push. (I was wrong earlier that it would.)
   - Net today: could push clean (`8ebc07170`) because docs commits pass vet and the push gates don't fail on the spike.
3. **The main working tree is unbuildable/unreliable standalone** while the spike is mid-flight (deletes 6 packages: `activelearn`, `learningretention`, `reasoninglearn`, `reasoningstore`, `toolselectlearn`, `toolselectstore`; ~140 files). To verify Go changes, use a **clean detached worktree at HEAD**: `git worktree add --detach /d/Aura-verify HEAD` → build/test in WSL there → `git worktree remove /d/Aura-verify --force`.
4. **Environment:** Go runs in **WSL only** (`wsl -e bash -lc 'export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"; cd /mnt/d/Aura && <go cmd>'`); never native `.exe` (AV blocks). Web gates on Windows Git Bash. Stage **explicit paths only** (never `git add -A` — spike's 140 files).

---

## Monday resume — decision tree

**First: is the spike still uncommitted?** `git status --short | wc -l`.

### If the spike has LANDED (tree clean, `git status` shows only your files):
1. Verify clean: WSL `go build ./... && go vet ./... && golangci-lint run $(bash scripts/go_packages.sh)` all green.
2. **Run the dark-code enforcement sweep** (now trustworthy): `deadcode -test $(bash scripts/go_packages.sh)`; per [the dark-code handoff](2026-07-24-dark-code-enforcement-handoff.md), confirm each finding has 0 non-test callers at HEAD before removing; remove dead TYPES not orphan methods. Commit (hooks pass now).
3. **Resume Phase A Task 3** from the plan: `task-brief` Task 3 → dispatch implementer (collapse `aura_hitl`+`aura_sappr` into one Telegram callback, delete `internal/channels/telegram/scheduled_approval.go`). Continue subagent-driven 3→10. Normal commits (no --no-verify).

### If the spike is STILL in-flight:
- **Do NOT resume Phase A code tasks** — the pre-commit lint blocks every `.go` commit and you can't bypass. Phase A Tasks 3-10 require committing Go code → blocked.
- You CAN still land docs-only work (vet-only pre-commit).
- Best use of time: ping the operator on whether to help the spike reach a lint-clean/deadcode-clean state, or wait.

---

## Phase A Tasks 3-10 (the plan, unchanged)

Full detail in [the plan](plans/2026-07-24-channel-approval-consolidation-phase-a.md). Summary:
- **T3** collapse the 2 Telegram callbacks → 1; delete `scheduled_approval.go`; framing-inclusive 64-byte on-wire guard.
- **T4** Telegram renders the directive Outcome (Approved/Rejected → deterministic IT confirmation, no continuation; Continue → startTurn; Pending → next; Terminated → disarm).
- **T5** enrich native approval: multi-row keyboard (Approva/Rifiuta/Dettagli) + ForceReply + formatted secret-safe message.
- **T6** WebUI `/api/approvals/.../resolve` returns the directive (200 JSON) via `SubmitAnswer`.
- **T7** React card consumes `{outcome}` → CardState; re-drive only on `continue`. **T8** rebuild `internal/webui/dist` (Linux node/docker webbuild).
- **T9** db_integration test (scheduled approval → Approved/Rejected + task activation). **T10** gates (coverage ≥85%, mutation on `classifyResolve`), live E2E >9.8 (schedule agent_job from TG + cockpit, identical resolution, no loop/dup), quality-snapshot re-attest, push, merge, CI green.
- **Design note:** WebUI Option B locked; scheduled_task_approval = deterministic outcome, no model turn (research-grounded: codex/ADK "one resolver, transports render", out-of-band = structured data).

---

## Ledger + pointers
- SDD progress ledger (gitignored scratch): `.superpowers/sdd/progress.md` — task-by-task commit log + resume conditions.
- Governing index: [docs/audit/consolidated-fix-plan-2026-07-20.md](../audit/consolidated-fix-plan-2026-07-20.md) (Phase A succeeds Wave 1.7).
- Predecessor handoff: [docs/superpowers/2026-07-24-1.7-session-handoff.md](2026-07-24-1.7-session-handoff.md).
- Follow-up memories: `project_telegram_miniapp_richer_askuser` (Phase A), `feedback_no_verify_forbidden`.
- **MEMORY.md index is near its read limit (19.8KB/24.4KB)** — compact it (one line per entry, move detail to topic files) early Monday.

## Open item at handoff
- CI on `8ebc07170` was still running when this was written. **Monday: confirm green** (`gh run list --limit 3`); if any job is red and the baseline `8ab57a0ef` was green, it's Phase A's / the push's to fix.
