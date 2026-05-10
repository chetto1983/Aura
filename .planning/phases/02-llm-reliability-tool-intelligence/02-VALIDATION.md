---
phase: 2
slug: llm-reliability-tool-intelligence
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-10
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

> Populated by gsd-planner — tasks from PLAN.md should map here with concrete test commands.
> The planner MUST provide a `<test>` block per task; this table is generated downstream from those.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD | TBD | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Based on RESEARCH.md Validation Architecture — files/fixtures the planner must create before substantive tasks:

- [ ] `internal/llm/classify_test.go` — retry classification table (content vs transport errors)
- [ ] `internal/reindex/worker_test.go` — buffered-channel coalescing + race semantics
- [ ] `internal/wiki/store_writes_test.go` — expected_updated_at conflict detection + git commit fallback
- [ ] `internal/tools/wiki_test.go` — write_wiki_page tool schema validation + conflict surface
- [ ] `internal/telegram/conversation_test.go` — per-turn ToolsProvider closure regression suite
- [ ] `scripts/test-godclass-guard.go` — CI gate enforcing CLAUDE.md ≤600 LOC rule
- [ ] Tool-vector collection rebuild fixture (Qdrant warm-cache invalidation)

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

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s for quick / < 120s for full
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
