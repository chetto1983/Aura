# REVIEW-2: Domain invariants + planning-state alignment

**Date:** 2026-05-14
**Reviewer:** Domain + Planning
**PRD version reviewed:** v2 (`docs/aura-restructure-prd.md`, 960 lines)
**Verdict:** REVISE_AND_RESUBMIT

## Summary

The PRD honors every hard memory invariant — wiki untouched, no graph DB, embeddinggemma locked, CPU budget unaffected, master-direct workflow respected, regex-on-NLP avoided, artifact-verification probes called for in G13. However, it is **silent on the four `.planning/` waves that the user has already invested in**, and that silence is the highest-severity gap from this angle: Wave 3 agent-swarm explicitly extends `agent.Runner.PhaseSink` and bumps `ChildMaxIterations` in files the PRD proposes to delete or move, Wave 2 graph-memory work targets paths the PRD relocates to `storage/`, Wave 1 has items already shipped that the PRD doesn't acknowledge, and the Context-Engineering Roadmap's Fase 5 explicitly modifies `agent.Task.MaxReturnChars` — a struct the PRD deletes wholesale. The single most important fix: §11 must add a "Planning-state reconciliation" decision that explicitly says whether the four waves are SUPERSEDED, DEFERRED-resume-after-restructure, or INTERLEAVED, with per-wave landing paths in the new layout.

## Findings

### F1 — BLOCKER: PRD never reconciles with the four `.planning/` waves

**Where:** PRD entire document // `.planning/CONTEXT-ENGINEERING-ROADMAP.md`, `.planning/wave1/fix_plan.md`, `.planning/wave2/fix_plan.md`, `.planning/wave3-agent-swarm/plan.md`

**Issue:** The PRD's §11 "Decisioni prese" enumerates 9 internal restructure decisions but does NOT address the four pre-existing `.planning/` documents that drive ongoing work. The Context-Engineering Roadmap (Fase 1–5, 10–17 days), Wave 1 tool-surface (partly shipped, see F4), Wave 2 graph-memory writers (~1–2 weeks, not started), and Wave 3 agent-swarm (~1–2 weeks, early-stage) all assume code paths the PRD demolishes or relocates. The PRD's §3 non-obiettivi mention 12 things explicitly out of scope, but the four waves are not among them. An executor reading this PRD has no way to tell whether to pause Wave 3.x, replan its landing sites, or fold its work into restructure commits.

**Fix for v3:** Add a new §3.1 "Planning-state reconciliation" with one row per wave (Context-Eng / Wave 1 / Wave 2 / Wave 3-agent-swarm) and the explicit decision per wave: SUPERSEDED | DEFERRED-resume-after-restructure | INTERLEAVED with named restructure commit. For SUPERSEDED, link the file in `.planning/` and state in the PRD what scope-coverage is preserved by the restructure. For DEFERRED, state which restructure commit unblocks each wave. For INTERLEAVED, name the commit number where the wave's work lands.

---

### F2 — BLOCKER: Wave 3 (agent-swarm) and Commit 2.1 (kill `agent.Runner`) collide head-on

**Where:** PRD §6 Commit 2.1 lines 622–665 (delete `internal/agent/runner.go`) // `.planning/wave3-agent-swarm/plan.md` Task 3.1 lines 28–34 (add `PhaseSink` interface to `internal/agent/runner.go`); Task 3.3 lines 137–139 (raise `ChildMaxIterations` clamp ceiling in `delegation_policy.go`, bump `roleMaxToolCalls` in `swarmtools/tools.go`)

**Issue:** Wave 3 plan as written cannot be executed against post-restructure code. Three concrete collisions:

1. Task 3.1 declares "define `PhaseSink` interface" in `internal/agent/runner.go` and "wire `PhaseSink` to `store.UpdateTaskPhase` on every transition" — the very file Commit 2.1 deletes. Verified: `D:\Aura\internal\agent\runner.go` lines 39–50 declare `Runner struct`; the file still exists today at 555 LOC.
2. Task 3.1 also requires `internal/swarm/manager.go` to wire `PhaseSink` and `internal/swarmtools/tools.go:243-329` (`ListSwarmTasksTool`, `ReadSwarmResultTool`) to extend `taskSummary` JSON shape with `phase`. After Commit 3.2 (merge tools), `swarmtools/` lives under `internal/agent/tools/swarmtools/` — Wave 3's file paths become stale.
3. Task 3.3 explicitly bumps `ChildMaxIterations` ceiling from 3 to 6 in `delegation_policy.go:74-76` and `roleMaxToolCalls` in `swarmtools/tools.go:427-438`. After Commit 6.1 swarm dispatches through `chat.Hub` with `Channel=swarm`, the iteration ceiling is no longer enforced at the same boundary — it's gated by `agent.Run` clamps reached via `agentruntime.Invocation`. Wave 3 plan is silent on this re-routing.

**Fix for v3:** Either (a) declare in §3.1 that Wave 3 is SUPERSEDED and re-author the plan onto `agent.Run` + `chat.Hub` (then archive `.planning/wave3-agent-swarm/plan.md` with a note), or (b) declare Wave 3 INTERLEAVED before Commit 2.1 with the PhaseSink + propose_patch landing as-is, then defer restructure of swarm/runner-kill until Wave 3 ships. Choose one — but the choice must be visible in §11.

---

### F3 — BLOCKER: Context-Engineering Roadmap Fase 5 modifies `agent.Task` — which the PRD deletes

**Where:** PRD §6 Commit 2.1 (deletes `agent.Task` along with the `Runner`) // `.planning/CONTEXT-ENGINEERING-ROADMAP.md` Fase 5 Task 5.1 line 393 ("add `MaxReturnChars int` to `agent.Task`"), Task 5.2 lines 397–402 ("consolidate `agentloop.finalizeAnswerAfterBudget` callers including `Runner.Run`"), Task 5.3 lines 405–417 (`Result.ArtifactPath` extension to `agent.Result`)

**Issue:** Fase 5 wants to alter `agent.Task` (line 393), `agent.Result` (line 408), and `Runner.Run` callers (line 399). The PRD's Commit 2.1 maps `Task` fields into `agent.Invocation` / `agentloop.Options` (mapping table lines 633–658) and **does not include** `MaxReturnChars` or `ArtifactPath` in that mapping table. After Commit 2.1 these Fase-5 fields have no home. The PRD's effort budget (~110h) does not contemplate this addition.

**Fix for v3:** Add `MaxReturnChars` and `ArtifactPath` (or their post-restructure equivalents) to the §6 Commit 2.1 mapping table — either as accepted-in-restructure scope, or with an explicit forward-pointer "deferred to post-restructure follow-up". Both options require a sentence in §3.1 stating whether Fase 5 is SUPERSEDED, DEFERRED, or INTERLEAVED.

---

### F4 — MAJOR: Wave 1 ship-vs-pending state not acknowledged

**Where:** PRD §6 (no mention of Wave 1) // `.planning/wave1/fix_plan.md` (5 tasks, status `☐` in file) // memory `project_open_gaps_2026-05-13.md` lines 12–17 (Wave 1.7 cleanup wave shipped 2026-05-13: BuildVectorIndex delete commit `a4ebe141`, COST_PER_TOKEN drop `4ad48390`, skill Invalidate `a1347a46`, `5d92dc76` trash cleanup)

**Issue:** `.planning/wave1/fix_plan.md` still shows tasks 1–5 as unchecked (`☐`), but memory confirms Wave 1.7 cleanup batch already shipped. Task 3 (remove vector tool router, deletes `registry_search_vector.go` + Qdrant collection `aura_tool_search_v2`) appears to overlap with the `BuildVectorIndex` deletion already done. Tasks 1 (RRF fusion in `mergeDocuments`), 2 (`tool_search` meta-tool), 4 (core toolset extended), 5 (`cmd/probe_tools` harness) may still be pending or partially done — the PRD doesn't say. PRD §1.2 line 131 lists `toolindex` and `toolsets` for merge into `agent/tools/` without flagging that Wave 1 was about to touch these files.

**Fix for v3:** In §3.1 add explicit status per Wave 1 task. For shipped items (BuildVectorIndex delete, COST_PER_TOKEN drop), note that Wave 1.7 is captured by restructure-as-cleanup and `.planning/wave1/fix_plan.md` should be archived after task verification. For pending items (RRF, tool_search, etc.), state whether they ship BEFORE Commit 3.2 (merge tools) or AFTER.

---

### F5 — MAJOR: Wave 2 graph-memory writers target paths the restructure moves

**Where:** PRD §5.14 line 547 (move `internal/ingest/*` → `internal/storage/sources/ingest/`) // `.planning/wave2/fix_plan.md` Task 2.3 line 89 (creates `internal/ingest/extractor.go`), Task 2.4 line 138 (modifies `internal/ingest/pipeline.go`, creates `internal/ingest/multi_page_touch.go`), Task 2.2 lines 55–59 (creates `internal/wiki/graph_index.go`)

**Issue:** Wave 2 creates ~1030 LOC across 5 commits, all targeted at file paths `internal/wiki/` and `internal/ingest/`. The PRD preserves `internal/wiki/` (good — §3 non-obiettivo #1) but relocates `internal/ingest/` under `storage/sources/`. If Wave 2 is mid-flight when Commit 1.4 (`chore(storage/sources): merge`) runs, the open work-in-progress diff breaks. If Wave 2 hasn't started, its plan needs path updates. The PRD doesn't address either direction. Also note Wave 2.2 builds `wiki/graph_index.go` for in-memory adjacency — this is exactly the "graph runtime" the PRD respects as locked invariant — but the PRD doesn't say Wave 2.2 is on the restructure's blessed path.

**Fix for v3:** §3.1 row for Wave 2: pick INTERLEAVED-before-restructure (ship Wave 2 first, restructure picks up new files) or DEFERRED-after-restructure (re-author Wave 2 plan with `storage/sources/ingest/` paths). State which Wave 2 success criteria the restructure does NOT regress (CLAUDE.md REUSABLE CODE rule for `internal/wiki/store.go::WritePage` invariants — restructure must preserve atomic-rename + git commit + per-page mutex; cite explicit acceptance criterion in §7).

---

### F6 — MAJOR: Wave 3 `propose_patch` landing site (Pack C) not addressed

**Where:** PRD §5 (no mention of `proposed_updates` table or summarizer reuse) // `.planning/wave3-agent-swarm/plan.md` Task 3.3 lines 125–186 // verified: `internal/conversation/summarizer/proposals.go:52-53` declares `SwarmRunID string` and `SwarmTaskID string` fields on `Provenance` struct (live in tree today)

**Issue:** Wave 3 Pack C is built on the explicit reuse of `internal/conversation/summarizer/proposals.go::Propose()` — the table schema already contains `SwarmRunID` and `SwarmTaskID` fields per `proposals.go:52-53`. CLAUDE.md REUSABLE-CODE rule was deliberately honored when the table was designed. The PRD does not list `internal/conversation/summarizer/` in §5 module-by-module and does not say where it lands after restructure. Worse: PRD §11 decision #2 ("conversation resta top-level") implies it stays but is silent on summarizer/proposals.go specifically. If Wave 3 Pack C ships before restructure, the file path is `internal/conversation/summarizer/proposals.go`; after restructure, unknown. The `propose_patch` tool itself (Task 3.3) lives under `internal/swarmtools/propose_patch.go` which Commit 3.2 moves into `agent/tools/swarmtools/`. Wave 3 plan is path-stale on this file too.

**Fix for v3:** Add to §5 a sub-section for `internal/conversation/summarizer/` stating it remains a sub-package of `conversation/` and that `proposals.go` is unchanged. Add to §3.1 the explicit statement that the `proposed_updates` table + `Provenance.SwarmRunID/SwarmTaskID` schema is preserved by the restructure (no migration touches it). Add an acceptance criterion in §7 G-new: `Grep -n 'SwarmRunID' internal/conversation/summarizer/proposals.go` returns at least 1 match post-restructure.

---

### F7 — MAJOR: §7 acceptance criteria miss domain invariants (wiki determinism, tool-argument privacy)

**Where:** PRD §7 lines 800–820 (G1–G16, all architectural except G13 artifact verification) // `CLAUDE.md` "Deterministic wiki writes: temperature=0, versioned prompts, atomic file writes + Git commits" and "Tool argument privacy: only tool names and argument keys are logged — never values"

**Issue:** G1–G16 cover package count, runtime singleton, JSON shape, LOC budgets, soak metrics, dependency edges. They do NOT have a single criterion that asserts:
1. Wiki writes after restructure still go through `WritePage` with `temperature=0`, atomic temp-file+rename, git commit, per-page mutex.
2. Tool argument privacy (only names + arg keys logged, never values, URLs with tokens, or base64) is preserved — Wave 3 Pack C `propose_patch` rationale-sanitization (line 152 of wave3 plan) is exactly this discipline and must survive the restructure.
3. Skills boot validator from Wave 3 Pack B (lines 85, 98) — does it survive Commit 1.5 / 1.6 merges?
4. `propose_patch` namespace isolation (only writes inside `WIKI_PATH`, blocks path traversal — Wave 3 line 162) — preserved post-restructure?

**Fix for v3:** Add G17 (wiki determinism), G18 (tool-arg privacy), G19 (skills-as-code validator survives), G20 (propose_patch namespace isolation). Each must be a one-line check (grep for `temperature=0` in wiki-write paths; grep for arg-value strings in log lines; boot validator runs and fails-loud on corrupted skill fixture; `target_slug` regex test).

---

### F8 — MAJOR: Effort estimate excludes the four `.planning/` waves; calendar conflict not surfaced

**Where:** PRD §10 lines 877–892 (110h / ~6 weeks) // `.planning/CONTEXT-ENGINEERING-ROADMAP.md` line 91 (10–17 days), `.planning/wave2/fix_plan.md` (~1030 LOC + tests, ~1–2 weeks implied), `.planning/wave3-agent-swarm/plan.md` (3 packs, ~700-950 LOC, ~1–2 weeks)

**Issue:** PRD honesty note v2 (§10 line 892) reads "6 weeks calendar" and acknowledges Phase 7's split. But it does not acknowledge that the user has roughly 4–6 weeks of OTHER planned work (Wave 2 + Wave 3 + Context-Eng Fase 1–5) sitting in `.planning/`. If executed serially after restructure that's ~3 months. If interleaved, the restructure timing must accommodate Wave 3 Task 3.1 PhaseSink work landing on the post-restructure code (which means Wave 3 plan must be re-authored first — a cost not in the §10 budget).

**Fix for v3:** §10 final paragraph adds a "Combined planning calendar" note: list the four waves' estimates, declare which run before/after/during restructure, and produce a total wall-clock estimate. If the answer is "restructure first, defer Wave 2/3/Context-Eng", acknowledge that in §10 honesty note.

---

### F9 — MAJOR: Wave 3 Pack A (`PhaseSink`, OTel spans, Telegram progressive surface) is invisible in the restructure

**Where:** PRD §4.3 lines 339–348 (Telegram data flow, no mention of phase events) // `.planning/wave3-agent-swarm/plan.md` Task 3.1 lines 24–69 (Pack A: 5 phase constants, OTel spans, progressive Telegram surface)

**Issue:** Wave 3 Pack A introduces `Phase string` field on `swarm.Task`, 5 typed phase constants, OTel span emission keyed `gen_ai.aura.agent.phase`, Telegram progressive-edit subscription via `internal/telegram/streaming.go`. The PRD §4.3 / §5.7 lay out the post-restructure event flow (`agent.Run` emits `Event{MessageDelta, ToolStart, ToolEnd, MessageDone, Usage}` → `chat.OutboundEvent` → channel adapter) and is silent on phase events. Three possible interpretations: (a) Wave 3 Pack A becomes redundant (already covered by `agent.Event` post-restructure), (b) Pack A's phase model is a strict superset (extends `Event` with phase enum), (c) the two designs conflict and PRD must pick. The silence is the bug.

**Fix for v3:** §4.3 add a line on phase events (or explicit drop). §5.1 `agent/` API section: if `EventType` gains a Phase variant, declare it; if Pack A is SUPERSEDED, say so and trace its requirements onto restructure event types.

---

### F10 — MAJOR: Memory invariant "Validate with verified benchmarks" applies but is under-enforced in §7

**Where:** PRD §7 G3 line 802, G6 line 805, G8 line 807 // `CLAUDE.md` "VALIDATE WITH VERIFIED BENCHMARKS, NEVER ONLY TOOL-CALL COUNTS" and "TESTS VERIFY QUALITY AND METRICS, NOT JUST 'DID IT RUN'"

**Issue:** G3 asserts `cmd/probe_chat` receives `ChatReply` "byte-identical to baseline" with `tokens ± 5 tolerance` — that's a structural check, not a content check. G6 asserts the store contains 3 task records — structural again. G8 asserts byte-comparison vs fixture — closest to "verify content", but the fixture is recorded under stub conditions (Commit 4.2 mock `tele.API`). None of G3/G6/G8 verifies that **the actual reply Aura produces is semantically correct** — i.e. the content matches the prompt's intent (the CLAUDE.md "validate substantive content — required keywords, minimum reply length, structural elements" requirement). PRD G13 nails artifact-verification for xlsx, but the soak G14 only specifies error rate / p95 / RSS, not content correctness.

**Fix for v3:** Add G-new (e.g. G21): one probe in the soak harness must run a deterministic semantic prompt (e.g. "list 3 wiki pages tagged X") and assert the reply contains the expected slugs by name AND the verified ground-truth (DB query for tag X). Otherwise the 48h soak can pass with Aura returning lobotomized text. Pair G14 (operational metrics) with G21 (semantic correctness on a fixture set of N≥5 prompts).

---

### F11 — MINOR: Memory invariant index in §8 row "Aura must always know her tools exist" cites Commit 2.1 but not the structural risk

**Where:** PRD §8 line 837 // memory `feedback_agent_must_know_tools_exist.md`

**Issue:** Row reads "Commit 2.1 elimina divergenza `chatPipeService` vs `conversation.RenderSystemPrompt`" — true and important, but it doesn't say what enforces this after restructure. Post-restructure `chat.Hub` is the only entry into `agent.Run`; if a future caller bypasses the hub and reuses a slim system prompt (Wave 3 Pack B's swarm-orchestrator skill could become that vector if it ever ships its own prompt scaffold), the invariant breaks again silently. Acceptance G5 (line 804) catches direct `agent.Run(` calls outside `internal/chat/*`, but does NOT assert system-prompt provenance.

**Fix for v3:** Either tighten G5 to also assert the system prompt was built via `conversation.RenderSystemPrompt` (probe via the conversation archive or chat_events), or add G-new "no agent entry point hand-rolls a system prompt; all paths route through `conversation.RenderSystemPrompt` or its post-restructure equivalent."

---

### F12 — MINOR: Picobot/swarm/Kimi K2.5 reconciliation — Wave 3 plan does cite paper-like patterns

**Where:** PRD §3 non-obiettivo #12 lines 230–233 (Kimi paper dropped) and §12 line 922 (paper as reference only) // `.planning/wave3-agent-swarm/plan.md` lines 4–8 (cites Hermes, nanobot patterns; converges on "orchestrator-worker with bounded `max_spawn_depth`") and line 14 ("NO swarm-puro / mesh / peer-to-peer — 2026 production consensus: swarms drift without supervisor")

**Issue:** Reviewer 1 F7 told the author to demote the Kimi K2.5 citation; v2 complied. The user's existing Wave 3 plan independently arrived at orchestrator-worker pattern with frozen-subagent isolation (Wave 3 plan line 17: "ALL write paths through existing `internal/wiki/store.go::WritePage`", line 162: namespace isolation) — Aura's local version of paper's PARL frame, without RL. The PRD could legitimately add ONE line: "The pattern Aura already implements (Wave 3 plan, agent-swarm) matches the architectural primitives of arXiv:2602.02276 §3 (orchestrator-worker, frozen workers via tool-allowlist, indirect write via proposals) without adopting the training apparatus." — but the v2 author chose silence. That's defensible.

**Fix for v3:** No change required. If the author wants, optionally add the one-line connection in §12 reference block citing `.planning/wave3-agent-swarm/plan.md` alongside the paper. Otherwise leave as-is.

---

### F13 — MINOR: `internal/conversation/summarizer/` missing from §5 module table

**Where:** PRD §1.2 line 141 lists "conversation" as kept-isolated; §5 has no §5.x for summarizer // `internal/conversation/summarizer/proposals.go` exists in tree, used by Wave 3 Pack C

**Issue:** §5 covers `internal/agent/`, `internal/agent/governance/`, `internal/agent/memory/`, `internal/agent/skills/`, `internal/agent/tools/`, `internal/chat/`, `internal/channels/{telegram,web,silent}/`, `internal/cron/`, `internal/llm/`, `internal/mcp/`, `internal/session/`, `internal/storage/{archive,search,sources}/`, `internal/config/`, `internal/api/`, `internal/swarm/`, `cmd/aura/` — but not `internal/conversation/` or its summarizer sub-package. The PRD says conversation stays top-level (§11.2) but doesn't enumerate its sub-tree. Wave 3 Pack C reuses `summarizer.SummariesStore.Propose()` (line 133), so summarizer's post-restructure location matters.

**Fix for v3:** Add §5.19 (or insert §5.6.5) for `internal/conversation/`: list sub-packages (`summarizer/`, plus whichever others survive), state unchanged, cite the Wave 3 Pack C dependency on `summarizer.Propose()`.

---

### F14 — MINOR: Master-direct workflow honored, but commit count mismatches §10 table vs §0 sommario

**Where:** PRD §0 line 26 ("8 commit rinomina/merge + 8 commit refactor reali + 2 sub-commit test-harness") = 18 // §10 line 888 "23 commits" // §6 enumerates 23 commits

**Issue:** §0 says "18 commit"; §10 totals 23; §6 enumerates 23 (1.1–1.7 = 7, then 2.1, 3.1–3.3, 4.1–4.3, 5.1–5.2, 6.1–6.2, 7.1–7.3, 8.1–8.2 = 16 → total 23). The §0 sentence is wrong. Minor but reviewer 2 has the same suspicion as reviewer 1 had about field counts: numbers in the sommario should match the body.

**Fix for v3:** Update §0 to "7 rinomina/merge commits + 13 refactor commits + 1 fixture harness commit + 2 cleanup commits = 23 atomic commits". Match the §10 table.

---

## Recommendations (non-blocking)

1. **Add `.planning/` archive note in §11.** After resolution of F1, the executor will need to know whether to archive `.planning/wave1/fix_plan.md`, `wave2/fix_plan.md`, `wave3-agent-swarm/plan.md`, `CONTEXT-ENGINEERING-ROADMAP.md` post-restructure. Default suggestion: move to `.planning/_archived/2026-05-pre-restructure/`, leave a stub pointing to the relevant PRD section.

2. **§4.4 swarm Channel flow could absorb Wave 3 Pack C cleanly.** The PRD's swarm-via-Hub bridge (line 374, ~40 LOC `swarmHubBridge`) is a natural landing site for `propose_patch` tool delivery — workers emit a tool result, the bridge forwards it to summarizer.Propose. If the author goes INTERLEAVED on Wave 3 Pack C, this is the spot.

3. **Add an acceptance G-doc-archive.** `.planning/wave-1.*` etc. moved or annotated. One-line check via `Test-Path`.

4. **§7 G14 metric "≤ baseline + 100ms p95" lacks a per-channel breakdown.** Telegram, /api/chat, cron, swarm have very different latency profiles. State each separately or accept the worst-case bound.

5. **§5.10 `internal/cron/` sub-cartelle preserve dominio — but Wave 3 Pack A's "boot-time recovery sweep" (line 39, query `status='running'` swarm tasks) targets `internal/swarm/manager.go`, not cron.** If Wave 3 Pack A INTERLEAVES, no conflict; if SUPERSEDED, this recovery sweep becomes a cron job and should be listed in cron's responsibilities.

---

## Verdict justification

REVISE_AND_RESUBMIT. The PRD honors all 17 of the hard memory invariants (verified row-by-row in §8) and gets the artifact-verification probe discipline right (G13). The architectural decisions are sound. The blocker is **silence on the four ongoing `.planning/` waves** — the user has invested 3+ weeks of planning into Context-Engineering, Wave 1, Wave 2, and Wave 3-agent-swarm work that directly contradicts or depends on files the restructure deletes/moves/renames. Without a §3.1 "Planning-state reconciliation" decision matrix per wave, an executor cannot know whether Wave 3 Task 3.1 PhaseSink work goes BEFORE Commit 2.1 (Wave 3 first), AFTER (re-author Wave 3), or NEVER (Wave 3 is replaced by post-restructure event-type extension). Three BLOCKERs (F1, F2, F3) plus six MAJORs (F4–F9, F10) plus four MINORs (F11–F14) — all clustered around planning-state alignment and the missing domain-invariant acceptance criteria. Once §3.1 is added and §7 grows G17–G21, the PRD is commit-ready.

— END REVIEW-2 —
