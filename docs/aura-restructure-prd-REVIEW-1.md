# REVIEW-1: Architecture critic

> **Status: HISTORICAL — architecture review of `aura-restructure-prd.md` v1. Findings F1-F15 are mostly addressed in [prd.md](../prd.md). Preserved as evidence per prd.md §3.2.**

**Date:** 2026-05-14
**Reviewer:** Architecture
**PRD version reviewed:** v1 (`docs/aura-restructure-prd.md`)
**Verdict:** REVISE_AND_RESUBMIT

## Summary

The PRD correctly diagnoses the three runtimes, correctly identifies the chathub Telegram outbound as a regression (verified — `chathub/adapters/telegram/outbound.go` has none of `renderForTelegramEntities`, `🧠 _cot_`, reasoning-channel split, nor entity-fallback), and correctly maps the canonical-by-design winners. However it presents a picobot LOC budget that is structurally unfair (picobot has no streaming, no phantom guard, no microcompact, no permissive pool, no entity-rendered output, and `chat.go` is a buffered-channel router with NO dispatcher/runID/event-tap surface), it mis-states the `*Bot` field count, the §5.6 "≤300 LOC" hub budget is unachievable without functional regression, the paper citation is decorative rather than architectural, and the directory target G1 is arithmetically wrong (§4.1's own count is 22, then §5 walks itself further away to "16 if we close conversation"). The single most important fix: drop the picobot LOC comparison as a design constraint and instead derive the hub/agent budgets from the verified surface area of features Aura must preserve (CoT, entity rendering, phantom guard, microcompact, permissive pool, streaming).

## Findings

### F1 — BLOCKER: god-class field count is wrong; weakens the diagnosis
**Where:** PRD §0 line 26 and §1.1 lines 44-60 // `D:/Aura/internal/telegram/bot.go:40-81`
**Issue:** PRD claims `*Bot` has 35 fields. Actual count of struct fields in `bot.go:40-81` is **41** (counting every field declaration, including `bgCtx/bgCancel/bgWg`, `debugDocsMu/debugDocs/debugDocSeq`, anonymous `compactMemoryHealth interface{...}`, plus all the explicit slots). The number is in the executive summary, the diagnosis title, and the rationale for Commit 16. An author who can't count fields cannot be trusted to inventory file moves. Either the PRD is wrong or the codebase moved under it — either way v2 must re-count and re-cite the line range.
**Fix for v2:** Re-read `bot.go:40-81`, give the actual count and break it down by category. If the breakdown table in §1.1 is the source of truth, every field listed there must map to a real line in bot.go (e.g. `Username` is a method, not a field — the table conflates fields and methods).

### F2 — BLOCKER: §5.6 "chat ≤300 LOC" budget is unsupportable
**Where:** PRD §5.6 line 328 // `internal/chathub/{types.go,hub.go,agentloop.go}` (174+274+220 = **668 LOC** plus 4 adapter files)
**Issue:** PRD wants a single `internal/chat/` package at ≤300 LOC total, derived from picobot's 99-LOC chat.go. But picobot's `chat.go` (verified — `D:/tmp/picobot/internal/chat/chat.go`, 99 LOC) is **a pair of buffered channels with a Subscribe/StartRouter map** — no Run record, no event ID minting, no `/stop` registry, no run-status state machine, no fan-out of multi-event types, no per-channel/per-mode adapter resolution, no `Run.Metadata`, no `EmitFn` translator. Aura's hub IS a different shape because it carries 7 EventType cases (RunStarted, MessageDelta, ToolStart, ToolEnd, MessageDone, Usage, Done/Error/Cancelled) and bidirectional dispatch. The 3 proposed "drops" in §5.6 (Drop `Run.Metadata`, drop the `(channel,mode)` matrix, drop the "any-adapter" fallback) total <50 LOC of savings — leaving ~620 LOC. ≤300 cannot be hit without ripping out features.
**Fix for v2:** State the actual achievable LOC after the 3 drops (≈600 LOC). If the PRD wants to go lower it must enumerate which capability is going away. Either way, justify the budget against Aura's earned surface, not picobot's missing surface.

### F3 — BLOCKER: picobot LOC comparison is unfair across the board
**Where:** PRD §1.1 Zona C line 80 (claims Aura is "3× larger without covering more cases") and §0 line 26 // `D:/tmp/picobot/internal/agent/loop.go` (369 LOC, full file read)
**Issue:** Picobot's agent loop has none of: phantom-tool guard (verified in `internal/agentloop/phantom_guard.go`), microcompact (verified in `internal/agentloop/governance.go`), permissive tool pool / `ToolResolver` (verified in `loop.go:159-176`, `loop.go:332`), per-tool `MaxCallsPerTool` budget, sliding-context `EnforceLimit` (in `internal/conversation/context.go`), or streaming token consumption (picobot uses `provider.Chat`, not `provider.Stream`). Picobot ALSO has no entity-rendered output — it puts a plain string on `out.Content`. The PRD's "covering more cases" comparison is comparing a feature-light reference to a feature-heavy production system. The covered-cases ratio is roughly inverse to what §1.1 implies.
**Fix for v2:** Add a feature matrix row to §1.1 enumerating "feature X — picobot has it? Aura has it?" with file refs. Drop the "3×" rhetoric; calibrate the agent/chat LOC targets to the count of features Aura preserves, e.g. ≤1600 LOC for `agent/` is realistic once phantom_guard.go (380 LOC) + governance.go + loop.go + runtime.go + pool.go are summed.

### F4 — BLOCKER: §11 open-question 7 ("drop `Run.Metadata`") is contradicted by the very code it lives in
**Where:** PRD §5.6 line 330 ("Drop `Run.Metadata`") and §11.7 line 760 // `internal/chathub/agentloop.go:91-103, 202-215`
**Issue:** The PRD proposes dropping `Run.Metadata` then asks Reviewer 1 to grep for callers. Result: the chathub's own `AgentLoopAdapter` writes `tools_exposed`, `prompt_version`, `prompt_hash`, `toolset`, `stats`, `final_text`, `delivered` into `Run.Metadata` (8 production writes in `agentloop.go`). Plus `chathub/adapters/telegram/outbound.go:122-136` reads `event.Payload["tele_bot"]`, `event.Payload["tele_recipient"]`, `event.Payload["tele_placeholder"]` from a payload map — a Metadata-shaped surface under a different name. Dropping Metadata without first replacing the channel-handle injection path will break the Telegram outbound. The PRD's claim "adapter-private state passes via ChannelData on InboundMessage" is correct in direction but the migration has cost ≥ Commit 12's effort, not the casual "drop" §5.6 implies.
**Fix for v2:** Either keep `Run.Metadata` and justify, or add a dedicated commit "refactor(chat): replace Run.Metadata with InboundMessage.ChannelData + typed event payloads" before Commit 12, with its own risk row in §9. Do not relegate this to an open question.

### F5 — MAJOR: G1 (package count target) is internally inconsistent
**Where:** PRD §2 line 118 (G1 ≤16) // §4.1 line 187 (claims 20) // §5 line 452 (claims "22 if counted, 16 if 2-3 more merges") // §7 line 666 (acceptance changes to ≤22)
**Issue:** The same goal is given three different numbers in three sections. The acceptance criteria silently relaxes the goal from ≤16 to ≤22. The "concurrency ⇒ session" speculation in §5 is left unresolved. Today Aura has 49 dirs (verified, `ls internal/`); the PRD's own merge math: 49 − (mcp 3→1 = −2) − (tools 4→1 = −3) − (storage/search 5→1 = −4) − (storage/sources 4→1 = −3) − (config 3→1 = −2) − (api 4→1 = −3) − (agent 3→1 = −2) − (session merging concurrency = −1) − (chathub→chat rename 0) − (scheduler→cron rename 0) − (orchestration is empty: −1) = **49 − 21 = 28**, not 16 and not 22. Some "merges" are sub-directory pushes that don't reduce top-level count.
**Fix for v2:** Pick one target number, show the arithmetic explicitly (line-by-line subtraction starting at 49), and make the acceptance match. If you can only reach 22, change G1 to 22 and stop pretending 16.

### F6 — MAJOR: `internal/orchestration/` is an EMPTY directory; PRD lists it as preserved
**Where:** PRD §1.2 line 98 ("Da mantenere isolati: ... orchestration ...") // `D:/Aura/internal/orchestration/` contains no `.go` files
**Issue:** The directory exists but has zero source files. Listing it as a top-level package to "preserve" is wrong — it should be deleted as part of the cleanup, not preserved. This is a small instance of a larger pattern: the PRD did not actually walk all 49 directories to verify what they contain. Other suspect cases the reviewer would re-walk: `tracing/`, `debugguard/`, `dbrecovery/`, `install/`, `release/` — do any of these have <100 LOC or no callers? The PRD's package inventory looks like it was assembled from memory, not from `ls`.
**Fix for v2:** Re-walk `internal/` with `Get-ChildItem internal -Directory | ForEach-Object { $_; Get-ChildItem $_.FullName -Filter *.go }`. Annotate each with LOC and caller count. Delete empties; collapse single-file <100 LOC packages explicitly.

### F7 — MAJOR: Paper citation is decorative, not architectural
**Where:** PRD §0, §4.4, §4.5, §12 // `D:/tmp/paper.md` §3 (lines 302-425 esp. 320-365)
**Issue:** The paper's Agent Swarm has three load-bearing concepts: (1) **orchestrator/subagent decoupling** — orchestrator is trainable, subagents are frozen checkpoints, trajectories excluded from optimization; (2) **PARL reward** — `r_parallel` to prevent serial collapse, `r_finish` to prevent spurious parallelism, both annealed to zero; (3) **critical-steps metric** — latency is `max(subagent_steps)` per stage, not `sum`. The PRD §4.4 reuses the same `agent.Task` shape for parent and child, runs everything through the same Hub with no "frozen" distinction, has no equivalent of `r_parallel`/`r_finish`, and no critical-step accounting. The "Channel: swarm" idea is just routing — it does NOT operationalize the paper's pattern. Worse, the PRD claims swarm becomes "first-class, validated by the paper" while actually it remains a thin wrapper over `agent.Runner.Run` (and after Commit 8, over `agentruntime.Run`).
**Fix for v2:** Either (a) drop the paper from §0/§4.5/§12 entirely (Aura doesn't do RL and the parallel-decomposition pattern predates the paper); or (b) keep the citation but address explicitly: how does Aura prevent serial collapse (LLM never spawning workers when it should)? How does it detect spurious parallelism (workers that don't aggregate)? How is "frozen" enforced — by code path, by tool isolation, by separate prompt? Without these answers the citation is rhetorical filler that weakens the PRD.

### F8 — MAJOR: G3 ("byte-identical /api/chat JSON shape") under-specified
**Where:** PRD §2 line 120 (G3) and §7 line 668 // `D:/Aura/internal/api/chat.go:29-35` (actual `ChatReply` struct)
**Issue:** Real shape is `{reply string, elapsed_ms int64, llm_calls int, tool_calls int, tokens int}`. Note `tokens` is a SCALAR int (likely prompt+completion sum), NOT a struct like `llm.TokenUsage` which has Prompt/Completion/CachedReadInput/etc fields. After migrating to `agentruntime` (Commit 8) the natural shape carries `llm.TokenUsage`. The "byte-identical" criterion will fail by construction unless the chathub bridge collapses TokenUsage back to a scalar. PRD §9 lists "Web `/api/chat` JSON shape drift" as Low probability, but this is a guaranteed shape choice not a probability.
**Fix for v2:** Decide explicitly: (a) preserve scalar `tokens` (and document the collapse rule: prompt+completion+cache_read? prompt+completion only?), or (b) break the shape and version the endpoint. Either choice needs to be in §5.8 (`channels/web`) or in a new section, not implicit in an acceptance criterion.

### F9 — MAJOR: Commit 8 (kill `agent.Runner`) under-estimates the unlimited-tool-calls path
**Where:** PRD §6 Commit 8 lines 514-525 // `D:/Aura/internal/agent/runner.go:77` (Task.MaxToolCalls int) and `D:/Aura/internal/agentloop/loop.go` (uses `MaxCallsPerTool`)
**Issue:** PRD claims "swarm uses `agent.Runner.Run` with options like `MaxToolCalls=0` (unlimited)" and asserts migration to `agentruntime.MaxIterations` is a clean swap. But `Task.MaxToolCalls` and `agentloop.Options.MaxCallsPerTool` are SEMANTICALLY DIFFERENT: the former is a total budget across the task; the latter is per-tool. Setting one to 0 (unlimited) is not equivalent to leaving the other unset. Additionally `agent.Runner` has `MaxToolResultChars`, `FinalizationTimeout`, `CompleteOnDeadline`, `ToolAllowlist` (verified runner.go:75-81) — agentruntime.Invocation may or may not expose all of these. Without an explicit field-by-field mapping table the migration is not commit-ready.
**Fix for v2:** Add a table to Commit 8: every field of `agent.Task` → where it goes in `agentruntime.Invocation` or `agentloop.Options` (with file:line refs) → semantic gotchas. If a field has no destination, decide: deprecate, preserve, or rename.

### F10 — MAJOR: Commit 16 sized at 20h is unrealistic given imports
**Where:** PRD §6 Commit 16 lines 600-612 // `internal/telegram/setup.go` has 32 internal-package imports (§1.1 enumerates them; verified setup.go is 1036 LOC with 76 `b.<field>` accesses)
**Issue:** Moving setup.go to `cmd/aura/app.go` is not "splitting in wire_*.go" — it is rewriting the composition root with new lifetime ownership (`*App` instead of `*Bot`), reshaping which package owns which background goroutine (`bgWg/bgCancel` currently on `*Bot`), and migrating 32 import sites. The PRD acknowledges this is "the most risky commit" but budgets 20h. Realistic single-developer budget for a god-class extraction touching 32 dependency boundaries plus tests is 32-48h. The PRD's own "honesty note" §10 admits the original 2-3w estimate was wrong; Commit 16 specifically deserves the same honesty.
**Fix for v2:** Re-estimate Commit 16 at 32-48h. Break it into 16a (move wiring functions, keep `*Bot`), 16b (extract goroutine ownership), 16c (delete legacy fields). Each sub-commit gets its own validation gate.

### F11 — MAJOR: PRD does not address `internal/swarmtools/` at all in §5 module-by-module
**Where:** PRD §1.2 line 95 lists `swarmtools` for merge into `agent/tools/`, §5.5 mentions sourcing from swarmtools, but §6 Commit 3 (merge agent/tools) just says "merge tools + toolindex + toolsets + swarmtools" with no detail // `D:/Aura/internal/swarmtools/` contains `tools.go` (15937 bytes = ~450 LOC), `delegation_policy.go` (3113 bytes), plus tests
**Issue:** swarmtools wires LLM-facing tools that talk to `swarm.Manager`. After Commit 15 (swarm via Hub), the swarmtools/tools.go calls into swarm need to be re-pointed at the Hub bridge. The merge in Commit 3 happens BEFORE Commit 15 — so swarmtools migrates with stale wiring and then must be touched again. This either creates a transitive rebuild step that's not in the plan, or breaks the "every commit leaves tree green" invariant.
**Fix for v2:** Either swap Commit 3 and Commit 15 order (do swarm-via-Hub first, then merge tools), or add an explicit "swarmtools wiring re-target" step inside Commit 15.

### F12 — MAJOR: Commit 12's "byte-comparison test" is not buildable as described
**Where:** PRD §6 Commit 12 lines 558-566 and §7 G8/G9 lines 673-674 // `D:/Aura/internal/telegram/streaming.go:101-208` (consumeStream)
**Issue:** `consumeStream` takes a real `tele.Context` and calls real `bot.Send`/`bot.Edit`. There is no recorded fixture today. The PRD says "in-memory mock" (§11.6) but `tele.Bot` has no exported interface — `tele.API` is the interface (verified: chathub outbound uses `tele.API`), but `tele.Context` is a concrete struct. Building a deterministic edit-sequence capture requires either (a) a `tele.API` mock that records calls in order, plus a shim `tele.Context`, or (b) a probe that runs the real bot against a Telegram test account. Neither exists today. The "byte-comparison test" is therefore a tool that must be BUILT inside Commit 12, not a gate the commit can simply pass.
**Fix for v2:** Promote the test harness to a dedicated Commit 11.5: "test(channels/telegram): record-and-replay fixture for streaming edits". Commit 12 then consumes the fixture. The harness has its own LOC and effort estimate.

### F13 — MAJOR: §5.10 cron renaming hides a real change in scheduler responsibilities
**Where:** PRD §5.10 lines 374-378 // `D:/Aura/internal/scheduler/` contains scheduler.go AND agent_job.go, issues.go, maintenance.go, wake.go
**Issue:** Scheduler today is NOT just a cron-runner. It owns: `agent_job` records (background agent tasks), `issues` queue (wiki maintenance), `maintenance` (periodic wiki-rebuild jobs), `wake` (cold-start re-hydration). Renaming the package to `cron` misnames 4 of its 5 responsibilities and will create import-time confusion. PRD also fails to note that scheduler tick → agent run is the second production caller of `agent.Runner.Run` (after swarm), so the Commit 8 migration must also re-target this path.
**Fix for v2:** Either keep `scheduler` name and add a sub-package `cron/` for the tick mechanism only, or rename to `cron` but split out `issues/` and `maintenance/` into their own packages first. Add scheduler-agentJob → agentruntime.Run migration as a sub-step of Commit 8.

### F14 — MINOR: §1.1 import count of 32 for setup.go matches reality but mis-presents 6 of them
**Where:** PRD §1.1 line 62 // `D:/Aura/internal/telegram/setup.go` imports
**Issue:** PRD lists 32 internal-package imports for setup.go. Spot-checked: the list includes `mcppolicy`, `mcpwatch`, `qdrant`, `markitdown`, `memoryindex` — all real. But it does NOT include `agent` (used to construct `agentRunner`), and `agentloop`/`agentruntime` are conflated. Minor but worth fixing.
**Fix for v2:** Re-derive the list with `grep "github.com/aura/aura/internal" setup.go | sort -u | wc -l`. Cite the actual count.

### F15 — MINOR: Memory invariants section misses one
**Where:** PRD §8 lines 689-707 // `memory/feedback_minillm_cpu_not_viable_for_tool_retrieval.md` (cited in user's MEMORY.md but not in §8)
**Issue:** §8 enumerates 16 memory invariants but skips the "mini-LLM CPU not viable for per-tool retrieval — use Embed cosine + manifest + permissive fallback" note. The restructure preserves the permissive tool pool (which IS this invariant in code), so the invariant is honored, but the table should cite it so the author of Commit 3 (merging tool packages) knows not to introduce a tool-routing LLM.
**Fix for v2:** Add the row.

## Recommendations (non-blocking)

1. **Add a §4.6 "what stays the same" diagram.** The PRD spends 800 LOC saying what moves. A 30-line diagram of "wiki, llm, budget, files, sandbox, workspace, tray, logging, db do not change" reduces reviewer load.

2. **Number the commits by phase.** §6's 19 commits cross 7 distinct phases (§10 already groups them). Re-label as Commit 1.1, 1.2, … so a phase rollback is one command.

3. **Acceptance G14 ("48h soak")** should specify the metric: error rate per 1000 messages, p95 latency, RSS growth slope. "Operativi senza regressione user-visible" is hand-wave.

4. **§4.2 dependency table** is informative but not executable. Add the exact `go list -deps` command(s) and the regex grep to enforce each forbidden edge in CI.

5. The §11 open questions are written with default answers already. Promote the defaults into the PRD body and DELETE the open-question section. Reviewers should answer questions that are genuinely open, not rubber-stamp defaults.

## Verdict justification

REVISE_AND_RESUBMIT. The PRD's diagnosis is mostly accurate (especially the chathub Telegram outbound regression — verified) and the commit ordering is broadly sane, but it has 4 blockers (field-count miscount that undermines the diagnosis credibility; an unsupportable hub LOC budget derived from a non-comparable reference; an unfair picobot LOC comparison that propagates everywhere; the `Run.Metadata` drop that the PRD itself proves is used in 8+ production writes) plus 9 majors that affect commit executability. The single most important fix is to re-anchor the LOC budgets and feature claims against Aura's verified surface rather than picobot's missing surface — once that's done, ~6 of the findings (F2, F3, F5, F10) collapse to consistent numbers. The paper citation should either earn its place by addressing serial-collapse / spurious-parallelism / frozen-subagent operationalization, or be dropped entirely. With these fixed v2 should be commit-ready.

— END REVIEW-1 —
