---
phase: 2
slug: llm-reliability-tool-intelligence
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-05-10
updated: 2026-05-11
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib) |
| **Config file** | none — Go testing is built in |
| **Quick run command** | `go test ./internal/{llm,wiki,tools,reindex}/ -count=1` |
| **Full suite command** | `go test -race ./...` |
| **Estimated runtime** | ~120 seconds full suite, ~15 seconds quick |

---

## Sampling Rate

- **After every task commit:** Run `go vet ./... && go build ./... && go test ./internal/<changed-package>/`
- **After every plan wave:** Run `go test -race ./internal/{llm,wiki,tools,reindex,agentloop,telegram}/`
- **Before `/gsd-verify-work`:** Full suite (`go test -race ./...`) must be green
- **Max feedback latency:** ~30 seconds for quick / ~120 seconds for full

---

## Per-Task Verification Map

> INFO 9 of 2026-05-11 plan revision 2: per-task verification is **delegated** to each PLAN.md's `<verify>` element.
> See `.planning/phases/02-llm-reliability-tool-intelligence/02-*-PLAN.md` for the authoritative test commands.
> This file delegates per-task validation to the plans; sign-off occurs after Wave 4 (Plan 08) green-light.
>
> Why delegate rather than enumerate here? Each plan already pins its `<verify><automated>` block to a
> concrete `go test -run <Test> ./internal/<pkg>/` invocation, so duplicating the mapping here would
> drift the moment a plan is revised. The Wave 0 fixture list (below) covers what needs to exist
> before substantive tasks run; per-task green-light is checked at the plan boundary.

| Task ID | Plan | Wave | Verification Source |
|---------|------|------|---------------------|
| — | all | 1–4 | See `<verify><automated>` element in each `02-*-PLAN.md`. Per-task green-light delegated to the plan files; this validation document signs off after the Wave 4 close-out smoke in Plan 08 Task 4. |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Based on RESEARCH.md Validation Architecture — files/fixtures the planner must create before substantive tasks. Each item below is owned by a specific plan and its Task 0 / first task:

- [ ] `internal/llm/classify_test.go` — retry classification table (content vs transport errors) — **Plan 01**
- [ ] `internal/reindex/worker_test.go` — buffered-channel coalescing + race semantics — **Plan 02**
- [ ] `internal/wiki/store_writes_test.go` — expected_updated_at conflict detection + git commit fallback — **Plan 03**
- [ ] `internal/tools/wiki_test.go` — write_wiki_page tool schema validation + conflict surface — **Plan 04**
- [ ] `internal/telegram/tools_provider_test.go` — per-turn ToolsProvider closure regression suite (renamed from conversation_test.go in 2026-05-11 plan revision 2 because the helper moved to a sibling file per WARNING 4) — **Plan 06**
- [ ] `internal/wiki/godclass_test.go` — Go test enforcing CLAUDE.md ≤600 LOC rule — **Plan 08 Task 1**
- [ ] `internal/tools/testdata/retrieval_fixture.jsonl` — Tool-vector collection rebuild fixture (15 hand-labeled rows; expected_tool values verified live during 2026-05-11 plan revision 2 per WARNING 6) — **Plan 08 Task 2**

*If none of these end up needed at planning time, RESEARCH.md's Wave 0 list (17 fixtures) supersedes — planner reconciles in PLAN.md frontmatter.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Telegram-side end-to-end: LLM writes wiki page → dashboard shows new page | WIKI-01 | Requires live Telegram bot + LLM endpoint, not amenable to CI | Send message asking bot to remember a fact; open dashboard `/wiki`; verify page exists with frontmatter |
| Dashboard manual edit during active conversation — verify conflict surface | WIKI-02 | Requires concurrent dashboard + Telegram session timing | Start Telegram conversation; edit page in dashboard; trigger LLM write; verify conflict error surfaces to chat |
| `unversioned: true` frontmatter visible in dashboard when git commit fails | GIT-01 | Requires simulating git failure (e.g., read-only worktree) | Make wiki dir read-only; trigger LLM wiki write; check page frontmatter `unversioned: true` rendered in dashboard |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies — verified at the plan level (each `02-*-PLAN.md` contains a `<verify><automated>` element on every task)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify — verified at the plan level
- [x] Wave 0 covers all MISSING references — see Wave 0 Requirements section above; each fixture is owned by a specific plan's first task
- [x] No watch-mode flags — all `<verify>` commands are one-shot `go test ... -count=1` invocations
- [x] Feedback latency < 30s for quick / < 120s for full
- [x] `nyquist_compliant: true` set in frontmatter (INFO 9 of 2026-05-11 plan revision 2: flipped because each plan already has its own `<verify>` block and Wave 0 fixtures are pre-listed above)

**Approval:** pending Wave 4 (Plan 08 Task 4) full-suite green-light per the delegation note above
