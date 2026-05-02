---
name: aura-agent-teams
description: "Spawn and orchestrate Claude Code Agent Teams for Aura multi-component milestones (backend + frontend + Q&A split). TRIGGER when: the user asks to start, spawn, or run an agent team for Aura; when planning a milestone that touches both Go internals and the React dashboard with QA coverage; when executing Phase 12 (Compounding Memory) or any future Aura phase whose plan declares agent-team execution; when the user mentions 'agent team', 'spawn team', 'teammate', 'team mode', or 'parallel teammates' in this repo. SKIP when: single-file changes, routine bug fixes, sequential refactors, work that doesn't span backend + frontend, anything that fits in one focused subagent or a single session loop. Do not use for code review alone (use gsd-code-reviewer subagent instead). Encodes the file-ownership invariants, model selection policy, spawn protocol, and coordination rules specific to Aura."
metadata:
  version: 1.0.0
  source: https://code.claude.com/docs/en/agent-teams
---

# Aura Agent Teams Orchestration

This skill encodes how to use Claude Code's experimental Agent Teams feature inside the Aura repo. Reference: <https://code.claude.com/docs/en/agent-teams>.

Agent Teams let you spawn 3–5 parallel Claude Code teammates with disjoint file ownership, coordinated via a shared task list. They cost more tokens than a single session but unlock real parallelism for cross-layer work.

---

## When to use (Aura-specific)

Use a team if **all** of these are true:

1. The work spans **at least two of**: Go backend (`internal/`, `cmd/`), React dashboard (`web/`), test/QA artifacts.
2. The plan can be split so each teammate **owns disjoint files** (zero overlap on writes).
3. The total task list is **≥ 10 atomic slices** — below that, coordination overhead beats parallel speedup.
4. There is an **executable plan committed to `docs/plans/`** with explicit slice ownership and dependencies (e.g. the Phase 12 plan).

Concrete fits in this repo:

- **New milestone with backend + frontend + tests** (e.g. Phase 12 Compounding Memory: `docs/plans/2026-05-02-phase-12-compounding-memory-plan.md`).
- **Cross-layer feature** that needs new SQLite schema + new API endpoints + new dashboard route + tests, all parallelizable.
- **Multi-hypothesis debugging** where backend, frontend, and Q&A each test different theories of a regression.

## When NOT to use

Direct from the official docs: *"Agent teams add coordination overhead and use significantly more tokens than a single session. They work best when teammates can operate independently. For sequential tasks, same-file edits, or work with many dependencies, a single session or subagents are more effective."*

In Aura, prefer a single session or a focused subagent for:

- Bug fixes touching one package.
- Refactors that mostly rewrite the same file.
- Adding a single tool to the registry.
- Documentation-only updates.
- Code review (use `gsd-code-reviewer` subagent).
- Single-purpose research (use `Explore` or `general-purpose` subagent).

---

## Team composition pattern (Aura standard)

For full-stack milestones, the canonical split is **Backend / Frontend / Q&A**:

| Teammate | Owns | Never touches |
|----------|------|---------------|
| **Backend** | `internal/`, `cmd/`, SQL migrations, `.env.example` (new keys) | `web/`, `internal/api/dist/` |
| **Frontend** | `web/src/`, regenerates `internal/api/dist/` | `internal/*.go`, `cmd/`, SQL |
| **Q&A** | `*_test.go` in modified packages, `cmd/debug_*/`, `docs/REVIEW.md` | Production code (never patches `internal/*.go` outside test files) |

**Lead** = the orchestrator session (you). Manages task dispatch, integration calls, and architectural escalations. Does NOT write production code unless unblocking the team.

If the work is backend-only (no `web/` changes), use a 2-teammate split (Backend + Q&A) or — better — drop to a single session. 2 teammates is sub-optimal for team mode and rarely justifies the overhead.

### Disjoint file invariants

- Frontend always rebuilds `internal/api/dist/` **last** in any cross-layer slice. Backend lands first, frontend rebuilds.
- Q&A reads production code but never writes outside `*_test.go` and `cmd/debug_*/`. If Q&A finds a bug, it files an issue/comment in the task list for the originating teammate to fix in a follow-up slice.
- Backend that wires a new API endpoint that the frontend needs publishes a "no-rebuild pact": commits the Go code, frontend opens a follow-up slice for the UI.

---

## Model selection policy (Aura standard)

Per the official docs, **teammates do not auto-inherit the lead's model**. You must specify per teammate at spawn time.

Default for Aura:

| Role | Model | Rationale |
|------|-------|-----------|
| Lead | **Sonnet 4.6** | Routing + synthesis + task dispatch; doesn't need Opus tier |
| Backend teammate | **Sonnet 4.6** | Standard Go work — patterns are well-established in `internal/` |
| Frontend teammate | **Sonnet 4.6** | React/TS work follows existing shadcn/Tailwind patterns |
| Q&A teammate | **Sonnet 4.6** | Test writing rarely needs Opus reasoning |
| Final code review pass | **Opus 4.7** (single shot, sub-agent) | High signal, low cost when scoped to the milestone diff |

**Escalation rule**: if a teammate gets blocked by a hard architectural decision (e.g. "do we use a streaming API or polling here?"), the teammate messages the Lead. Lead temporarily switches that teammate to Opus 4.7 (`/model` on that teammate's pane) for that turn, then back to Sonnet.

Cost estimate vs all-Opus 4.7 1M: ~70–80% savings.

---

## Spawn protocol

Prerequisite: `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` must be set in `.claude/settings.json` (env block) or environment, and Claude Code restarted. The flag is auto-modification protected — only the user can flip it. If you (Claude) detect the flag is missing, stop and ask the user to enable it.

Once enabled:

1. **Confirm prerequisites in the lead session**:
   - Plan file exists at `docs/plans/YYYY-MM-DD-<phase>-plan.md` and is committed.
   - Working tree clean (`git status` empty).
   - All test commands documented in the plan.

2. **Compose the spawn request**:
   ```text
   Create an agent team to execute Phase 12 (Compounding Memory) per
   docs/plans/2026-05-02-phase-12-compounding-memory-plan.md.

   Spawn 3 teammates, all on Sonnet 4.6:
   - Backend: owns internal/conversation/, internal/scheduler/,
     internal/api/conversations*.go, summaries*.go, maintenance*.go,
     internal/telegram/bot.go (hook wiring), SQL migrations, .env.example.
     Slices: 12a-12i.
   - Frontend: owns web/src/components/Conversations*.tsx, Summaries*.tsx,
     Maintenance*.tsx, web/src/api.ts, types/api.ts, Sidebar.tsx, Shell.tsx,
     App.tsx. Regenerates internal/api/dist/. Slices: 12j-12n.
   - Q&A: owns *_test.go in modified packages, cmd/debug_summarizer/,
     docs/REVIEW.md. Slices: 12o-12u.

   Each teammate must read the plan first, claim slices in dependency
   order from the task list, follow the project's atomic-commit rule
   (one commit per slice, stage explicit paths), and run the verification
   commands in each slice before committing.

   Lead remains in this session for dispatch + integration. Final slice
   12u spawns a single Opus 4.7 sub-session for the code review pass.
   ```

3. **Use the `TeamCreate` tool** (loaded via `ToolSearch` with `query: "select:TeamCreate,SendMessage,TeamDelete"`).

4. **Watch the task list**: `~/.claude/tasks/{team-name}/` is the shared state. Inspect with `Read` to detect stuck tasks.

---

## Coordination rules

- **Frequent commits**: each slice is one atomic commit. Teammates stage explicit paths; never `git add -A` (per project memory `feedback_atomic_commits_per_slice.md`).
- **Tests pass before commit**: `go test ./...` and (for concurrency-touching slices) `go test -race`. Frontend slices: `npm run lint && npx tsc --noEmit && vite build`.
- **Sync `.env.example` and `.env`**: any new config key must land in both (per memory `feedback_env_sync.md`). `.env.example` is tracked; `.env` is gitignored runtime — backend teammate updates both, but only stages `.env.example`.
- **Reference Picobot + llm-wiki patterns**: when uncertain, check `D:\tmp\picobot` and `D:\Aura\docs\llm-wiki.md` before writing new code (per memory `feedback_picobot_wiki_reference.md`).
- **Use `Body`, not `Content`** for wiki pages. Wiki links: `[[slug]]` form.
- **Embedding key separation**: `EMBEDDING_API_KEY` for Mistral embeddings is dedicated; never fall back to `LLM_API_KEY`.
- **Frontend rebuild commit**: includes both `web/` source changes AND `internal/api/dist/` rebuilt artifacts. Single commit, never split.

---

## Lead responsibilities during execution

1. **Daily standup-style task list inspection**: read `~/.claude/tasks/{team-name}/` once per work session. Detect:
   - Stuck tasks (in-progress > 1 hour without commit).
   - Dependency blockers (claimed but waiting on upstream slice not yet merged).
   - Conflict zone violations (a teammate touched files outside their ownership).

2. **Architectural escalations**: when a teammate messages "blocked on decision X":
   - Quick decisions: answer inline.
   - Hard decisions: switch that teammate to Opus 4.7 for the turn, or pause and brainstorm with the user.

3. **Integration calls**: when a slice depends on another teammate's work (e.g. frontend slice 12j depends on backend 12c), make the integration verification call in the lead session before dispatching the dependent slice.

4. **Final reconciliation**: after all slices land, the lead updates `prd.md`, `tasks/prd-aura.md`, and `docs/implementation-tracker.md` with the milestone summary (one or two atomic commits owned by lead). Then tags release.

---

## Shutdown

When milestone is done:

1. Verify all slices committed and tests green.
2. Spawn the final Opus 4.7 code review pass (slice 12u in Phase 12).
3. Apply any high-severity REVIEW.md findings as 12u.N follow-up commits (assigned to appropriate teammate).
4. Lead asks the team to shut down gracefully.
5. Clean up team config: delete `~/.claude/teams/{team-name}/` and `~/.claude/tasks/{team-name}/` (per official docs; cleanup must run from lead only).

---

## Reference: canonical example (Phase 12)

- Design: `docs/plans/2026-05-02-phase-12-compounding-memory-design.md`
- Plan: `docs/plans/2026-05-02-phase-12-compounding-memory-plan.md`
- Team shape: 3 teammates (Backend / Frontend / Q&A), all Sonnet 4.6.
- 21 slices: 12a–12u.
- Final review: Opus 4.7 sub-agent → `docs/REVIEW.md`.

When the user says "spawn the team for Phase 12" (or similar), follow the plan §Team Composition and the spawn protocol above.

---

## Anti-patterns to surface to the user

If the user asks to spawn a team but the work fails the "When to use" criteria, push back. Examples:

- *"Spawn a team to fix this bug in `bot.go`"* → No, single-file edit. Use a focused subagent or just edit directly.
- *"Spawn a team to add a tool to the registry"* → No, single sequential change. Single session.
- *"Spawn a team to refactor `internal/conversation/context.go`"* → No, same-file conflict guaranteed. Single session with `Plan` subagent for design.

When in doubt, ask: "is this work parallelizable into ≥3 disjoint file scopes, with ≥10 atomic slices, planned in `docs/plans/`?" If no on any of these, recommend a lighter approach.
